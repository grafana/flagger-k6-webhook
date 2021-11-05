package k6

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func (c *Client) Run(scriptContent string, upload bool, wait bool) error {
	tempFile, err := os.CreateTemp("", "k6-script")
	if err != nil {
		return fmt.Errorf("could not create a tempfile for the script: %v", err)
	}
	if _, err := tempFile.WriteString(scriptContent); err != nil {
		return fmt.Errorf("could not write the script to a tempfile: %v", err)
	}

	args := []string{"run"}
	if upload {
		args = append(args, "--out", "cloud")
	}
	args = append(args, tempFile.Name())

	cmd := c.cmd("k6", args...)
	cmdString := "k6 " + strings.Join(args, " ")
	if log.GetLevel() == log.DebugLevel {
		fmt.Fprintln(os.Stderr, string(scriptContent))
	}

	if !wait {
		log.Infof("launching '%s' asynchronously", cmdString)
		return cmd.Start()
	}

	log.Infof("launching 'k6 %s'", strings.Join(args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run '%s'\nerr: %v\nout:\n%s", cmdString, err, output)
	}

	if log.GetLevel() == log.DebugLevel {
		fmt.Fprintln(os.Stderr, string(output))
	}

	return nil
}
