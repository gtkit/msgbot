package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gtkit/msgbot"
)

const testURL = "https://hooks.example.com/alert"

// recordingTransport 记录每次出站请求，并按预设序列返回响应。
// 序列耗尽后重复最后一项；calls 用 atomic 计数以支持并发测试。
type recordingTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
	steps    []step
	calls    atomic.Int64
}

type recordedRequest struct {
	method      string
	url         string
	contentType string
	body        []byte
}

type step struct {
	status     int
	body       string
	retryAfter string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
	}

	t.mu.Lock()
	t.requests = append(t.requests, recordedRequest{
		method:      req.Method,
		url:         req.URL.String(),
		contentType: req.Header.Get("Content-Type"),
		body:        body,
	})
	i := int(t.calls.Add(1)) - 1
	steps := t.steps
	t.mu.Unlock()

	st := step{status: http.StatusOK, body: `{"code":0}`}
	if len(steps) > 0 {
		if i >= len(steps) {
			i = len(steps) - 1
		}
		st = steps[i]
	}

	header := make(http.Header)
	if st.retryAfter != "" {
		header.Set("Retry-After", st.retryAfter)
	}
	return &http.Response{
		StatusCode: st.status,
		Body:       io.NopCloser(strings.NewReader(st.body)),
		Header:     header,
		Request:    req,
	}, nil
}

func (t *recordingTransport) last() recordedRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requests[len(t.requests)-1]
}

// echoBuilder 把整条归一化消息回显成 payload，便于断言字段映射。
func echoBuilder(m *Message) (any, error) {
	return map[string]any{
		"kind":    string(m.Kind),
		"text":    m.Text,
		"title":   m.Title,
		"content": m.Content,
		"at_all":  m.Options.AtAll,
		"at":      m.Options.AtUserIDs,
		"rich":    m.RichText != nil,
		"image":   m.Image != nil,
	}, nil
}

func newHook(t *testing.T, tr http.RoundTripper, build PayloadBuilder, cfg msgbot.Config) *Webhook {
	t.Helper()

	cfg.WebhookURL = testURL
	cfg.HTTPClient = &http.Client{Transport: tr}
	hook, err := New(cfg, build)
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}
	return hook
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return got
}

func kindOf(t *testing.T, err error) *msgbot.Error {
	t.Helper()

	var e *msgbot.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *msgbot.Error, got %v", err)
	}
	return e
}

func TestNewValidatesConfig(t *testing.T) {
	t.Parallel()

	ok := func(*Message) (any, error) { return nil, nil }
	tests := []struct {
		name    string
		url     string
		build   PayloadBuilder
		wantErr string
	}{
		{name: "missing url", url: "", build: ok, wantErr: "webhook URL is required"},
		{name: "relative url", url: "/hook", build: ok, wantErr: "invalid webhook URL"},
		{name: "non http scheme", url: "ftp://example.com/hook", build: ok, wantErr: "invalid webhook URL"},
		{name: "no host", url: "https://", build: ok, wantErr: "invalid webhook URL"},
		{name: "nil builder", url: testURL, build: nil, wantErr: "payload builder is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hook, err := New(msgbot.Config{WebhookURL: tt.url}, tt.build)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if hook != nil {
				t.Fatal("a rejected config must not yield a usable provider")
			}
		})
	}

	// 错误信息不得回显可能带凭据的原始 URL。
	_, err := New(msgbot.Config{WebhookURL: "ftp://example.com/hook?token=secret-value"}, ok)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error must not echo the raw URL, got %v", err)
	}
}

func TestWebhookMetadata(t *testing.T) {
	t.Parallel()

	hook := newHook(t, &recordingTransport{}, echoBuilder, msgbot.Config{})
	if hook.Platform() != msgbot.PlatformWebhook {
		t.Fatalf("want platform %q, got %q", msgbot.PlatformWebhook, hook.Platform())
	}
	if hook.Stats() == nil {
		t.Fatal("Stats must not be nil")
	}

	var _ msgbot.Provider = hook
}

