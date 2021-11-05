package k6

import (
	"os"
	"os/exec"
)

type Client struct {
	token string
}

func NewClient(token string) (*Client, error) {
	client := &Client{token: token}
	return client, nil
}

func (c *Client) cmd(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Env = append(os.Environ(), "K6_CLOUD_TOKEN="+c.token)
	return cmd
}
