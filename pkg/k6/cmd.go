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
	return cmd, cmd.Start()
}

func (c *LocalRunnerClient) cmd(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Env = append(os.Environ(), "K6_CLOUD_TOKEN="+c.token)

	return cmd
}
