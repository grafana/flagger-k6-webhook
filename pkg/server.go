package pkg

import (
	"fmt"
	"net/http"

	"github.com/grafana/flagger-k6-webhook/pkg/handlers"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

func Listen(client *k6.Client, port int) error {
	gatherHandler, err := handlers.NewGatherHandler(client)
	if err != nil {
		return err
	}
	launchHandler, err := handlers.NewLaunchHandler(client)
	if err != nil {
		return err
	}

	serveAddress := fmt.Sprintf(":%d", port)
	logrus.Info("starting server at " + serveAddress)

	http.HandleFunc("/health", handlers.HandleHealth)
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/launch-test", launchHandler)
	http.Handle("/gather-results", gatherHandler)

	return http.ListenAndServe(serveAddress, nil)
}