func TestSendPostsJSONPayload(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	hook := newHook(t, tr, func(m *Message) (any, error) {
		return map[string]any{"msg": m.Text}, nil
	}, msgbot.Config{})

	if err := hook.SendText(context.Background(), "hi"); err != nil {
		t.Fatalf("send text: %v", err)
	}

	req := tr.last()
	if req.method != http.MethodPost {
		t.Fatalf("want POST, got %s", req.method)
	}
	if req.url != testURL {
		t.Fatalf("want url %s, got %s", testURL, req.url)
	}
	if req.contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type %q", req.contentType)
	}
	if got := decode(t, req.body)["msg"]; got != "hi" {
		t.Fatalf("want payload msg=hi, got %v", got)
	}
	if hook.Stats().TotalSent() != 1 || hook.Stats().TotalError() != 0 || hook.Stats().TotalMuted() != 0 {
		t.Fatalf("want sent=1 error=0 muted=0, got sent=%d error=%d muted=%d",
			hook.Stats().TotalSent(), hook.Stats().TotalError(), hook.Stats().TotalMuted())
	}
}

// TestMessageFieldMapping 钉住 Kind 与字段的对应关系——这是 PayloadBuilder 的契约，
// 调用方按它写 switch。改动任一映射，测试即失败。
func TestMessageFieldMapping(t *testing.T) {
	t.Parallel()

	richText := &msgbot.RichTextMessage{Title: "标题"}
	image := &msgbot.ImageMessage{PicURL: "https://example.com/x.png"}

	tests := []struct {
		name string
		call func(*Webhook) error
		want map[string]any
	}{
		{
			name: "text",
			call: func(w *Webhook) error {
				return w.SendText(context.Background(), "hi", msgbot.WithAtAll(), msgbot.WithAtUsers("u1", "u2"))
			},
			want: map[string]any{"kind": "text", "text": "hi", "at_all": true, "at": []any{"u1", "u2"}, "rich": false, "image": false},
		},
		{
			name: "markdown",
			call: func(w *Webhook) error {
				return w.SendMarkdown(context.Background(), "标题", "正文", msgbot.WithAtUsers("u1"))
			},
			want: map[string]any{"kind": "markdown", "title": "标题", "content": "正文", "at_all": false, "at": []any{"u1"}, "rich": false, "image": false},
		},
		{
			name: "richtext",
			call: func(w *Webhook) error { return w.SendRichText(context.Background(), richText) },
			want: map[string]any{"kind": "richtext", "at_all": false, "rich": true, "image": false},
		},
		{
			name: "image",
			call: func(w *Webhook) error { return w.SendImage(context.Background(), image) },
			want: map[string]any{"kind": "image", "at_all": false, "rich": false, "image": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &recordingTransport{}
			hook := newHook(t, tr, echoBuilder, msgbot.Config{})
			if err := tt.call(hook); err != nil {
				t.Fatalf("send: %v", err)
			}

			got := decode(t, tr.last().body)
			for key, want := range tt.want {
				if !equalJSON(got[key], want) {
					t.Fatalf("field %q: want %v, got %v (full payload %v)", key, want, got[key], got)
				}
			}
		})
	}
}

func equalJSON(got, want any) bool {
	gotList, gotOK := got.([]any)
	wantList, wantOK := want.([]any)
	if gotOK || wantOK {
		if !gotOK || !wantOK || len(gotList) != len(wantList) {
			return false
		}
		for i := range wantList {
			if gotList[i] != wantList[i] {
				return false
			}
		}
		return true
	}
	return got == want
}

