package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type launchPayload struct {
	flaggerWebhook
	Metadata struct {
		Script               string `json:"script"`
		UploadToCloudString  string `json:"upload_to_cloud"`
		UploadToCloud        bool
		WaitForResultsString string `json:"wait_for_results"`
		WaitForResults       bool
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

	return payload, nil
}

type launchHandler struct {
	client *k6.Client
}

// NewLaunchHandler returns an handler that launches a k6 load test
func NewLaunchHandler(client *k6.Client) (http.HandlerFunc, error) {
	handler := &launchHandler{
		client: client,
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

	if err := h.client.Run(payload.Metadata.Script, payload.Metadata.UploadToCloud, payload.Metadata.WaitForResults); err != nil {
		logError(req, resp, fmt.Sprintf("error while gathering results: %v", err), 400)
		return
	}

	log.WithField("command", req.RequestURI).Infof("the load test for %s.%s succeeded!", payload.Name, payload.Namespace)
}
