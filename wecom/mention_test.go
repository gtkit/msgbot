package wecom

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	news "github.com/gtkit/msgbot"
)

func TestSendTextMergesMentions(t *testing.T) {
	bot, bodies := newWeComTestWebhook(t, nil)

	if err := bot.SendText(context.Background(), "hi", news.WithAtAll(), news.WithAtUsers("u1")); err != nil {
		t.Fatalf("send text: %v", err)
	}
	body := (*bodies)[0]
	if !strings.Contains(body, `"@all"`) {
		t.Fatalf("missing @all in mentioned_list: %s", body)
	}
	if !strings.Contains(body, `"u1"`) {
		t.Fatalf("missing user in mentioned_list: %s", body)
	}
}

func TestSendMarkdownIgnoresMentionsButSends(t *testing.T) {
	logger := &captureLogger{}
	var body string
	bot, err := New(news.Config{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test",
		Logger:     logger,
		HTTPClient: &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"errcode":0}`)), Request: req}, nil
		})},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := bot.SendMarkdown(context.Background(), "title", "content", news.WithAtAll()); err != nil {
		t.Fatalf("markdown with @ options must still send: %v", err)
	}
	if !strings.Contains(body, `"msgtype":"markdown"`) {
		t.Fatalf("message not sent: %s", body)
	}
	if logger.debugCount() == 0 {
		t.Fatal("expected a debug log noting the ignored @ option")
	}
}

type captureLogger struct {
	mu    sync.Mutex
	debug int
}

func (l *captureLogger) DebugContext(context.Context, string, ...any) {
	l.mu.Lock()
	l.debug++
	l.mu.Unlock()
}

func (l *captureLogger) ErrorContext(context.Context, string, ...any) {}

func (l *captureLogger) debugCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debug
}
