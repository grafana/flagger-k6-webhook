package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	log "github.com/sirupsen/logrus"
)

type gatherPayload struct {
	flaggerWebhook
	Metadata struct {
	} `json:"metadata"`
}

func newGatherPayload(req *http.Request) (*gatherPayload, error) {
	payload := &gatherPayload{}

	if req.Body == nil {
		return nil, errors.New("no request body")
	}
	defer req.Body.Close()
	if err := json.NewDecoder(req.Body).Decode(payload); err != nil {
		return nil, err
	}

	if err := payload.validateBaseWebhook(); err != nil {
		return nil, fmt.Errorf("error while validating base webhook: %v", err)
	}

	return payload, nil
}

type gatherHandler struct {
	client      k6.Client
	slackClient slack.Client
}

// NewGatherHandler returns an handler that gathers test results
// This is needed for longer test runs.
func NewGatherHandler(client k6.Client, slackClient slack.Client) (http.Handler, error) {
	return &gatherHandler{
		client:      client,
		slackClient: slackClient,
	}, nil
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
