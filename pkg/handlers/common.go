package handlers

import (
	"net/http"

	log "github.com/sirupsen/logrus"
)

type flaggerWebhook struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
}

func logError(req *http.Request, resp http.ResponseWriter, err string, code int) {
	log.WithField("from", req.RemoteAddr).WithField("command", req.RequestURI).Error(err)
	http.Error(resp, err, code)
}
