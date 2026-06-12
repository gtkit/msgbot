package wecom

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	news "github.com/gtkit/msgbot"
)

func TestWebhookSendText(t *testing.T) {
	var body string
	bot, err := New(news.Config{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test",
		HTTPClient: &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok"}`)), Request: req}, nil
		})},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := bot.SendText(context.Background(), "hello", news.WithAtAll()); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if !strings.Contains(body, `"msgtype":"text"`) || !strings.Contains(body, `"@all"`) {
		t.Fatalf("unexpected payload %s", body)
	}
	if bot.Stats().TotalSent() != 1 {
		t.Fatalf("want sent 1, got %d", bot.Stats().TotalSent())
	}
}

func TestBuildImageMessage(t *testing.T) {
	img := BuildImageMessage([]byte("image"))
	if img.Base64 == "" || img.MD5 == "" {
		t.Fatalf("incomplete image message: %+v", img)
	}
}

func TestWebhookSendVariants(t *testing.T) {
	bot, bodies := newWeComTestWebhook(t, []string{
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
		`{"errcode":0,"errmsg":"ok"}`,
	})

	if bot.Platform() != news.PlatformWeCom {
		t.Fatal("platform mismatch")
	}
	if err := bot.SendMarkdown(context.Background(), "title", "body"); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if err := bot.SendRichText(context.Background(), &news.RichTextMessage{Content: [][]news.RichTextTag{{{Tag: "text", Text: "hello"}}}}); err != nil {
		t.Fatalf("send rich text: %v", err)
	}
	if err := bot.SendImage(context.Background(), BuildImageMessage([]byte("image"))); err != nil {
		t.Fatalf("send image: %v", err)
	}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("image"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := bot.SendImageFromFile(context.Background(), file); err != nil {
		t.Fatalf("send image file: %v", err)
	}
	if len(*bodies) != 4 {
		t.Fatalf("want 4 payloads, got %d", len(*bodies))
	}
}

func TestWebhookErrorsAndToken(t *testing.T) {
	if _, err := New(news.Config{}); err == nil {
		t.Fatal("expected missing url error")
	}
	bot, _ := newWeComTestWebhook(t, []string{`{"errcode":1,"errmsg":"bad"}`})
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
	if err := bot.SendImage(context.Background(), &news.ImageMessage{}); err == nil {
		t.Fatal("expected image validation error")
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
	client := &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("want GET, got %s", req.Method)
		}
		if !strings.Contains(req.URL.RawQuery, "corpid=corp") {
			t.Fatalf("unexpected query %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok","access_token":"token","expires_in":7200}`)),
			Request:    req,
		}, nil
	})}
	token, err := GetAccessToken(context.Background(), "corp", "secret", client)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if token.Token() != "token" || token.ExpiresIn != 7200 {
		t.Fatalf("unexpected token: %+v", token)
	}

	errClient := &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"errcode":1,"errmsg":"bad"}`)),
			Request:    req,
		}, nil
	})}
	if _, err := GetAccessToken(context.Background(), "corp", "secret", errClient); err == nil {
		t.Fatal("expected api error")
	}
}

func newWeComTestWebhook(t *testing.T, responses []string) (*Webhook, *[]string) {
	t.Helper()

	var bodies []string
	bot, err := New(news.Config{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test",
		HTTPClient: &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
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

type wecomRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn wecomRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
