package slack

//go:generate mockgen -destination=../mocks/mock_slack_client.go -package=mocks -mock_names=Client=MockSlackClient github.com/grafana/flagger-k6-webhook/pkg/slack Client

type Client interface {
	SendMessages(channels []string, text, context string) (map[string]string, error)
	UpdateMessages(slackMessages map[string]string, text, context string) error
	AddFileToThreads(slackMessages map[string]string, fileName, content string) error
}
