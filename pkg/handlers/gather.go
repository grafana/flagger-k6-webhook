package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

type gatherPayload struct {
	flaggerWebhook
	Metadata struct {
	} `json:"metadata"`
}

func newGatherPayload(req *http.Request) (*gatherPayload, error) {
	payload := &gatherPayload{}

	defer req.Body.Close()
	if err := json.NewDecoder(req.Body).Decode(payload); err != nil {
		return nil, err
	}

	return payload, nil
}

type gatherHandler struct {
	client      *k6.Client
	slackClient *slack.Client
}

// NewGatherHandler returns an handler that gathers test results
// This is needed for longer test runs.
func NewGatherHandler(client *k6.Client, slackClient *slack.Client) (http.HandlerFunc, error) {
	handler := &gatherHandler{
		client:      client,
		slackClient: slackClient,
	}

	return promhttp.InstrumentHandlerCounter(
		promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gather_requests_total",
				Help: "Total number of /gather-results requests by HTTP code.",
			},
			[]string{"code"},
		),
		handler,
	), nil
}

func (rh *gatherHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	payload, err := newGatherPayload(req)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while validating request: %v", err), 400)
		return
	}

	// TODO
	err = fmt.Errorf("gather not implemented. Payload: %v", payload)
	if err != nil {
		logError(req, resp, fmt.Sprintf("error while gathering results: %v", err), 400)
		return
	}

	log.WithField("command", req.RequestURI).Infof("the load test for %s succeeded!", "deployment name")
}
