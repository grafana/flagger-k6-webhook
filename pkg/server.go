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

	return http.ListenAndServe(serveAddress, nil)
}
