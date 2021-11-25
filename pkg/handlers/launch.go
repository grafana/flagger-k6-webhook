package handlers

import (
	"bytes"
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
	log "github.com/sirupsen/logrus"
)

// https://regex101.com/r/Pn7VUB/1
var outputRegex = regexp.MustCompile(`output: cloud \((?P<url>https:\/\/app\.k6\.io\/runs\/\d+)\)`)

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
	} `json:"metadata"`
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

	if payload.Metadata.Script == "" {
		return nil, errors.New("missing script")
	}

	if payload.Metadata.UploadToCloudString == "" {
		payload.Metadata.UploadToCloud = false
	} else if payload.Metadata.UploadToCloud, err = strconv.ParseBool(payload.Metadata.UploadToCloudString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'upload_to_cloud': %w", err)
	}

	if payload.Metadata.WaitForResultsString == "" {
		payload.Metadata.WaitForResults = true
	} else if payload.Metadata.WaitForResults, err = strconv.ParseBool(payload.Metadata.WaitForResultsString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'wait_for_results': %w", err)
	}

	if payload.Metadata.SlackChannelsString != "" {
		payload.Metadata.SlackChannels = strings.Split(payload.Metadata.SlackChannelsString, ",")
	}

	if payload.Metadata.MinFailureDelayString == "" {
		payload.Metadata.MinFailureDelay = 2 * time.Minute
	} else if payload.Metadata.MinFailureDelay, err = time.ParseDuration(payload.Metadata.MinFailureDelayString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'min_failure_delay': %w", err)
	}

	return payload, nil
}

type launchHandler struct {
	client      k6.Client
	slackClient slack.Client

	lastFailureTime      map[string]time.Time
	lastFailureTimeMutex sync.Mutex

	// mockables
	sleep func(time.Duration)
}

// NewLaunchHandler returns an handler that launches a k6 load test.
func NewLaunchHandler(client k6.Client, slackClient slack.Client) (http.Handler, error) {
	return &launchHandler{
		client:          client,
		slackClient:     slackClient,
		lastFailureTime: make(map[string]time.Time),
		sleep:           time.Sleep,
	}, nil
}

func (h *launchHandler) getLastFailureTime(runKey string) (time.Time, bool) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	v, ok := h.lastFailureTime[runKey]
	return v, ok
}

func (h *launchHandler) setLastFailureTime(runKey string) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	h.lastFailureTime[runKey] = time.Now()
}

func (h *launchHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cmdLog := createLogEntry(req)
	logError := func(err error) {
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
	runKey := payload.Namespace + "-" + payload.Name + "-" + payload.Phase
	fail := func(message string) {
		h.setLastFailureTime(runKey)
		cmdLog.Error(message)
		if buf.Len() > 0 {
			message += "\n" + buf.String()
		}
		http.Error(resp, message, 400)
	}

	if v, ok := h.getLastFailureTime(runKey); ok && time.Since(v) < payload.Metadata.MinFailureDelay {
		fail("not enough time since last failure")
		return
	}

	cmdLog.Info("launching k6 test")
	cmd, err := h.client.Start(payload.Metadata.Script, payload.Metadata.UploadToCloud, &buf)
	if err != nil {
		fail(fmt.Sprintf("error while launching the test: %v", err))
		return
	}

	cmdLog.Info("waiting for output path")
	slackContext := payload.Metadata.NotificationContext
	// Find the Cloud URL from the k6 output
	if waitErr := h.waitForOutputPath(cmdLog, &buf); waitErr != nil {
		text := fmt.Sprintf(":red_circle: Load testing of `%s` in namespace `%s` didn't start successfully", payload.Name, payload.Namespace)
		slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, text, slackContext)
		logError(err)
		logError(h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()))
		fail(fmt.Sprintf("error while waiting for test to start: %v", waitErr))
		return
	}

	if payload.Metadata.UploadToCloud {
		matches := outputRegex.FindStringSubmatch(buf.String())
		if len(matches) < 2 {
			fail("couldn't find the cloud URL in the output")
			return
		}
		url := matches[1]
		slackContext += fmt.Sprintf("\nCloud URL: <%s>", url)
		cmdLog.Infof("cloud run URL: %s", url)
	}

	// Write the initial message to each channel
	text := fmt.Sprintf(":warning: Load testing of `%s` in namespace `%s` has started", payload.Name, payload.Namespace)
	slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, text, slackContext)
	logError(err)

	if !payload.Metadata.WaitForResults {
		cmdLog.Infof("the load test for %s.%s was launched successfully!", payload.Name, payload.Namespace)
		return
	}

	// Wait for the test to finish and write the output to slack
	cmdLog.Info("waiting for the results")
	err = cmd.Wait()
	logError(h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()))

	// Load testing failed, log the output
	if err != nil {
		logError(h.slackClient.UpdateMessages(slackMessages, fmt.Sprintf(":red_circle: Load testing of `%s` in namespace `%s` has failed", payload.Name, payload.Namespace), slackContext))
		fail(fmt.Sprintf("failed to run: %v", err))
		return
	}

	// Success!
	logError(h.slackClient.UpdateMessages(slackMessages, fmt.Sprintf(":large_green_circle: Load testing of `%s` in namespace `%s` has succeeded", payload.Name, payload.Namespace), slackContext))
	_, err = resp.Write(buf.Bytes())
	logError(err)
	cmdLog.Infof("the load test for %s.%s succeeded!", payload.Name, payload.Namespace)
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
