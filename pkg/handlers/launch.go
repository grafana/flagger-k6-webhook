package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	emojiSuccess = ":large_green_circle:"
	emojiWarning = ":warning:"
	emojiFailure = ":red_circle:"
)

// https://regex101.com/r/OZwd8Y/1
var outputRegex = regexp.MustCompile(`output: cloud \((?P<url>https:\/\/((app\.k6\.io)|([^/]+\.grafana.net\/a\/k6-app))\/runs\/\d+)\)`)

type launchPayload struct {
	flaggerWebhook
	Metadata struct {
		Script string `json:"script"`

		// If true, the test results will be uploaded to cloud
		UploadToCloudString string `json:"upload_to_cloud"`
		UploadToCloud       bool

		// If true, the handler will wait for the k6 run to be completed
		WaitForResultsString string `json:"wait_for_results"`
		WaitForResults       bool

		// Notification settings. Context is added at the end of the message
		SlackChannelsString string `json:"slack_channels"`
		SlackChannels       []string
		NotificationContext string `json:"notification_context"`

		// Min delay between failures. All other runs will fail immediately. This prevents retries
		MinFailureDelay       time.Duration
		MinFailureDelayString string `json:"min_failure_delay"`

		// Set environment variables when running the k6 script
		EnvVars       map[string]string
		EnvVarsString string `json:"env_vars"`

		// Inject secrets to environment (map of `<ENV>` -> `<namespace (default: payload namespace)>/<secret name>/<secret key>`)
		KubernetesSecrets       map[string]string
		KubernetesSecretsString string `json:"kubernetes_secrets"`
	} `json:"metadata"`
}

func (p *launchPayload) statusMessage(emoji, status string) string {
	return fmt.Sprintf("%s Load testing of `%s` in namespace `%s` %s", emoji, p.Name, p.Namespace, status)
}

func (p *launchPayload) key() string {
	return fmt.Sprintf("%s-%s-%s", p.Namespace, p.Name, p.Phase)
}

func newLaunchPayload(req *http.Request) (*launchPayload, error) {
	var err error
	payload := &launchPayload{}

	if req.Body == nil {
		return nil, errors.New("no request body")
	}
	defer req.Body.Close()
	if err = json.NewDecoder(req.Body).Decode(payload); err != nil {
		return nil, err
	}

	if err := payload.validateBaseWebhook(); err != nil {
		return nil, fmt.Errorf("error while validating base webhook: %w", err)
	}

	if err := payload.validate(); err != nil {
		return nil, err
	}

	return payload, nil
}

func (p *launchPayload) validate() error {
	var err error

	if p.Metadata.Script == "" {
		return errors.New("missing script")
	}

	if p.Metadata.UploadToCloudString == "" {
		p.Metadata.UploadToCloud = false
	} else if p.Metadata.UploadToCloud, err = strconv.ParseBool(p.Metadata.UploadToCloudString); err != nil {
		return fmt.Errorf("error parsing value for 'upload_to_cloud': %w", err)
	}

	if p.Metadata.WaitForResultsString == "" {
		p.Metadata.WaitForResults = true
	} else if p.Metadata.WaitForResults, err = strconv.ParseBool(p.Metadata.WaitForResultsString); err != nil {
		return fmt.Errorf("error parsing value for 'wait_for_results': %w", err)
	}

	if p.Metadata.SlackChannelsString != "" {
		p.Metadata.SlackChannels = strings.Split(p.Metadata.SlackChannelsString, ",")
	}

	if p.Metadata.MinFailureDelayString == "" {
		p.Metadata.MinFailureDelay = 2 * time.Minute
	} else if p.Metadata.MinFailureDelay, err = time.ParseDuration(p.Metadata.MinFailureDelayString); err != nil {
		return fmt.Errorf("error parsing value for 'min_failure_delay': %w", err)
	}

	if p.Metadata.EnvVarsString != "" {
		if err := json.Unmarshal([]byte(p.Metadata.EnvVarsString), &p.Metadata.EnvVars); err != nil {
			return fmt.Errorf("error parsing value for 'env_vars': %w", err)
		}
	}

	if p.Metadata.KubernetesSecretsString != "" {
		if err := json.Unmarshal([]byte(p.Metadata.KubernetesSecretsString), &p.Metadata.KubernetesSecrets); err != nil {
			return fmt.Errorf("error parsing value for 'kubernetes_secrets': %w", err)
		}
	}

	return nil
}

type launchHandler struct {
	client      k6.Client
	kubeClient  kubernetes.Interface
	slackClient slack.Client

	lastFailureTime      map[string]time.Time
	lastFailureTimeMutex sync.Mutex

	processToWaitFor     chan k6.TestRun
	waitForProcessesDone chan struct{}
	ctx                  context.Context

	availableTestRuns chan struct{}

	metricsRegistry    *prometheus.Registry
	metricTestDuration *prometheus.SummaryVec

	// mockables
	sleep func(time.Duration)
}

type LaunchHandler interface {
	http.Handler
	Wait()
}

