package feishu

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

// TestSendTextMessageToEachReceiveIDType 钉住五种收件人类型都会被原样写进
// receive_id_type 查询参数，且 receive_id 落在请求体里。
func TestSendTextMessageToEachReceiveIDType(t *testing.T) {
	tests := []struct {
		idType    ReceiveIDType
		receiveID string
		want      string
	}{
		{idType: ReceiveIDOpenID, receiveID: "ou_xxx", want: "open_id"},
		{idType: ReceiveIDUserID, receiveID: "user_xxx", want: "user_id"},
		{idType: ReceiveIDUnionID, receiveID: "on_xxx", want: "union_id"},
		{idType: ReceiveIDEmail, receiveID: "someone@example.com", want: "email"},
		{idType: ReceiveIDChatID, receiveID: "oc_xxx", want: "chat_id"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			app := newTestApp(t, rt)

			if err := app.SendTextMessageTo(context.Background(), tt.idType, tt.receiveID, "hello"); err != nil {
				t.Fatalf("send: %v", err)
			}
			if len(rt.requests) != 1 {
				t.Fatalf("want 1 request, got %d", len(rt.requests))
			}

			parsed, err := url.Parse(rt.requests[0].url)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}
			if got := parsed.Query().Get("receive_id_type"); got != tt.want {
				t.Fatalf("want receive_id_type %q, got %q", tt.want, got)
			}
			payload := decodeMap(t, rt.requests[0].body)
			if got := payload["receive_id"]; got != tt.receiveID {
				t.Fatalf("want receive_id %q, got %v", tt.receiveID, got)
			}
			if got := payload["msg_type"]; got != "text" {
				t.Fatalf("want msg_type text, got %v", got)
			}
		})
	}
}

// TestLegacyAppMethodsStayOnOpenID 锁定既有方法的行为没有随 *To 的引入改变：
// 仍按 open_id 投递，URL 与 MessageAPI 常量一致。
func TestLegacyAppMethodsStayOnOpenID(t *testing.T) {
	rt := &recordingRoundTripper{}
	app := newTestApp(t, rt)

	if err := app.SendTextMessage(context.Background(), "ou_xxx", "hello"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if rt.requests[0].url != MessageAPI {
		t.Fatalf("want url %s, got %s", MessageAPI, rt.requests[0].url)
	}
	if !strings.HasSuffix(MessageAPI, "receive_id_type=open_id") {
		t.Fatalf("MessageAPI must stay the open_id endpoint, got %s", MessageAPI)
	}
	if got := decodeMap(t, rt.requests[0].body)["receive_id"]; got != "ou_xxx" {
		t.Fatalf("want receive_id ou_xxx, got %v", got)
	}
}

// TestSendMessageToValidationRejectsLocally 是「非法输入本地即拒、不解析 token、
// 不发请求」这一契约的反证测试：把任一校验挪到 a.token(ctx) 之后，tokenCalls
// 就会增长，测试失败。
func TestSendMessageToValidationRejectsLocally(t *testing.T) {
	tests := []struct {
		name    string
		call    func(*App) error
		wantErr string
	}{
		{
			name:    "unknown id type",
			call:    func(a *App) error { return a.SendTextMessageTo(context.Background(), "nope", "id", "hi") },
			wantErr: "unsupported receive id type",
		},
		{
			name:    "empty id type",
			call:    func(a *App) error { return a.SendTextMessageTo(context.Background(), "", "id", "hi") },
			wantErr: "unsupported receive id type",
		},
		{
			name:    "uppercase id type",
			call:    func(a *App) error { return a.SendTextMessageTo(context.Background(), "OPEN_ID", "id", "hi") },
			wantErr: "unsupported receive id type",
		},
		{
			name:    "empty receive id",
			call:    func(a *App) error { return a.SendTextMessageTo(context.Background(), ReceiveIDChatID, "", "hi") },
			wantErr: "receive_id is required",
		},
		{
			name:    "empty text",
			call:    func(a *App) error { return a.SendTextMessageTo(context.Background(), ReceiveIDChatID, "id", "") },
			wantErr: "text content is empty",
		},
		{
			name:    "image unknown id type",
			call:    func(a *App) error { return a.SendImageMessageTo(context.Background(), "nope", "id", "p.png") },
			wantErr: "unsupported receive id type",
		},
		{
			name:    "image empty receive id",
			call:    func(a *App) error { return a.SendImageMessageTo(context.Background(), ReceiveIDChatID, "", "p.png") },
			wantErr: "receive_id is required",
		},
		{
			name:    "image empty path",
			call:    func(a *App) error { return a.SendImageMessageTo(context.Background(), ReceiveIDChatID, "id", "") },
			wantErr: "image path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			tokenCalls := 0
			app, err := NewAppWithTokenSource(func(context.Context) (*AccessToken, error) {
				tokenCalls++
				return &AccessToken{TenantAccessToken: "tenant-token"}, nil
			}, &http.Client{Transport: rt})
			if err != nil {
				t.Fatalf("new app: %v", err)
			}

			gotErr := tt.call(app)
			var e *msgbot.Error
			if !errors.As(gotErr, &e) || e.Kind != msgbot.KindValidation {
				t.Fatalf("want validation *msgbot.Error, got %v", gotErr)
			}
			if !strings.Contains(gotErr.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, gotErr)
			}
			if len(rt.requests) != 0 {
				t.Fatalf("a locally rejected send must not reach the network, got %d requests", len(rt.requests))
			}
			if tokenCalls != 0 {
				t.Fatalf("a locally rejected send must not resolve a token, got %d resolutions", tokenCalls)
			}
		})
	}
}