// TestBuilderReceivesNonNilOptions 锁定「Options 始终非 nil」这一契约：
// 调用方直接读 m.Options.AtAll，若某条路径传了 nil 就会 panic。
func TestBuilderReceivesNonNilOptions(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	hook := newHook(t, tr, func(m *Message) (any, error) {
		if m == nil {
			return nil, errors.New("message is nil")
		}
		if m.Options == nil {
			return nil, errors.New("options is nil")
		}
		return map[string]any{"ok": true}, nil
	}, msgbot.Config{})

	ctx := context.Background()
	calls := []func() error{
		func() error { return hook.SendText(ctx, "t") },
		func() error { return hook.SendMarkdown(ctx, "t", "c") },
		func() error { return hook.SendRichText(ctx, &msgbot.RichTextMessage{}) },
		func() error { return hook.SendImage(ctx, &msgbot.ImageMessage{}) },
	}
	for i, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

// TestBuilderErrorIsValidationAndNotRetried 是「构造函数返回错误即立即失败、不重试」
// 的反证测试：若 Config.Send 把它当作可重试错误，transport 调用数会 > 0 或重试多次。
func TestBuilderErrorIsValidationAndNotRetried(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("端点不支持该消息类型")
	tr := &recordingTransport{}
	hook := newHook(t, tr, func(*Message) (any, error) {
		return nil, sentinel
	}, msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 3}})

	err := hook.SendText(context.Background(), "hi")
	e := kindOf(t, err)
	if e.Kind != msgbot.KindValidation {
		t.Fatalf("want kind %q, got %q", msgbot.KindValidation, e.Kind)
	}
	if e.Retryable {
		t.Fatal("a builder rejection must never be retryable")
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("the builder's own error must stay unwrappable")
	}
	if tr.calls.Load() != 0 {
		t.Fatalf("a builder rejection must not reach the transport, got %d calls", tr.calls.Load())
	}
	if hook.Stats().TotalError() != 1 || hook.Stats().TotalSent() != 0 || hook.Stats().TotalMuted() != 0 {
		t.Fatalf("want sent=0 error=1 muted=0, got sent=%d error=%d muted=%d",
			hook.Stats().TotalSent(), hook.Stats().TotalError(), hook.Stats().TotalMuted())
	}
	// 原因不应被打印两遍。
	if n := strings.Count(err.Error(), sentinel.Error()); n != 1 {
		t.Fatalf("cause must appear exactly once in %q, got %d", err.Error(), n)
	}
}

func TestUnmarshalablePayloadIsValidation(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	hook := newHook(t, tr, func(*Message) (any, error) {
		return map[string]any{"ch": make(chan int)}, nil
	}, msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 3}})

	e := kindOf(t, hook.SendText(context.Background(), "hi"))
	if e.Kind != msgbot.KindValidation || e.Retryable {
		t.Fatalf("want non-retryable validation error, got kind=%q retryable=%v", e.Kind, e.Retryable)
	}
	if tr.calls.Load() != 0 {
		t.Fatalf("want no transport call, got %d", tr.calls.Load())
	}
}