// NewLaunchHandler returns an handler that launches a k6 load test.
func NewLaunchHandler(ctx context.Context, client k6.Client, kubeClient kubernetes.Interface, slackClient slack.Client, maxConcurrentTests int) (LaunchHandler, error) {
	if slackClient == nil {
		return nil, errors.New("unexpected state. Slack client is nil")
	}

	h := &launchHandler{
		client:               client,
		kubeClient:           kubeClient,
		slackClient:          slackClient,
		lastFailureTime:      make(map[string]time.Time),
		sleep:                time.Sleep,
		processToWaitFor:     make(chan k6.TestRun, maxConcurrentTests),
		waitForProcessesDone: make(chan struct{}, 1),
		ctx:                  ctx,
	}
	h.availableTestRuns = make(chan struct{}, maxConcurrentTests)
	for range maxConcurrentTests {
		h.availableTestRuns <- struct{}{}
	}

	metricMaxConcurrentTests := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "launch_max_concurrent_tests",
		Help: "The maximum number of concurrent tests",
	})
	metricMaxConcurrentTests.Set(float64(maxConcurrentTests))
	if err := prometheus.Register(metricMaxConcurrentTests); err != nil {
		log.Warnf("Failed to register new metric: %s", err.Error())
	}

	metricAvailableConcurrentTests := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "launch_available_concurrent_tests",
		Help: "The current number of available concurrent tests. If 0 then new requests will be rejected",
	}, func() float64 {
		return float64(len(h.availableTestRuns))
	})
	if err := prometheus.Register(metricAvailableConcurrentTests); err != nil {
		log.Warnf("Failed to register new metric: %s", err.Error())
	}

	// metricTestDuration is an internal metric that we use to calculate the
	// expected wait time in case the maximum number of concurrent tests is
	// reached:
	metricTestDuration := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name:       "launch_test_duration",
		Help:       "Durations of the executed k6 test run in seconds",
		Objectives: map[float64]float64{0.5: float64(30)},
	}, []string{"exitCode"})
	h.metricTestDuration = metricTestDuration
	h.metricsRegistry = prometheus.NewRegistry()
	_ = h.metricsRegistry.Register(h.metricTestDuration)

	go h.waitForProcesses(ctx)
	return h, nil
}

// Wait is blocking until all subprocesses have terminated. This should only be
// used if the passed context can (and is) canceled.
func (h *launchHandler) Wait() {
	<-h.waitForProcessesDone
	log.Debug("launch handler finished")
}

// waitForProcesses handles incoming processes and waits for them to complete.
// This way we can avoid k6 jobs where we do not need the results to become
// zombie processes.
func (h *launchHandler) waitForProcesses(ctx context.Context) {
	defer func() {
		h.waitForProcessesDone <- struct{}{}
	}()
	wg := sync.WaitGroup{}
loop:
	for {
		select {
		case cmd := <-h.processToWaitFor:
			wg.Add(1)
			go func() {
				h.waitForProcess(cmd)
				wg.Done()
			}()
		case <-ctx.Done():
			break loop
		}
	}
	wg.Wait()
}

func (h *launchHandler) waitForProcess(cmd k6.TestRun) {
	if cmd == nil {
		log.Warnf("nil as testrun passed")
		return
	}
	pid := cmd.PID()
	log.WithField("pid", pid).Debug("waiting for testrun to exit")
	_ = cmd.Wait()
	h.trackExecutionDuration(cmd)
	log.WithField("pid", pid).Debugf("testrun exited")

	h.availableTestRuns <- struct{}{}
}

// registerProcessCleanup adds a handler to the process so that it will
// eventually be closed and its resources returned.
//
// Note that this method can actually block which will, in turn, cause the
// calling HTTP handler to be blocked.
func (h *launchHandler) registerProcessCleanup(cmd k6.TestRun) {
	h.processToWaitFor <- cmd
}

func (h *launchHandler) getLastFailureTime(payload *launchPayload) (time.Time, bool) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	v, ok := h.lastFailureTime[payload.key()]
	return v, ok
}

func (h *launchHandler) setLastFailureTime(payload *launchPayload) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	h.lastFailureTime[payload.key()] = time.Now()
}

