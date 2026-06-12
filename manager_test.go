package msgbot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestResponseErr(t *testing.T) {
	if err := (&Response{}).Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err := (&Response{ErrCode: 400, ErrMsg: "bad"}).Err()
	if err == nil || !strings.Contains(err.Error(), "code=400, msg=bad") {
		t.Fatalf("want api error, got %v", err)
	}
}

func TestManager(t *testing.T) {
	fs := &fakeProvider{platform: PlatformFeishu}
	wc := &fakeProvider{platform: PlatformWeCom}

	mgr := NewManager(fs, wc)
	if mgr.Default() != fs {
		t.Fatal("default provider mismatch")
	}
	if mgr.Feishu() != fs || mgr.WeCom() != wc {
		t.Fatal("platform provider mismatch")
	}

	mgr.SetDefault(PlatformWeCom)
	if mgr.Default() != wc {
		t.Fatal("updated default provider mismatch")
	}
	if mgr.Get(PlatformFeishu) != fs || mgr.DingTalk() != nil {
		t.Fatal("manager lookup mismatch")
	}
	if len(mgr.All()) != 2 {
		t.Fatalf("want 2 providers, got %d", len(mgr.All()))
	}
	if multi, err := mgr.Multi(); err != nil || multi == nil {
		t.Fatalf("manager multi: %v", err)
	}
}

func TestMulti(t *testing.T) {
	if _, err := NewMulti(); err == nil {
		t.Fatal("expected error for empty providers")
	}

	errProvider := &fakeProvider{platform: PlatformWeCom, err: errors.New("send failed")}
	multi, err := NewMulti(&fakeProvider{platform: PlatformFeishu}, errProvider)
	if err != nil {
		t.Fatalf("new multi: %v", err)
	}
	if err := multi.SendText(context.Background(), "hello"); err == nil {
		t.Fatal("expected joined error")
	}

	single, err := NewMulti(&fakeProvider{platform: PlatformFeishu})
	if err != nil {
		t.Fatalf("new single multi: %v", err)
	}
	ctx := context.Background()
	if err := single.SendMarkdown(ctx, "title", "body"); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if err := single.SendRichText(ctx, &RichTextMessage{}); err != nil {
		t.Fatalf("send rich text: %v", err)
	}
	if err := single.SendImage(ctx, &ImageMessage{}); err != nil {
		t.Fatalf("send image: %v", err)
	}
}

func TestRichTextToMarkdown(t *testing.T) {
	got := RichTextToMarkdown(&RichTextMessage{
		Title: "标题",
		Content: [][]RichTextTag{{
			{Tag: "text", Text: "查看 "},
			{Tag: "a", Text: "链接", Href: "https://example.com"},
			{Tag: "at", UserID: "all"},
		}},
	})
	want := "### 标题\n\n查看 [链接](https://example.com)@所有人"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestConfigStatsAndLogger(t *testing.T) {
	cfg := Config{}
	cfg.Freeze()
	if cfg.GetHTTPClient() == nil {
		t.Fatal("http client is nil")
	}

	logger := &SlogLogger{L: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cfg.Logger = logger
	cfg.LogDebug(context.Background(), "debug")
	cfg.LogError(context.Background(), "error")
	logger.DebugContext(context.Background(), "debug")
	logger.ErrorContext(context.Background(), "error")

	var stats Stats
	stats.IncSent()
	stats.IncError()
	if stats.TotalSent() != 1 || stats.TotalError() != 1 {
		t.Fatalf("unexpected stats sent=%d error=%d", stats.TotalSent(), stats.TotalError())
	}
}

type fakeProvider struct {
	platform Platform
	err      error
}

func (p *fakeProvider) SendText(context.Context, string, ...SendOption) error {
	return p.err
}

func (p *fakeProvider) SendMarkdown(context.Context, string, string, ...SendOption) error {
	return p.err
}

func (p *fakeProvider) SendRichText(context.Context, *RichTextMessage) error {
	return p.err
}

func (p *fakeProvider) SendImage(context.Context, *ImageMessage) error {
	return p.err
}

func (p *fakeProvider) Platform() Platform {
	return p.platform
}
