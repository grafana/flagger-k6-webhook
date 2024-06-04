package k6

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

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

func (c *LocalRunnerClient) Start(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (TestRun, error) {
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

	cmd := c.cmd("k6", args...)
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	cmd.Env = os.Environ()
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	log.Debugf("launching 'k6 %s'", strings.Join(args, " "))
	return &DefaultTestRun{cmd}, cmd.Start()
}

func (c *LocalRunnerClient) cmd(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Env = append(os.Environ(), "K6_CLOUD_TOKEN="+c.token)

	return cmd
}
