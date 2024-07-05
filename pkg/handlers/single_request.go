package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// singleRequestHandler is the counterpart to launchHandler as it holds state
// and functionality for dealing with a single incoming request. All global
// process-handling responsibilities are owned by launchHandler.
type singleRequestHandler struct {
	req  *http.Request
	resp http.ResponseWriter
	log  *log.Entry
	lh   *launchHandler

	// Fields that are set during handling
	payload              *launchPayload
	buf                  *bytes.Buffer
	processCtx           context.Context
	cancelProcessContext context.CancelFunc
	testRunRequested     bool
	asyncCleanup         bool
	// This stores context information over the request time to be submitted to
	// the end-user via slack.
	slackContext string
	slackThreads map[string]string
}

func newSingleRequestHandler(resp http.ResponseWriter, req *http.Request, lh *launchHandler) *singleRequestHandler {
	srh := singleRequestHandler{
		resp: resp,
		req:  req,
		log:  createLogEntry(req),
		lh:   lh,
	}
	return &srh
}

func (h *singleRequestHandler) Handle(requestCtx context.Context) {
	if err := h.requestTestRun(); err != nil {
		h.log.Warn("Maximum concurrent test runs reached. Rejecting request.")
		h.resp.Header().Set("Retry-After", fmt.Sprintf("%d", h.lh.getWaitTime()))
		http.Error(h.resp, "Maximum concurrent test runs reached", http.StatusTooManyRequests)
		return
	}

	h.buf = &bytes.Buffer{}

	payload, err := newLaunchPayload(h.req)
	if err != nil {
		h.log.Error(err)
		http.Error(h.resp, fmt.Sprintf("error while validating request: %v", err), 400)
		h.lh.releaseTestRun()
		return
	}
	h.payload = payload
	h.slackContext = payload.Metadata.NotificationContext

	if err := h.checkAgainstLastFailureTime(); err != nil {
		h.failRequest(err)
		return
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer func() {
		if payload.Metadata.WaitForResults {
			cancelCtx()
		}
	}()
	go func() {
		h.propagateCancel(requestCtx, payload, cancelCtx)
	}()
	h.processCtx = ctx
	h.cancelProcessContext = cancelCtx

	cmd, err := h.startK6Test(ctx)
	if err != nil {
		if cmd != nil {
			h.logIfError(h.sendSlackMessage(h.payload.statusMessage(emojiFailure, "didn't start successfully")))
			h.logIfError(h.addFileToSlackThread("k6-results.txt", h.buf.String()))
			h.registerProcessCleanup(cmd)
		}
		h.failRequest(err)
		return
	}

	if err := h.attachCloudURL(); err != nil {
		h.failRequest(err)
		h.registerProcessCleanup(cmd)
		return
	}

	// Write the initial message to each channel
	h.logIfError(h.sendSlackMessage(payload.statusMessage(emojiWarning, "has started")))

	// Now process the result
	if err := h.processResult(cmd); err != nil {
		// The process has already been registered for cleanup inside processResult
		// where appropriate.
		h.failRequest(err)
		return
	}
}

func (h *singleRequestHandler) requestTestRun() error {
	h.log.Info("Requesting test run")
	if err := h.lh.requestTestRun(); err != nil {
		return err
	}
	h.testRunRequested = true
	return nil
}

func (h *singleRequestHandler) releaseTestRun() {
	h.log.Info("Releasing test run")
	if !h.testRunRequested {
		return
	}
	if h.asyncCleanup {
		h.log.Debug("releasing will happen asynchronously")
		return
	}
	h.lh.releaseTestRun()
	h.testRunRequested = false
}

func (h *singleRequestHandler) registerProcessCleanup(cmd k6.TestRun) {
	h.asyncCleanup = true
	h.lh.registerProcessCleanup(cmd)
}

func (h *singleRequestHandler) processResult(cmd k6.TestRun) error {
	if !h.payload.Metadata.WaitForResults {
		h.log.Infof("the load test for %s.%s was launched successfully!", h.payload.Name, h.payload.Namespace)
		// We also need to register the cancelCtx func for asynchronous
		// cleanup. In the synchronous cases we can cancel that context right
		// away.
		cmd.SetCancelFunc(h.cancelProcessContext)
		h.registerProcessCleanup(cmd)
		return nil
	}

	defer func() {
		h.releaseTestRun()
	}()

	h.log.Info("waiting for the results")
	err := cmd.Wait()
	h.lh.trackExecutionDuration(cmd)
	h.logIfError(h.addFileToSlackThread("k6-results.txt", h.buf.String()))

	// Load testing failed, log the output
	if err != nil {
		h.logIfError(h.updateSlackMessage(h.payload.statusMessage(emojiFailure, "has failed")))
		return fmt.Errorf("failed to run: %w", err)
	}

	// Success!
	h.logIfError(h.updateSlackMessage(h.payload.statusMessage(emojiSuccess, "has succeeded")))
	_, err = h.resp.Write(h.buf.Bytes())
	h.logIfError(err)
	h.log.Infof("the load test for %s.%s succeeded!", h.payload.Name, h.payload.Namespace)
	return nil
}

func (h *singleRequestHandler) checkAgainstLastFailureTime() error {
	lastFailureTime, present := h.lh.getLastFailureTime(h.payload)
	if present && time.Since(lastFailureTime) < h.payload.Metadata.MinFailureDelay {
		return fmt.Errorf("not enough time since last failure")
	}
	return nil
}

func (h *singleRequestHandler) failRequest(err error) {
	msg := err.Error()
	h.lh.setLastFailureTime(h.payload)
	h.log.Error(msg)
	if h.buf != nil && h.buf.Len() > 0 {
		msg += "\n" + h.buf.String()
	}
	http.Error(h.resp, msg, 400)
	// If the request has been marked for async cleanup, releasing happens there
	if !h.asyncCleanup {
		h.releaseTestRun()
	}
	if h.cancelProcessContext != nil {
		h.cancelProcessContext()
	}
}

func (h *singleRequestHandler) startK6Test(ctx context.Context) (k6.TestRun, error) {
	h.log.Info("fetching secrets (if any)")
	envVars, err := h.buildEnvVars(h.payload)
	if err != nil {
		return nil, err
	}

	h.log.Info("launching k6 test")
	cmd, err := h.lh.client.Start(ctx, h.payload.Metadata.Script, h.payload.Metadata.UploadToCloud, envVars, h.buf)
	if err != nil {
		return nil, fmt.Errorf("error while launching test: %w", err)
	}

	h.log.Info("waiting for output path")
	// Find the Cloud URL from the k6 output
	if waitErr := h.waitForOutputPath(); waitErr != nil {
		return cmd, fmt.Errorf("error while waiting for test to start: %w", waitErr)
	}

	return cmd, nil
}

func (h *singleRequestHandler) sendSlackMessage(msg string) error {
	threads, err := h.lh.slackClient.SendMessages(h.payload.Metadata.SlackChannels, msg, h.slackContext)
	if err != nil {
		return err
	}
	h.slackThreads = threads
	return nil
}

func (h *singleRequestHandler) addFileToSlackThread(name string, content string) error {
	return h.lh.slackClient.AddFileToThreads(h.slackThreads, name, content)
}

func (h *singleRequestHandler) updateSlackMessage(msg string) error {
	return h.lh.slackClient.UpdateMessages(h.slackThreads, msg, h.slackContext)
}

func (h *singleRequestHandler) buildEnvVars(payload *launchPayload) (map[string]string, error) {
	envVars := payload.Metadata.EnvVars

	if len(payload.Metadata.KubernetesSecrets) == 0 {
		return envVars, nil
	}

	if h.lh.kubeClient == nil {
		return nil, errors.New("kubernetes client is not configured")
	}

	if envVars == nil {
		envVars = make(map[string]string)
	}

	for env, secret := range payload.Metadata.KubernetesSecrets {
		parts := strings.SplitN(secret, "/", 3)
		namespace := payload.Namespace
		if len(parts) > 2 {
			namespace = parts[0]
			parts = parts[1:]
		}
		secretName := parts[0]
		secretKey := parts[1]
		secret, err := h.lh.kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error fetching secret %s/%s: %w", namespace, secretName, err)
		}
		if v, ok := secret.Data[secretKey]; ok {
			envVars[env] = string(v)
		} else {
			return nil, fmt.Errorf("secret %s/%s does not have key %s", namespace, secretName, secretKey)
		}
	}
	return envVars, nil
}

