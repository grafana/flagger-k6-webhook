package slack

import (
	log "github.com/sirupsen/logrus"
)

type noopClient struct{}

func (c *noopClient) SendMessages(channels []string, text, context string) (map[string]string, error) {
	if len(channels) > 0 {
		log.Debugf("Slack disabled. Would've sent the following message: %s", text)
	}
	return nil, nil
}

func (c *noopClient) UpdateMessages(slackMessages map[string]string, text, context string) error {
	if len(slackMessages) > 0 {
		log.Debugf("Slack disabled. Would've updated messages to: %s", text)
	}
	return nil
}

func (c *noopClient) AddFileToThreads(slackMessages map[string]string, fileName, content string) error {
	if len(slackMessages) > 0 {
		log.Debugf("Slack disabled. Would've uploaded file named: %s", fileName)
	}
	return nil
}
