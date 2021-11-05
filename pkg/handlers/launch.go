package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

// https://regex101.com/r/Pn7VUB/1
var outputRegex = regexp.MustCompile(`output: cloud \((?P<url>https:\/\/app\.k6\.io\/runs\/\d+)\)`)

type launchPayload struct {
	flaggerWebhook
	Metadata struct {
		Script               string `json:"script"`
		UploadToCloudString  string `json:"upload_to_cloud"`
		UploadToCloud        bool
		WaitForResultsString string `json:"wait_for_results"`
		WaitForResults       bool
		SlackChannelsString  string `json:"slack_channels"`
		SlackChannels        []string
	} `json:"metadata"`
}

func newLaunchPayload(req *http.Request) (*launchPayload, error) {
	var err error
	payload := &launchPayload{}

	defer req.Body.Close()
	if err = json.NewDecoder(req.Body).Decode(payload); err != nil {
		return nil, err
	}

	if payload.Metadata.UploadToCloudString == "" {
		payload.Metadata.UploadToCloud = false
	} else if payload.Metadata.UploadToCloud, err = strconv.ParseBool(payload.Metadata.UploadToCloudString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'upload_to_cloud': %v", err)
	}

	if payload.Metadata.WaitForResultsString == "" {
		payload.Metadata.WaitForResults = true
	} else if payload.Metadata.WaitForResults, err = strconv.ParseBool(payload.Metadata.WaitForResultsString); err != nil {
		return nil, fmt.Errorf("error parsing value for 'wait_for_results': %v", err)
	}

	if payload.Metadata.SlackChannelsString != "" {
		payload.Metadata.SlackChannels = strings.Split(payload.Metadata.SlackChannelsString, ",")
	}

	return payload, nil
}

type launchHandler struct {
	client      *k6.Client
	slackClient *slack.Client
}

// NewLaunchHandler returns an handler that launches a k6 load test
func NewLaunchHandler(client *k6.Client, slackClient *slack.Client) (http.HandlerFunc, error) {
	handler := &launchHandler{
		client:      client,
		slackClient: slackClient,
	}

	return promhttp.InstrumentHandlerCounter(
		promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "launch_requests_total",
				Help: "Total number of /launch-test requests by HTTP code.",
			},
			[]string{"code"},
		),
		handler,
	), nil
}

func (h *launchHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	payload, err := newLaunchPayload(req)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while validating request: %v", err), 400)
		return
	}

	var buf bytes.Buffer
	cmd, err := h.client.Start(payload.Metadata.Script, payload.Metadata.UploadToCloud, &buf)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while launching the test: %v", err), 400)
		return
	}

	if !payload.Metadata.WaitForResults {
		log.WithField("command", req.RequestURI).Infof("the load test for %s.%s was launched successfully!", payload.Name, payload.Namespace)
		return
	}

	for i := 0; i < 6; i++ {
		if !strings.Contains(buf.String(), "output:") {
			log.Debug("waiting 2 seconds for test to start")
			time.Sleep(2 * time.Second)
			continue
		}

		break
	}

	url := ""
	if payload.Metadata.UploadToCloud {
		url = outputRegex.FindStringSubmatch(buf.String())[1]
		log.Infof("cloud run URL: %s", url)
	}

	// Writing the initial message to each channel
	slackMessages := map[string]string{}
	for _, channel := range payload.Metadata.SlackChannels {
		channelId, ts, _, err := h.slackClient.SendMessage(channel, slack.MsgOptionBlocks(slackBlocks(*payload, ":warning:", "started", url)...))
		if err != nil {
			log.WithField("channel", channel).Errorf("error while sending message to slack: %v", err)
			continue
		}
		slackMessages[channelId] = ts
	}

	cmdErr := cmd.Wait()

	for channelId, ts := range slackMessages {
		fileParams := slack.FileUploadParameters{
			Title:           "Output",
			Filetype:        "txt",
			Content:         buf.String(),
			Channels:        []string{channelId},
			ThreadTimestamp: ts,
		}
		_, err = h.slackClient.UploadFile(fileParams)
		if err != nil {
			log.Errorf("error while uploading output to slack: %v", err)
		}
	}

	if cmdErr != nil {
		fmt.Fprint(os.Stderr, buf.String())

		for channelId, ts := range slackMessages {
			if _, _, _, err := h.slackClient.UpdateMessage(channelId, ts, slack.MsgOptionBlocks(slackBlocks(*payload, ":red_circle:", "failed", url)...)); err != nil {
				log.WithField("channel", channelId).Errorf("error while sending message to slack: %v", err)
				continue
			}
		}

		logError(req, resp, fmt.Sprintf("failed to run: %v", cmdErr), 400)
		return
	}

	for channelId, ts := range slackMessages {
		_, _, _, err := h.slackClient.UpdateMessage(channelId, ts, slack.MsgOptionBlocks(slackBlocks(*payload, ":large_green_circle:", "succeeded", url)...))
		if err != nil {
			log.WithField("channel", channelId).Errorf("error while sending message to slack: %v", err)
			continue
		}
	}

	log.WithField("command", req.RequestURI).Infof("the load test for %s.%s succeeded!", payload.Name, payload.Namespace)
}

func slackBlocks(payload launchPayload, emoji, status, url string) []slack.Block {
	text := fmt.Sprintf("%s Load testing of `%s` in namespace `%s` has %s", emoji, payload.Name, payload.Namespace, status)

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil,
		),
	}
	if url != "" {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("Cloud URL: <%s>", url), false, false),
		))
	}
	return blocks
}
