package dingtalk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

func TestSendTextInjectsMentionMobiles(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, nil)

	if err := bot.SendText(context.Background(), "alert", msgbot.WithAtUsers("13800000000")); err != nil {
		t.Fatalf("send text: %v", err)
	}
	body := (*bodies)[0]
	if !strings.Contains(body, "@13800000000") {
		t.Fatalf("mobile must be injected into body: %s", body)
	}
	if !strings.Contains(body, `"atMobiles":["13800000000"]`) {
		t.Fatalf("atMobiles must be set: %s", body)
	}
}

func TestSendMarkdownInjectsMentionMobiles(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, nil)

	if err := bot.SendMarkdown(context.Background(), "title", "content", msgbot.WithAtUsers("13800000000")); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if !strings.Contains((*bodies)[0], "@13800000000") {
		t.Fatalf("mobile must be injected into markdown body: %s", (*bodies)[0])
	}
}

func TestSendMarkdownAtAll(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, nil)

	if err := bot.SendMarkdown(context.Background(), "title", "content", msgbot.WithAtAll()); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if !strings.Contains((*bodies)[0], `"isAtAll":true`) {
		t.Fatalf("isAtAll must be set: %s", (*bodies)[0])
	}
}

func TestSendMarkdownRequiresTitle(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, nil)

	err := bot.SendMarkdown(context.Background(), "", "content")
	if err == nil {
		t.Fatal("expected title validation error")
	}
	var e *msgbot.Error
	if !errors.As(err, &e) || e.Kind != msgbot.KindValidation {
		t.Fatalf("want KindValidation, got %v", err)
	}
	if len(*bodies) != 0 {
		t.Fatalf("validation failure must not send, got %d requests", len(*bodies))
	}
}

func TestSendRichTextRequiresTitle(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, nil)

	err := bot.SendRichText(context.Background(), &msgbot.RichTextMessage{})
	if err == nil {
		t.Fatal("expected rich text title validation error")
	}
	if len(*bodies) != 0 {
		t.Fatalf("validation failure must not send, got %d requests", len(*bodies))
	}
}
