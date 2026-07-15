package dingtalk

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

func TestWebhookSendLink(t *testing.T) {
	var body string
	bot, err := New(msgbot.Config{
		WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=test",
		HTTPClient: &http.Client{Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok"}`)), Request: req}, nil
		})},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := bot.SendLink(context.Background(), "title", "text", "https://example.com", ""); err != nil {
		t.Fatalf("send link: %v", err)
	}
	if !strings.Contains(body, `"msgtype":"link"`) || !strings.Contains(body, `"messageUrl":"https://example.com"`) {
		t.Fatalf("unexpected payload %s", body)
	}
}

func TestWebhookValidation(t *testing.T) {
	if _, err := New(msgbot.Config{}); err == nil {
		t.Fatal("expected missing url error")
	}
	bot, err := New(msgbot.Config{WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=test"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := bot.SendFeedCard(context.Background(), nil); err == nil {
		t.Fatal("expected feed card validation error")
	}
}

func TestWebhookSendVariants(t *testing.T) {
	bot, bodies := newDingTalkTestWebhook(t, []string{
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
	})

	if bot.Platform() != msgbot.PlatformDingTalk {
		t.Fatal("platform mismatch")
	}
	if bot.Stats() == nil {
		t.Fatal("stats is nil")
	}
	if err := bot.SendText(context.Background(), "hello", msgbot.WithAtAll()); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if err := bot.SendMarkdown(context.Background(), "title", "body", msgbot.WithAtUsers("13800000000")); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if err := bot.SendRichText(context.Background(), &msgbot.RichTextMessage{Title: "title", Content: [][]msgbot.RichTextTag{{{Tag: "text", Text: "hello"}}}}); err != nil {
		t.Fatalf("send rich text: %v", err)
	}
	if err := bot.SendImageFromURL(context.Background(), "https://example.com/a.png"); err != nil {
		t.Fatalf("send image url: %v", err)
	}
	if err := bot.SendActionCard(context.Background(), &ActionCard{
		Title: "title",
		Text:  "body",
		Buttons: []Button{
			{Title: "open", ActionURL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("send action card: %v", err)
	}
	if len(*bodies) != 5 {
		t.Fatalf("want 5 payloads, got %d", len(*bodies))
	}
}

func TestWebhookErrorsAndToken(t *testing.T) {
	bot, _ := newDingTalkTestWebhook(t, []string{`{"errcode":1,"errmsg":"bad"}`})
	if err := bot.SendText(context.Background(), "hello"); err == nil {
		t.Fatal("expected api error")
	}
	if err := bot.SendText(context.Background(), ""); err == nil {
		t.Fatal("expected text validation error")
	}
	if err := bot.SendMarkdown(context.Background(), "title", ""); err == nil {
		t.Fatal("expected markdown validation error")
	}
	if err := bot.SendRichText(context.Background(), nil); err == nil {
		t.Fatal("expected rich text validation error")
	}
	if err := bot.SendImage(context.Background(), &msgbot.ImageMessage{}); err == nil {
		t.Fatal("expected image validation error")
	}
	if err := bot.SendLink(context.Background(), "", "text", "https://example.com", ""); err == nil {
		t.Fatal("expected link validation error")
	}
	if err := bot.SendActionCard(context.Background(), nil); err == nil {
		t.Fatal("expected action card validation error")
	}
	if err := bot.SendImageFromURL(context.Background(), ""); err == nil {
		t.Fatal("expected image url validation error")
	}

	token := &AccessToken{AccessToken: "token"}
	if token.Token() != "token" {
		t.Fatal("token mismatch")
	}
	if _, err := GetAccessToken(context.Background(), "", "secret"); err == nil {
		t.Fatal("expected token validation error")
	}
}

func TestGetAccessToken(t *testing.T) {
	client := &http.Client{Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("want GET, got %s", req.Method)
		}
		if !strings.Contains(req.URL.RawQuery, "appkey=app") {
			t.Fatalf("unexpected query %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok","access_token":"token","expires_in":7200}`)),
			Request:    req,
		}, nil
	})}
	token, err := GetAccessToken(context.Background(), "app", "secret", client)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if token.Token() != "token" || token.ExpiresIn != 7200 {
		t.Fatalf("unexpected token: %+v", token)
	}

	errClient := &http.Client{Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"errcode":1,"errmsg":"bad"}`)),
			Request:    req,
		}, nil
	})}
	if _, err := GetAccessToken(context.Background(), "app", "secret", errClient); err == nil {
		t.Fatal("expected api error")
	}
}

func newDingTalkTestWebhook(t *testing.T, responses []string) (*Webhook, *[]string) {
	t.Helper()

	var bodies []string
	bot, err := New(msgbot.Config{
		WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=test",
		HTTPClient: &http.Client{Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			bodies = append(bodies, string(data))
			body := `{"errcode":0,"errmsg":"ok"}`
			if len(responses) > 0 {
				body = responses[0]
				responses = responses[1:]
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return bot, &bodies
}

type dingtalkRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn dingtalkRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
