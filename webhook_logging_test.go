package msgbot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/dingtalk"
	"github.com/gtkit/msgbot/feishu"
	"github.com/gtkit/msgbot/wecom"
)

func TestWebhookDebugLogsRedactSecrets(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		configure func(*captureLogger, *http.Client) (msgbot.Provider, error)
	}{
		{
			name:   "feishu",
			secret: "feishu-secret-token",
			configure: func(logger *captureLogger, client *http.Client) (msgbot.Provider, error) {
				return feishu.New(msgbot.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/feishu-secret-token", Logger: logger, HTTPClient: client})
			},
		},
		{
			name:   "wecom",
			secret: "wecom-secret-token",
			configure: func(logger *captureLogger, client *http.Client) (msgbot.Provider, error) {
				return wecom.New(msgbot.Config{WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=wecom-secret-token", Logger: logger, HTTPClient: client})
			},
		},
		{
			name:   "dingtalk",
			secret: "dingtalk-secret-token",
			configure: func(logger *captureLogger, client *http.Client) (msgbot.Provider, error) {
				return dingtalk.New(msgbot.Config{WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=dingtalk-secret-token", Secret: "signing-secret", Logger: logger, HTTPClient: client})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &captureLogger{}
			client := &http.Client{Transport: loggingRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{"code":0}`
				if tt.name != "feishu" {
					body = `{"errcode":0}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			provider, err := tt.configure(logger, client)
			if err != nil {
				t.Fatalf("configure provider: %v", err)
			}
			if err := provider.SendText(context.Background(), "hello"); err != nil {
				t.Fatalf("send text: %v", err)
			}

			logs := logger.String()
			for _, forbidden := range []string{tt.secret, "signing-secret", "timestamp=", "sign=", "/open-apis/", "/cgi-bin/", "/robot/"} {
				if strings.Contains(logs, forbidden) {
					t.Fatalf("logs contain sensitive value %q: %s", forbidden, logs)
				}
			}

			errorLogger := &captureLogger{}
			errorClient := &http.Client{Transport: loggingRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed")
			})}
			errorProvider, err := tt.configure(errorLogger, errorClient)
			if err != nil {
				t.Fatalf("configure error provider: %v", err)
			}
			err = errorProvider.SendText(context.Background(), "hello")
			if err == nil || !strings.Contains(err.Error(), "dial failed") {
				t.Fatalf("expected transport error, got %v", err)
			}
			combined := err.Error() + "\n" + errorLogger.String()
			for _, forbidden := range []string{tt.secret, "signing-secret", "timestamp=", "sign=", "/open-apis/", "/cgi-bin/", "/robot/"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("transport failure leaked sensitive value %q: %s", forbidden, combined)
				}
			}
		})
	}
}

func TestWebhookConstructorsRejectInvalidURLs(t *testing.T) {
	constructors := []struct {
		name string
		new  func(string) error
	}{
		{name: "feishu", new: func(rawURL string) error { _, err := feishu.New(msgbot.Config{WebhookURL: rawURL}); return err }},
		{name: "wecom", new: func(rawURL string) error { _, err := wecom.New(msgbot.Config{WebhookURL: rawURL}); return err }},
		{name: "dingtalk", new: func(rawURL string) error { _, err := dingtalk.New(msgbot.Config{WebhookURL: rawURL}); return err }},
	}

	for _, constructor := range constructors {
		for _, rawURL := range []string{"/relative", "ftp://example.com/hook"} {
			t.Run(constructor.name+"_invalid", func(t *testing.T) {
				if err := constructor.new(rawURL); err == nil {
					t.Fatalf("expected invalid URL error for %q", rawURL)
				}
			})
		}
	}
}

type captureLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *captureLogger) DebugContext(_ context.Context, msg string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, msg+" "+fmt.Sprint(args...))
	l.mu.Unlock()
}

func (l *captureLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, msg+" "+fmt.Sprint(args...))
	l.mu.Unlock()
}

func (l *captureLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.logs, "\n")
}

type loggingRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn loggingRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
