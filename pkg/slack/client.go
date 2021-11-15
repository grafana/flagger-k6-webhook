package slack

import (
	"fmt"

	"github.com/slack-go/slack"
)

type slackClientWrapper struct {
	client *slack.Client
}

func NewClient(token string) Client {
	if token == "" {
		return nil
	}

	return &slackClientWrapper{
		client: slack.New(token),
	}
}

func (w *slackClientWrapper) SendMessages(channels []string, text, context string) (map[string]string, error) {
	slackMessages := map[string]string{}
	for _, channel := range channels {
		channelId, ts, _, err := w.client.SendMessage(channel, messageBlocks(text, context))
		if err != nil {
			return nil, fmt.Errorf("error sending message to %s: %v", channel, err)
		}
		slackMessages[channelId] = ts
	}
	return slackMessages, nil
}

func (w *slackClientWrapper) UpdateMessages(slackMessages map[string]string, text, context string) error {
	for channelId, ts := range slackMessages {
		if _, _, _, err := w.client.UpdateMessage(channelId, ts, messageBlocks(text, context)); err != nil {
			return fmt.Errorf("error updating message %s in channel %s: %v", ts, channelId, err)
		}
	}
	return nil
}

func (w *slackClientWrapper) AddFileToThreads(slackMessages map[string]string, fileName, content string) error {
	for channelId, ts := range slackMessages {
		fileParams := slack.FileUploadParameters{
			Title:           fileName,
			Content:         content,
			Channels:        []string{channelId},
			ThreadTimestamp: ts,
		}
		if _, err := w.client.UploadFile(fileParams); err != nil {
			return fmt.Errorf("error while uploading output to %s in slack channel %s: %v", ts, channelId, err)
		}
	}

	return nil
}

func messageBlocks(text, context string) slack.MsgOption {
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil,
		),
	}
	if context != "" {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType, context, false, false),
		))
	}
	return slack.MsgOptionBlocks(blocks...)
}
