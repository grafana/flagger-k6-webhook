package k6

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type LocalRunnerClient struct {
	token string
}

func NewLocalRunnerClient(token string) (Client, error) {
	client := &LocalRunnerClient{token: token}
	return client, nil
}

type DefaultTestRun struct {
	*exec.Cmd
	startedAt     time.Time
	exitedAt      time.Time
	cancelContext context.CancelFunc
}

func (tr *DefaultTestRun) Start() error {
	tr.startedAt = time.Now()
	return tr.Cmd.Start()
}

func (tr *DefaultTestRun) Wait() error {
	defer func() {
		tr.exitedAt = time.Now()
	}()
	return tr.Cmd.Wait()
}

func (tr *DefaultTestRun) ExitCode() int {
	if tr.Cmd != nil && tr.Cmd.ProcessState != nil {
		return tr.Cmd.ProcessState.ExitCode()
	}
	return -1
}

func (tr *DefaultTestRun) CleanupContext() {
	if tr.cancelContext != nil {
		tr.cancelContext()
	}
}

func (tr *DefaultTestRun) ExecutionDuration() time.Duration {
	if tr.startedAt.IsZero() || tr.exitedAt.IsZero() {
		return time.Duration(0)
	}
	return tr.exitedAt.Sub(tr.startedAt)
}

func (tr *DefaultTestRun) Kill() error {
	if tr.Cmd != nil && tr.Cmd.Process != nil {
		return tr.Cmd.Process.Kill()
	}
	return nil
}

func (tr *DefaultTestRun) PID() int {
	if tr.Cmd != nil && tr.Cmd.Process != nil {
		return tr.Cmd.Process.Pid
	}
	return -1
}

func (tr *DefaultTestRun) Exited() bool {
	if tr.Cmd != nil && tr.Cmd.ProcessState != nil {
		return tr.Cmd.ProcessState.Exited()
	}
	return false
}

func (tr *DefaultTestRun) SetCancelFunc(fn context.CancelFunc) {
	tr.cancelContext = fn
}

func (c *LocalRunnerClient) Start(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (TestRun, error) {
	tempFile, err := os.CreateTemp("", "k6-script")
	if err != nil {
		return nil, fmt.Errorf("could not create a tempfile for the script: %w", err)
	}
	if _, err := tempFile.WriteString(scriptContent); err != nil {
		return nil, fmt.Errorf("could not write the script to a tempfile: %w", err)
	}

	args := []string{"run"}
	if upload {
		args = append(args, "--out", "cloud")
	}
	args = append(args, tempFile.Name())

	cmd := c.cmd(ctx, "k6", args...)
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	cmd.Env = os.Environ()
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	log.Debugf("launching 'k6 %s'", strings.Join(args, " "))
	run := &DefaultTestRun{Cmd: cmd}
	return run, run.Start()
}

func (c *LocalRunnerClient) cmd(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = append(os.Environ(), "K6_CLOUD_TOKEN="+c.token)

	return cmd
}