func (h *singleRequestHandler) propagateCancel(requestCtx context.Context, payload *launchPayload, cancelCtx context.CancelFunc) {
	if payload.Metadata.WaitForResults {
		select {
		case <-requestCtx.Done():
			cancelCtx()
		case <-h.lh.ctx.Done():
			cancelCtx()
		}
	} else {
		// If we are not waiting for the results then we should only cancel
		// if the global context is done:
		<-h.lh.ctx.Done()
		cancelCtx()
	}
}

func (h *singleRequestHandler) waitForOutputPath() error {
	for i := 0; i < 10; i++ {
		if strings.Contains(h.buf.String(), "output:") {
			return nil
		}
		h.log.Debug("waiting 2 seconds for test to start")
		h.lh.sleep(2 * time.Second)
	}
	return errors.New("timeout")
}

func (h *singleRequestHandler) attachCloudURL() error {
	if !h.payload.Metadata.UploadToCloud {
		return nil
	}
	url, err := getCloudURL(h.buf.String())
	if err != nil {
		return err
	}
	h.slackContext += fmt.Sprintf("\nCloud URL: <%s>", url)
	h.log.Infof("cloud run URL: %s", url)
	return nil
}

func getCloudURL(output string) (string, error) {
	matches := outputRegex.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", errors.New("couldn't find the cloud URL in the output")
	}
	return matches[1], nil
}

func (h *singleRequestHandler) logIfError(err error) {
	if err == nil {
		return
	}
	h.log.Error(err.Error())
}
