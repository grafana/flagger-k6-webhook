package k6

//go:generate mockgen -destination=../mocks/mock_k6_client.go -package=mocks -mock_names=Client=MockK6Client,TestRun=MockK6TestRun github.com/grafana/flagger-k6-webhook/pkg/k6 Client,TestRun

import (
	"context"
	"io"
	"time"
)

type Client interface {
	Start(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (TestRun, error)
}

type TestRun interface {
	Wait() error
	Kill() error
	PID() int
	Exited() bool
	ExitCode() int
	ExecutionDuration() time.Duration
}
