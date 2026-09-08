package feishu

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

// newSwitchedApp 构造一个带开关的 App，并返回 transport 与 token 解析次数指针，
// 便于断言「静音时既不解析 token 也不发请求」。
func newSwitchedApp(t *testing.T, sw *msgbot.Switch, responses ...roundTripResponse) (*App, *recordingRoundTripper, *int) {
	t.Helper()

	rt := &recordingRoundTripper{responses: responses}
	tokenCalls := 0
	app, err := NewAppWithConfig(AppConfig{
		Source: func(context.Context) (*AccessToken, error) {
			tokenCalls++
			return &AccessToken{TenantAccessToken: "tenant-token"}, nil
		},
		HTTPClient: &http.Client{Transport: rt},
		Switch:     sw,
	})
	if err != nil {
		t.Fatalf("new app with config: %v", err)
	}
	return app, rt, &tokenCalls
}

func tempImage(t *testing.T) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "msgbot-feishu-image-*.png")
	if err != nil {
		t.Fatalf("create temp image: %v", err)
	}
	if _, err := file.Write([]byte("image-data")); err != nil {
		t.Fatalf("write temp image: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp image: %v", err)
	}
	return file.Name()
}

func TestNewAppWithConfigValidation(t *testing.T) {
	source := func(context.Context) (*AccessToken, error) {
		return &AccessToken{TenantAccessToken: "tenant-token"}, nil
	}

	tests := []struct {
		name    string
		cfg     AppConfig
		wantErr string
	}{
		{name: "neither token nor source", cfg: AppConfig{}, wantErr: "got neither"},
		{
			name:    "both token and source",
			cfg:     AppConfig{Token: &AccessToken{TenantAccessToken: "t"}, Source: source},
			wantErr: "got both",
		},
		{name: "empty tenant token", cfg: AppConfig{Token: &AccessToken{}}, wantErr: "tenant access token is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := NewAppWithConfig(tt.cfg)
			var e *msgbot.Error
			if !errors.As(err, &e) || e.Kind != msgbot.KindValidation {
				t.Fatalf("want validation *msgbot.Error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if app != nil {
				t.Fatal("a rejected config must not yield a usable app")
			}
		})
	}

	for _, cfg := range []AppConfig{
		{Token: &AccessToken{TenantAccessToken: "tenant-token"}},
		{Source: source},
	} {
		if _, err := NewAppWithConfig(cfg); err != nil {
			t.Fatalf("valid config rejected: %v", err)
		}
	}
}

