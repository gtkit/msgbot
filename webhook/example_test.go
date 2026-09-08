package webhook_test

import (
	"fmt"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/webhook"
)

func ExampleNew() {
	// payload 完全由调用方决定：按 Kind 处理关心的消息类型，对其余返回错误。
	hook, err := webhook.New(
		msgbot.Config{WebhookURL: "https://hooks.example.com/alert"},
		func(m *webhook.Message) (any, error) {
			switch m.Kind {
			case webhook.KindText:
				return map[string]any{"text": m.Text, "at_all": m.Options.AtAll}, nil
			case webhook.KindMarkdown:
				return map[string]any{"title": m.Title, "body": m.Content}, nil
			default:
				return nil, fmt.Errorf("端点不支持 %s 消息", m.Kind)
			}
		},
	)
	if err != nil {
		return
	}

	fmt.Println(hook.Platform())
	// Output: webhook
}
