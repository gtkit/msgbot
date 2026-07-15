package feishu

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

func newMentionWebhook(t *testing.T, rt *recordingRoundTripper) *Webhook {
	t.Helper()
	bot, err := New(msgbot.Config{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		HTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}
	return bot
}

func textContent(t *testing.T, body []byte) string {
	t.Helper()
	payload := decodeMap(t, body)
	content, ok := payload["content"].(map[string]any)
	if !ok {
		t.Fatalf("content is %T, want object", payload["content"])
	}
	text, ok := content["text"].(string)
	if !ok {
		t.Fatalf("text is %T, want string", content["text"])
	}
	return text
}

func TestSendTextEscapesMentionedUser(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{{status: http.StatusOK, body: `{"code":0}`}}}
	bot := newMentionWebhook(t, rt)

	if err := bot.SendText(context.Background(), "hi", msgbot.WithAtUsers(`ou_"evil`)); err != nil {
		t.Fatalf("send text: %v", err)
	}
	text := textContent(t, rt.requests[0].body)
	if strings.Contains(text, `ou_"evil`) {
		t.Fatalf("raw quote must not appear in tag: %s", text)
	}
	if !strings.Contains(text, "&quot;") {
		t.Fatalf("quote should be escaped: %s", text)
	}
}

func TestSendTextMergesAtAllAndUsers(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{{status: http.StatusOK, body: `{"code":0}`}}}
	bot := newMentionWebhook(t, rt)

	if err := bot.SendText(context.Background(), "hi", msgbot.WithAtAll(), msgbot.WithAtUsers("ou_1")); err != nil {
		t.Fatalf("send text: %v", err)
	}
	text := textContent(t, rt.requests[0].body)
	if !strings.Contains(text, `<at user_id="all">`) {
		t.Fatalf("missing at-all tag: %s", text)
	}
	if !strings.Contains(text, `<at user_id="ou_1">`) {
		t.Fatalf("missing at-user tag: %s", text)
	}
}

func TestSendMarkdownUsesCardMentionSyntax(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{{status: http.StatusOK, body: `{"code":0}`}}}
	bot := newMentionWebhook(t, rt)

	if err := bot.SendMarkdown(context.Background(), "title", "body", msgbot.WithAtAll(), msgbot.WithAtUsers("ou_1")); err != nil {
		t.Fatalf("send markdown: %v", err)
	}

	payload := decodeMap(t, rt.requests[0].body)
	card, ok := payload["card"].(map[string]any)
	if !ok {
		t.Fatalf("card is %T", payload["card"])
	}
	elements, ok := card["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("elements missing: %v", card["elements"])
	}
	content, _ := elements[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, "<at id=all></at>") {
		t.Fatalf("missing card at-all: %s", content)
	}
	if !strings.Contains(content, "<at id=ou_1></at>") {
		t.Fatalf("missing card at-user: %s", content)
	}
	// 卡片 @ 不能使用文本消息的 user_id 语法。
	if strings.Contains(content, "user_id=") {
		t.Fatalf("card must not use text-message mention syntax: %s", content)
	}
}

func TestNewAppWithTokenSource(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{{status: http.StatusOK, body: `{"code":0}`}}}
	app, err := NewAppWithTokenSource(func(context.Context) (*AccessToken, error) {
		return &AccessToken{TenantAccessToken: "fresh-token"}, nil
	}, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("new app with token source: %v", err)
	}

	if err := app.SendTextMessage(context.Background(), "ou_x", "hi"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if got := rt.requests[0].header.Get("Authorization"); got != "Bearer fresh-token" {
		t.Fatalf("want token from source, got %q", got)
	}
}

func TestNewAppWithTokenSourceRejectsNil(t *testing.T) {
	if _, err := NewAppWithTokenSource(nil); err == nil {
		t.Fatal("expected error for nil token source")
	}
}

func TestAppResolvesTokenOncePerOperation(t *testing.T) {
	var calls int
	source := func(context.Context) (*AccessToken, error) {
		calls++
		return &AccessToken{TenantAccessToken: "tok-once"}, nil
	}
	rt := &recordingRoundTripper{responses: []roundTripResponse{
		{status: http.StatusOK, body: `{"code":0,"data":{"image_key":"img_xxx"}}`},
		{status: http.StatusOK, body: `{"code":0}`},
	}}
	app, err := NewAppWithTokenSource(source, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "img-*.png")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	if _, err := file.WriteString("image-data"); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	_ = file.Close()

	if err := app.SendImageMessage(context.Background(), "ou_x", file.Name()); err != nil {
		t.Fatalf("send image: %v", err)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(rt.requests))
	}
	auth0 := rt.requests[0].header.Get("Authorization")
	auth1 := rt.requests[1].header.Get("Authorization")
	if auth0 != auth1 {
		t.Fatalf("upload and send used different tokens: %q vs %q", auth0, auth1)
	}
	if calls != 1 {
		t.Fatalf("token must resolve once per operation, resolved %d times", calls)
	}
}
