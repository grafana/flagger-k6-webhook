package handlers

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
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

func createLogEntry(req *http.Request) *log.Entry {
	return log.WithFields(log.Fields{
		"requestID": uuid.NewString(),
		"command":   req.RequestURI,
		"ip":        req.RemoteAddr,
	})
}