// TestSendImageMessageToResolvesTokenOnce 锁定图片消息的上传与发送复用同一个
// token：source 只被调用一次，两个请求带同一个 Authorization。
func TestSendImageMessageToResolvesTokenOnce(t *testing.T) {
	imagePath := tempImage(t)

	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok","data":{"image_key":"img_xxx"}}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}

	tokenCalls := 0
	app, err := NewAppWithTokenSource(func(context.Context) (*AccessToken, error) {
		tokenCalls++
		return &AccessToken{TenantAccessToken: "tenant-token"}, nil
	}, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if err := app.SendImageMessageTo(context.Background(), ReceiveIDChatID, "oc_xxx", imagePath); err != nil {
		t.Fatalf("send image: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("upload and send must share one token resolution, got %d", tokenCalls)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(rt.requests))
	}
	if rt.requests[0].url != FeishuUploadImageAPI {
		t.Fatalf("want upload url %s, got %s", FeishuUploadImageAPI, rt.requests[0].url)
	}

	parsed, err := url.Parse(rt.requests[1].url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if got := parsed.Query().Get("receive_id_type"); got != "chat_id" {
		t.Fatalf("want receive_id_type chat_id, got %q", got)
	}
	payload := decodeMap(t, rt.requests[1].body)
	if got := payload["receive_id"]; got != "oc_xxx" {
		t.Fatalf("want receive_id oc_xxx, got %v", got)
	}
	if got := decodeMap(t, []byte(payload["content"].(string)))["image_key"]; got != "img_xxx" {
		t.Fatalf("want image_key img_xxx, got %v", got)
	}
}

// TestWebhookSendImageFromFileHonoursSwitch 是「静音时不做发送前的网络预处理」
// 这一契约的反证测试：移除 SendImageFromFile 开头的静音短路，上传请求就会发出，
// 测试失败。
func TestWebhookSendImageFromFileHonoursSwitch(t *testing.T) {
	imagePath := tempImage(t)

	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok","data":{"image_key":"img_xxx"}}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}
	gate := msgbot.NewSwitch()
	gate.Disable()

	bot, err := New(msgbot.Config{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		HTTPClient: &http.Client{Transport: rt},
		Switch:     gate,
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}

	if err := bot.SendImageFromFile(context.Background(), "tenant-token", imagePath); err != nil {
		t.Fatalf("muted send must return nil, got %v", err)
	}
	if len(rt.requests) != 0 {
		t.Fatalf("muted send must not upload the image, got %d requests", len(rt.requests))
	}
	if bot.Stats().TotalSent() != 0 || bot.Stats().TotalError() != 0 {
		t.Fatal("muted is neither success nor failure")
	}
	// 恰好一次：前置短路命中后直接返回，不会再经 Config.Send 又计一次。
	if bot.Stats().TotalMuted() != 1 {
		t.Fatalf("want muted=1 exactly, got %d", bot.Stats().TotalMuted())
	}

	gate.Enable()
	if err := bot.SendImageFromFile(context.Background(), "tenant-token", imagePath); err != nil {
		t.Fatalf("send after enable: %v", err)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("want upload + send after enable, got %d requests", len(rt.requests))
	}
}
