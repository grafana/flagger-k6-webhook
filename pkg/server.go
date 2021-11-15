package pkg

import (
	"fmt"
	"net/http"

	"github.com/grafana/flagger-k6-webhook/pkg/handlers"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

func Listen(client k6.Client, slackClient slack.Client, port int) error {
	gatherHandler, err := handlers.NewGatherHandler(client, slackClient)
	if err != nil {
		return err
	}
	launchHandler, err := handlers.NewLaunchHandler(client, slackClient)
	if err != nil {
		return err
	}

	serveAddress := fmt.Sprintf(":%d", port)
	logrus.Info("starting server at " + serveAddress)

	http.HandleFunc("/health", handlers.HandleHealth)
	http.Handle("/metrics", promhttp.Handler())

	http.Handle("/launch-test",
		promhttp.InstrumentHandlerCounter(
			promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "launch_requests_total",
					Help: "Total number of /launch-test requests by HTTP code.",
				},
				[]string{"code"},
			),
			launchHandler,
		),
	)

	http.Handle("/gather-results",
		promhttp.InstrumentHandlerCounter(
			promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "gather_requests_total",
					Help: "Total number of /gather-results requests by HTTP code.",
				},
				[]string{"code"},
			),
			gatherHandler,
		),
	)

	return http.ListenAndServe(serveAddress, nil)
}
