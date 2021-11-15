package handlers

import (
	"errors"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type flaggerWebhook struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
}

func (w *flaggerWebhook) validateBaseWebhook() error {
	if w.Name == "" {
		return errors.New("missing name")
	}
	if w.Namespace == "" {
		return errors.New("missing namespace")
	}
	if w.Phase == "" {
		return errors.New("missing phase")
	}
	return nil
}

func logError(req *http.Request, resp http.ResponseWriter, err string, code int) {
	log.WithField("from", req.RemoteAddr).WithField("command", req.RequestURI).Error(err)
	http.Error(resp, err, code)
}