// TestNewAppWithConfigSnapshotsStaticToken 锁定静态 token 被快照：构造后修改
// 原对象不影响已创建的 App。这条契约原本由 NewApp 持有，提取 staticSource 后
// 两个入口共享同一实现，必须仍然成立。
func TestNewAppWithConfigSnapshotsStaticToken(t *testing.T) {
	token := &AccessToken{TenantAccessToken: "original"}
	rt := &recordingRoundTripper{}
	app, err := NewAppWithConfig(AppConfig{Token: token, HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatalf("new app with config: %v", err)
	}

	token.TenantAccessToken = "mutated"
	if err := app.SendTextMessage(context.Background(), "ou_xxx", "hi"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if got := rt.requests[0].header.Get("Authorization"); got != "Bearer original" {
		t.Fatalf("app must use the token snapshot, got %q", got)
	}
}

// TestAppMutedSkipsTokenAndRequest 是「App 静音时不解析 token、不发请求」这一
// 契约的反证测试：移除 SendTextMessageTo / SendImageMessageTo 里的 a.muted()
// 短路，tokenCalls 与请求数都会增长，测试失败。
func TestAppMutedSkipsTokenAndRequest(t *testing.T) {
	image := tempImage(t)

	tests := []struct {
		name string
		call func(*App) error
	}{
		{
			name: "text",
			call: func(a *App) error {
				return a.SendTextMessageTo(context.Background(), ReceiveIDChatID, "oc_xxx", "hi")
			},
		},
		{
			name: "legacy text",
			call: func(a *App) error { return a.SendTextMessage(context.Background(), "ou_xxx", "hi") },
		},
		{
			name: "image",
			call: func(a *App) error {
				return a.SendImageMessageTo(context.Background(), ReceiveIDChatID, "oc_xxx", image)
			},
		},
		{
			name: "legacy image",
			call: func(a *App) error { return a.SendImageMessage(context.Background(), "ou_xxx", image) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := msgbot.NewSwitch()
			gate.Disable()
			app, rt, tokenCalls := newSwitchedApp(t, gate)

			if err := tt.call(app); err != nil {
				t.Fatalf("muted send must return nil, got %v", err)
			}
			if len(rt.requests) != 0 {
				t.Fatalf("muted send must not reach the network, got %d requests", len(rt.requests))
			}
			if *tokenCalls != 0 {
				t.Fatalf("muted send must not resolve a token, got %d resolutions", *tokenCalls)
			}
		})
	}
}

// TestAppValidationWinsOverMute 锁定检查顺序：参数校验在静音之前。静音是运维
// 开关，不该把编程错误一起吞掉——否则静音期间写错的调用要等到恢复发送才暴露。
func TestAppValidationWinsOverMute(t *testing.T) {
	gate := msgbot.NewSwitch()
	gate.Disable()
	app, rt, tokenCalls := newSwitchedApp(t, gate)

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name:    "unknown id type",
			call:    func() error { return app.SendTextMessageTo(context.Background(), "nope", "id", "hi") },
			wantErr: "unsupported receive id type",
		},
		{
			name:    "empty receive id",
			call:    func() error { return app.SendTextMessageTo(context.Background(), ReceiveIDChatID, "", "hi") },
			wantErr: "receive_id is required",
		},
		{
			name:    "empty text",
			call:    func() error { return app.SendTextMessageTo(context.Background(), ReceiveIDChatID, "id", "") },
			wantErr: "text content is empty",
		},
		{
			name:    "image empty path",
			call:    func() error { return app.SendImageMessageTo(context.Background(), ReceiveIDChatID, "id", "") },
			wantErr: "image path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			var e *msgbot.Error
			if !errors.As(err, &e) || e.Kind != msgbot.KindValidation {
				t.Fatalf("muting must not swallow a validation error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}

	if len(rt.requests) != 0 || *tokenCalls != 0 {
		t.Fatalf("no request or token resolution expected, got %d/%d", len(rt.requests), *tokenCalls)
	}
}

func TestAppResumesAfterEnable(t *testing.T) {
	gate := msgbot.NewSwitch()
	gate.Disable()
	app, rt, tokenCalls := newSwitchedApp(t, gate,
		roundTripResponse{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
	)

	ctx := context.Background()
	if err := app.SendTextMessageTo(ctx, ReceiveIDChatID, "oc_xxx", "hi"); err != nil {
		t.Fatalf("muted send: %v", err)
	}
	gate.Enable()
	if err := app.SendTextMessageTo(ctx, ReceiveIDChatID, "oc_xxx", "hi"); err != nil {
		t.Fatalf("send after enable: %v", err)
	}

	if len(rt.requests) != 1 {
		t.Fatalf("want 1 request (muted one skipped), got %d", len(rt.requests))
	}
	if *tokenCalls != 1 {
		t.Fatalf("want 1 token resolution, got %d", *tokenCalls)
	}
}

// TestAppSharesSwitchWithWebhook 锁定 App 与 webhook provider 用的是同一个开关
// 语义：一个 Disable 同时静音两者，这是「一键停发」覆盖飞书应用消息的关键。
func TestAppSharesSwitchWithWebhook(t *testing.T) {
	gate := msgbot.NewSwitch()
	app, appRT, tokenCalls := newSwitchedApp(t, gate)

	hookRT := &recordingRoundTripper{}
	bot, err := New(msgbot.Config{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		HTTPClient: &http.Client{Transport: hookRT},
		Switch:     gate,
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}

	gate.Disable()
	ctx := context.Background()
	if err := app.SendTextMessageTo(ctx, ReceiveIDChatID, "oc_xxx", "hi"); err != nil {
		t.Fatalf("app: %v", err)
	}
	if err := bot.SendText(ctx, "hi"); err != nil {
		t.Fatalf("webhook: %v", err)
	}

	if len(appRT.requests) != 0 || len(hookRT.requests) != 0 || *tokenCalls != 0 {
		t.Fatalf("one Disable must mute both, got app=%d hook=%d token=%d",
			len(appRT.requests), len(hookRT.requests), *tokenCalls)
	}
}

// TestAppWithoutSwitchAlwaysSends 锁定既有构造函数的行为不变：NewApp 与
// NewAppWithTokenSource 创建的 App 没有开关，永远视为启用。
func TestAppWithoutSwitchAlwaysSends(t *testing.T) {
	tests := []struct {
		name string
		make func(*recordingRoundTripper) (*App, error)
	}{
		{
			name: "NewApp",
			make: func(rt *recordingRoundTripper) (*App, error) {
				return NewApp(&AccessToken{TenantAccessToken: "tenant-token"}, &http.Client{Transport: rt})
			},
		},
		{
			name: "NewAppWithTokenSource",
			make: func(rt *recordingRoundTripper) (*App, error) {
				return NewAppWithTokenSource(func(context.Context) (*AccessToken, error) {
					return &AccessToken{TenantAccessToken: "tenant-token"}, nil
				}, &http.Client{Transport: rt})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			app, err := tt.make(rt)
			if err != nil {
				t.Fatalf("construct: %v", err)
			}
			if app.muted() {
				t.Fatal("an app without a switch must never be muted")
			}
			if err := app.SendTextMessage(context.Background(), "ou_xxx", "hi"); err != nil {
				t.Fatalf("send text: %v", err)
			}
			if len(rt.requests) != 1 {
				t.Fatalf("want 1 request, got %d", len(rt.requests))
			}
		})
	}
}
