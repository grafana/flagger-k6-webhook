package main

import (
	"fmt"
	"os"

	"github.com/grafana/flagger-k6-webhook/pkg"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultPort = 80

	flagCloudToken       = "cloud-token"
	flagLogLevel         = "log-level"
	flagListenPort       = "listen-port"
	flagSlackToken       = "slack-token"
	flagKubernetesClient = "kubernetes-client"

	kubernetesClientNone      = "none"
	kubernetesClientInCluster = "in-cluster"
)

func main() {
	if err := run(os.Args); err != nil {
		log.Fatalf("execution failed: %s", err)
	}
}

func run(args []string) error {
	app := cli.NewApp()
	app.Name = "flagger-k6-webhook"
	app.Usage = "Launches k6 load testing from a flagger webhook"
	app.Action = launchServer

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    flagCloudToken,
			EnvVars: []string{"K6_CLOUD_TOKEN"},
		},
		&cli.IntFlag{
			Name:    flagListenPort,
			EnvVars: []string{"LISTEN_PORT"},
			Value:   defaultPort,
		},
		&cli.StringFlag{
			Name:    flagLogLevel,
			EnvVars: []string{"LOG_LEVEL"},
			Value:   log.InfoLevel.String(),
		},
		&cli.StringFlag{
			Name:    flagSlackToken,
			EnvVars: []string{"SLACK_TOKEN"},
		},
		&cli.StringFlag{
			Name:    flagKubernetesClient,
			EnvVars: []string{"KUBERNETES_CLIENT"},
			Value:   kubernetesClientNone,
			Usage:   fmt.Sprintf("Kubernetes client to use: '%s' or '%s'", kubernetesClientInCluster, kubernetesClientNone),
		},
	}

	return app.Run(args)
}

func launchServer(c *cli.Context) error {
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

	return pkg.Listen(client, kubeClient, slackClient, c.Int(flagListenPort))
}