func (h *launchHandler) buildEnvVars(payload *launchPayload) (map[string]string, error) {
	envVars := payload.Metadata.EnvVars

	if len(payload.Metadata.KubernetesSecrets) == 0 {
		return envVars, nil
	}

	if h.kubeClient == nil {
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
		secret, err := h.kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
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

func (h *launchHandler) getWaitTime() int64 {
	families, err := h.metricsRegistry.Gather()
	if err != nil {
		return 60
	}
	for _, family := range families {
		if family.GetName() == "launch_test_duration" {
			for _, metric := range family.GetMetric() {
				for _, quantile := range metric.GetSummary().GetQuantile() {
					if quantile.GetQuantile() == 0.5 {
						result := quantile.GetValue()
						return int64(result)
					}
				}
			}
		}
	}
	return 60
}

func (h *launchHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cmdLog := createLogEntry(req)
	select {
	case <-h.availableTestRuns:
	default:
		cmdLog.Warn("Maximum concurrent test runs reached. Rejecting request.")
		resp.Header().Set("Retry-After", fmt.Sprintf("%d", h.getWaitTime()))
		http.Error(resp, "Maximum concurrent test runs reached", http.StatusTooManyRequests)
		return
	}
	logIfError := func(err error) {
		if err != nil {
			cmdLog.Error(err)
		}
	}

	cmdLog.Info("parsing the request payload")
	payload, err := newLaunchPayload(req)
	if err != nil {
		cmdLog.Error(err)
		http.Error(resp, fmt.Sprintf("error while validating request: %v", err), 400)
		return
	}

	// define the fail function
	// this function returns a 400 status and saves the failure time (to avoid retries, if the user has configured to do so)
	var buf bytes.Buffer
	fail := func(message string) {
		h.setLastFailureTime(payload)
		cmdLog.Error(message)
		if buf.Len() > 0 {
			message += "\n" + buf.String()
		}
		http.Error(resp, message, 400)
	}

	if v, ok := h.getLastFailureTime(payload); ok && time.Since(v) < payload.Metadata.MinFailureDelay {
		fail("not enough time since last failure")
		return
	}

	cmdLog.Info("fetching secrets (if any)")
	envVars, err := h.buildEnvVars(payload)
	if err != nil {
		fail(err.Error())
		return
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer func() {
		if payload.Metadata.WaitForResults {
			cancelCtx()
		}
	}()
	go func() {
		h.propagateCancel(req, payload, cancelCtx)
	}()

	cmdLog.Info("launching k6 test")
	cmd, err := h.client.Start(ctx, payload.Metadata.Script, payload.Metadata.UploadToCloud, envVars, &buf)
	if err != nil {
		fail(fmt.Sprintf("error while launching the test: %v", err))
		return
	}

	cmdLog.Info("waiting for output path")
	slackContext := payload.Metadata.NotificationContext
	// Find the Cloud URL from the k6 output
	if waitErr := h.waitForOutputPath(cmdLog, &buf); waitErr != nil {
		slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, payload.statusMessage(emojiFailure, "didn't start successfully"), slackContext)
		logIfError(err)
		logIfError(h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()))
		fail(fmt.Sprintf("error while waiting for test to start: %v", waitErr))
		h.registerProcessCleanup(cmd)
		return
	}

	if payload.Metadata.UploadToCloud {
		url, err := getCloudURL(buf.String())
		if err != nil {
			fail(err.Error())
			h.registerProcessCleanup(cmd)
			return
		}
		slackContext += fmt.Sprintf("\nCloud URL: <%s>", url)
		cmdLog.Infof("cloud run URL: %s", url)
	}

	// Write the initial message to each channel
	slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, payload.statusMessage(emojiWarning, "has started"), slackContext)
	logIfError(err)

	if !payload.Metadata.WaitForResults {
		cmdLog.Infof("the load test for %s.%s was launched successfully!", payload.Name, payload.Namespace)
		h.registerProcessCleanup(cmd)
		return
	}

	// Wait for the test to finish and write the output to slack
	defer func() {
		h.availableTestRuns <- struct{}{}
	}()
	cmdLog.Info("waiting for the results")
	err = cmd.Wait()
	h.trackExecutionDuration(cmd)
	logIfError(h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()))

	// Load testing failed, log the output
	if err != nil {
		logIfError(h.slackClient.UpdateMessages(slackMessages, payload.statusMessage(emojiFailure, "has failed"), slackContext))
		fail(fmt.Sprintf("failed to run: %v", err))
		return
	}

	// Success!
	logIfError(h.slackClient.UpdateMessages(slackMessages, payload.statusMessage(emojiSuccess, "has succeeded"), slackContext))
	_, err = resp.Write(buf.Bytes())
	logIfError(err)
	cmdLog.Infof("the load test for %s.%s succeeded!", payload.Name, payload.Namespace)
}

func (h *launchHandler) trackExecutionDuration(cmd k6.TestRun) {
	if dur := cmd.ExecutionDuration(); dur != 0 {
		h.metricTestDuration.With(prometheus.Labels{"exitCode": fmt.Sprintf("%d", cmd.ExitCode())}).Observe(float64(dur / time.Second))
	}
}

func (h *launchHandler) propagateCancel(req *http.Request, payload *launchPayload, cancelCtx context.CancelFunc) {
	if payload.Metadata.WaitForResults {
		select {
		case <-req.Context().Done():
			cancelCtx()
		case <-h.ctx.Done():
			cancelCtx()
		}
	} else {
		// If we are not waiting for the results then we should only cancel
		// if the global context is done:
		<-h.ctx.Done()
		cancelCtx()
	}
	log.Info("canceling process")
}

func (h *launchHandler) waitForOutputPath(cmdLog *log.Entry, buf *bytes.Buffer) error {
	for i := 0; i < 10; i++ {
		if strings.Contains(buf.String(), "output:") {
			return nil
		}
		cmdLog.Debug("waiting 2 seconds for test to start")
		h.sleep(2 * time.Second)
	}
	return errors.New("timeout")
}

func getCloudURL(output string) (string, error) {
	matches := outputRegex.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", errors.New("couldn't find the cloud URL in the output")
	}
	return matches[1], nil
}
