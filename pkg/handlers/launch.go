package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
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

		// Min delay between runs. All other runs will fail immediately
		MinDelay       time.Duration
		MinDelayString string `json:"min_delay"`
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

	if payload.Metadata.MinDelayString == "" {
		payload.Metadata.MinDelay = 5 * time.Minute
	} else if payload.Metadata.MinDelay, err = time.ParseDuration(payload.Metadata.MinDelayString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'min_delay': %w", err)
	}

	return payload, nil
}

type launchHandler struct {
	client      k6.Client
	slackClient slack.Client

	lastRunTime      map[string]time.Time
	lastRunTimeMutex sync.Mutex

	// mockables
	sleep func(time.Duration)
}

// NewLaunchHandler returns an handler that launches a k6 load test
func NewLaunchHandler(client k6.Client, slackClient slack.Client) (http.Handler, error) {
	return &launchHandler{
		client:      client,
		slackClient: slackClient,
		lastRunTime: make(map[string]time.Time),
		sleep:       time.Sleep,
	}, nil
}

func (h *launchHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	payload, err := newLaunchPayload(req)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while validating request: %v", err), 400)
		return
	}

	runKey := payload.Namespace + "-" + payload.Name + "-" + payload.Phase

	h.lastRunTimeMutex.Lock()
	if v, ok := h.lastRunTime[runKey]; ok && time.Since(v) < payload.Metadata.MinDelay {
		logError(req, resp, "not enough time since last run", 400)
		h.lastRunTimeMutex.Unlock()
		return
	}
	h.lastRunTime[runKey] = time.Now()
	h.lastRunTimeMutex.Unlock()

	var buf bytes.Buffer
	cmd, err := h.client.Start(payload.Metadata.Script, payload.Metadata.UploadToCloud, &buf)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while launching the test: %v", err), 400)
		return
	}

	slackContext := payload.Metadata.NotificationContext
	// Find the Cloud URL from the k6 output
	if waitErr := h.waitForOutputPath(&buf); waitErr != nil {
		logError(req, resp, fmt.Sprintf("error while waiting for test to start: %v", waitErr), 400)
		text := fmt.Sprintf(":red_circle: Load testing of `%s` in namespace `%s` didn't start successfully", payload.Name, payload.Namespace)
		slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, text, slackContext)
		if err != nil {
			log.Error(err)
		}
		if err := h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()); err != nil {
			log.Error(err)
		}
		return
	}

	url := ""
	if payload.Metadata.UploadToCloud {
		url = outputRegex.FindStringSubmatch(buf.String())[1]
		slackContext += fmt.Sprintf("\nCloud URL: <%s>", url)
		log.Infof("cloud run URL: %s", url)
	}

	// Write the initial message to each channel
	text := fmt.Sprintf(":warning: Load testing of `%s` in namespace `%s` has started", payload.Name, payload.Namespace)
	slackMessages, err := h.slackClient.SendMessages(payload.Metadata.SlackChannels, text, slackContext)
	if err != nil {
		log.Error(err)
	}

	if !payload.Metadata.WaitForResults {
		log.WithField("command", req.RequestURI).Infof("the load test for %s.%s was launched successfully!", payload.Name, payload.Namespace)
		return
	}

	// Wait for the test to finish and write the output to slack
	cmdErr := cmd.Wait()
	if err := h.slackClient.AddFileToThreads(slackMessages, "k6-results.txt", buf.String()); err != nil {
		log.Error(err)
	}

	// Load testing failed, log the output
	if cmdErr != nil {
		fmt.Fprint(os.Stderr, buf.String())
		if err := h.slackClient.UpdateMessages(slackMessages, fmt.Sprintf(":red_circle: Load testing of `%s` in namespace `%s` has failed", payload.Name, payload.Namespace), slackContext); err != nil {
			log.Error(err)
		}

		logError(req, resp, fmt.Sprintf("failed to run: %v", cmdErr), 400)
		return
	}

	// Success!
	if err := h.slackClient.UpdateMessages(slackMessages, fmt.Sprintf(":large_green_circle: Load testing of `%s` in namespace `%s` has succeeded", payload.Name, payload.Namespace), slackContext); err != nil {
		log.Error(err)
	}
	log.WithField("command", req.RequestURI).Infof("the load test for %s.%s succeeded!", payload.Name, payload.Namespace)
}

func (h *launchHandler) waitForOutputPath(buf *bytes.Buffer) error {
	for i := 0; i < 10; i++ {
		if strings.Contains(buf.String(), "output:") {
			return nil
		}
		log.Debug("waiting 2 seconds for test to start")
		h.sleep(2 * time.Second)
	}
	return errors.New("timeout")
}
