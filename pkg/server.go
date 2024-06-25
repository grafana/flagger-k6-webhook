package pkg

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/flagger-k6-webhook/pkg/handlers"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

func Listen(ctx context.Context, client k6.Client, kubeClient kubernetes.Interface, slackClient slack.Client, port int, maxProcessHandlers int) error {
	launcherCtx, cancelLaunchCtx := context.WithCancel(ctx)
	launchHandler, err := handlers.NewLaunchHandler(launcherCtx, client, kubeClient, slackClient, maxProcessHandlers)
	defer func() {
		logrus.Debug("shutting down launch handler")
		cancelLaunchCtx()
		launchHandler.Wait()
	}()
	if err != nil {
		return err
	}

	serveAddress := fmt.Sprintf(":%d", port)
	logrus.Info("starting server at " + serveAddress)

	mux := http.NewServeMux()
	srv := http.Server{
		Handler: mux,
		Addr:    serveAddress,
	}

	go func() {
		<-ctx.Done()
		cancelLaunchCtx()
		timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		_ = srv.Shutdown(timeoutCtx)
	}()

	mux.HandleFunc("/health", handlers.HandleHealth)
	mux.Handle("/metrics", promhttp.Handler())

	mux.Handle("/launch-test",
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

	return srv.ListenAndServe()
}
