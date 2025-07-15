package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/grafana/flagger-k6-webhook/pkg"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultPort               = 8000
	defaultMaxConcurrentTests = 1000

	flagCloudToken         = "cloud-token"
	flagLogLevel           = "log-level"
	flagListenPort         = "listen-port"
	flagSlackToken         = "slack-token"
	flagKubernetesClient   = "kubernetes-client"
	flagMaxConcurrentTests = "max-concurrent-tests"
	flagVersion            = "version"

	kubernetesClientNone      = "none"
	kubernetesClientInCluster = "in-cluster"
)

func main() {
	if err := run(os.Args); err != nil {
		log.Fatalf("execution failed: %s", err)
	}
}

func run(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	app := cli.Command{}
	app.Name = "flagger-k6-webhook"
	app.Usage = "Launches k6 load testing from a flagger webhook"
	app.Action = launchServer

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    flagCloudToken,
			Sources: cli.EnvVars("K6_CLOUD_TOKEN"),
		},
		&cli.IntFlag{
			Name:    flagListenPort,
			Sources: cli.EnvVars("LISTEN_PORT"),
			Value:   defaultPort,
		},
		&cli.StringFlag{
			Name:    flagLogLevel,
			Sources: cli.EnvVars("LOG_LEVEL"),
			Value:   log.InfoLevel.String(),
		},
		&cli.StringFlag{
			Name:    flagSlackToken,
			Sources: cli.EnvVars("SLACK_TOKEN"),
		},
		&cli.StringFlag{
			Name:    flagKubernetesClient,
			Sources: cli.EnvVars("KUBERNETES_CLIENT"),
			Value:   kubernetesClientNone,
			Usage:   fmt.Sprintf("Kubernetes client to use: '%s' or '%s'", kubernetesClientInCluster, kubernetesClientNone),
		},
		&cli.IntFlag{
			Name:    flagMaxConcurrentTests,
			Sources: cli.EnvVars("MAX_CONCURRENT_TESTS"),
			Value:   defaultMaxConcurrentTests,
		},
		&cli.BoolFlag{
			Name:  flagVersion,
			Value: false,
		},
	}

	return app.Run(ctx, args)
}

func launchServer(ctx context.Context, c *cli.Command) error {
	log.Info("Version ", version.Info())
	log.Info("Build Context ", version.BuildContext())
	if err := prometheus.Register(versioncollector.NewCollector("flagger_k6_webhook")); err != nil {
		return err
	}
	if c.Bool(flagVersion) {
		return nil
	}

	logLevel, err := log.ParseLevel(c.String(flagLogLevel))
	if err != nil {
		return err
	}
	log.SetLevel(logLevel)

	client, err := k6.NewLocalRunnerClient(c.String(flagCloudToken))
	if err != nil {
		return err
	}
	slackClient := slack.NewClient(c.String(flagSlackToken))

	var kubeClient kubernetes.Interface
	if c.String(flagKubernetesClient) == kubernetesClientInCluster {
		log.Info("creating in-cluster kubernetes client")
		kubeConfig, err := rest.InClusterConfig()
		if err != nil {
			return err
		}
		if kubeClient, err = kubernetes.NewForConfig(kubeConfig); err != nil {
			return err
		}
	} else {
		log.Info("not creating a kubernetes client")
	}

	return pkg.Listen(ctx, client, kubeClient, slackClient, c.Int(flagListenPort), c.Int(flagMaxConcurrentTests))
}