// TestNilMessagesRejectedBeforeBuilder 锁定 nil 富文本/图片在本地被拒，
// 且构造函数根本不会被调用——否则调用方的 switch 分支会对 nil 解引用。
func TestNilMessagesRejectedBeforeBuilder(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	called := false
	hook := newHook(t, tr, func(*Message) (any, error) {
		called = true
		return nil, nil
	}, msgbot.Config{})

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{name: "nil rich text", call: func() error { return hook.SendRichText(context.Background(), nil) }, wantErr: "rich text message is nil"},
		{name: "nil image", call: func() error { return hook.SendImage(context.Background(), nil) }, wantErr: "image message is nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			e := kindOf(t, err)
			if e.Kind != msgbot.KindValidation {
				t.Fatalf("want kind %q, got %q", msgbot.KindValidation, e.Kind)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}

	if called {
		t.Fatal("the payload builder must not run for a locally rejected message")
	}
	if tr.calls.Load() != 0 {
		t.Fatalf("want no transport call, got %d", tr.calls.Load())
	}
}

func TestHTTPRetrySemantics(t *testing.T) {
	t.Parallel()

	t.Run("429 is retried", func(t *testing.T) {
		t.Parallel()

		tr := &recordingTransport{steps: []step{
			{status: http.StatusTooManyRequests, body: `rate limited`, retryAfter: "0"},
			{status: http.StatusOK, body: `{"code":0}`},
		}}
		hook := newHook(t, tr, echoBuilder, msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 2, InitialDelay: 1}})

		if err := hook.SendText(context.Background(), "hi"); err != nil {
			t.Fatalf("send text: %v", err)
		}
		if tr.calls.Load() != 2 {
			t.Fatalf("want 2 attempts, got %d", tr.calls.Load())
		}
	})

	t.Run("business code is not retried", func(t *testing.T) {
		t.Parallel()

		tr := &recordingTransport{steps: []step{{status: http.StatusOK, body: `{"code":40001,"msg":"bad token"}`}}}
		hook := newHook(t, tr, echoBuilder, msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 3, InitialDelay: 1}})

		e := kindOf(t, hook.SendText(context.Background(), "hi"))
		if e.Kind != msgbot.KindPlatform {
			t.Fatalf("want kind %q, got %q", msgbot.KindPlatform, e.Kind)
		}
		if e.Code != 40001 {
			t.Fatalf("want code 40001, got %d", e.Code)
		}
		if e.Retryable {
			t.Fatal("a generic endpoint has no known rate-limit codes, so platform errors must not be retryable")
		}
		if tr.calls.Load() != 1 {
			t.Fatalf("want exactly 1 attempt, got %d", tr.calls.Load())
		}
	})
}

// TestSwitchMutesWebhook 锁定通用 webhook 与三平台共享同一个开关语义：
// 静音时既不发请求、也不调用构造函数。
func TestSwitchMutesWebhook(t *testing.T) {
	t.Parallel()

	gate := msgbot.NewSwitch()
	gate.Disable()

	tr := &recordingTransport{}
	called := false
	hook := newHook(t, tr, func(*Message) (any, error) {
		called = true
		return nil, nil
	}, msgbot.Config{Switch: gate})

	if err := hook.SendText(context.Background(), "hi"); err != nil {
		t.Fatalf("muted send must return nil, got %v", err)
	}
	if called {
		t.Fatal("muted send must not run the payload builder")
	}
	if tr.calls.Load() != 0 {
		t.Fatalf("muted send must not reach the transport, got %d calls", tr.calls.Load())
	}
	if hook.Stats().TotalSent() != 0 || hook.Stats().TotalError() != 0 {
		t.Fatal("muted is neither success nor failure")
	}
	if hook.Stats().TotalMuted() != 1 {
		t.Fatalf("want muted=1, got %d", hook.Stats().TotalMuted())
	}

	gate.Enable()
	if err := hook.SendText(context.Background(), "hi"); err != nil {
		t.Fatalf("send after enable: %v", err)
	}
	if tr.calls.Load() != 1 {
		t.Fatalf("want 1 call after enable, got %d", tr.calls.Load())
	}
}

// TestConcurrentSendIsRaceFree 是「可安全并发使用」这一契约的反证测试：
// 把 Webhook 的字段改成可变（例如在 send 里写 w.cfg），-race 下即报竞争。
func TestConcurrentSendIsRaceFree(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	hook := newHook(t, tr, echoBuilder, msgbot.Config{})

	const goroutines, perGoroutine = 8, 25
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			ctx := context.Background()
			for range perGoroutine {
				if err := hook.SendText(ctx, "hi"); err != nil {
					t.Errorf("send text: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()

	want := int64(goroutines * perGoroutine)
	if tr.calls.Load() != want {
		t.Fatalf("want %d calls, got %d", want, tr.calls.Load())
	}
	if hook.Stats().TotalSent() != want {
		t.Fatalf("want sent=%d, got %d", want, hook.Stats().TotalSent())
	}
}
