package wecom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

func TestSendImageFromFileRejectsOversize(t *testing.T) {
	// 写一个超过 2MB 上限的临时文件。
	path := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 3<<20)), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var called bool
	bot, err := New(msgbot.Config{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test",
		HTTPClient: &http.Client{Transport: wecomRoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: 200, Body: http.NoBody, Request: nil}, nil
		})},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	err = bot.SendImageFromFile(context.Background(), path)
	var e *msgbot.Error
	if !errors.As(err, &e) || e.Kind != msgbot.KindValidation {
		t.Fatalf("want validation error for oversize file, got %v", err)
	}
	if called {
		t.Fatal("oversize file must be rejected before any HTTP request")
	}
}
