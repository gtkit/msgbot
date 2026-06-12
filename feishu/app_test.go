package feishu

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	json "github.com/gtkit/json/v2"
	news "github.com/gtkit/msgbot"
)

func TestNewApp(t *testing.T) {
	tests := []struct {
		name    string
		token   *AccessToken
		wantErr string
	}{
		{
			name:    "nil token",
			wantErr: "access token is nil",
		},
		{
			name:    "empty tenant token",
			token:   &AccessToken{},
			wantErr: "tenant access token is empty",
		},
		{
			name:  "valid token",
			token: &AccessToken{TenantAccessToken: "tenant-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := NewApp(tt.token)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if app == nil {
				t.Fatal("app is nil")
			}
		})
	}
}

func TestApp_SendTextMessage(t *testing.T) {
	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}
	app := newTestApp(t, rt)

	err := app.SendTextMessage(context.Background(), "ou_xxx", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.requests) != 1 {
		t.Fatalf("want 1 request, got %d", len(rt.requests))
	}

	req := rt.requests[0]
	if req.method != http.MethodPost {
		t.Fatalf("want POST, got %s", req.method)
	}
	if req.url != MessageAPI {
		t.Fatalf("want url %s, got %s", MessageAPI, req.url)
	}
	if got := req.header.Get("Authorization"); got != "Bearer tenant-token" {
		t.Fatalf("want Authorization Bearer tenant-token, got %q", got)
	}

	payload := decodeMap(t, req.body)
	if got := payload["receive_id"]; got != "ou_xxx" {
		t.Fatalf("want receive_id ou_xxx, got %v", got)
	}
	if got := payload["msg_type"]; got != "text" {
		t.Fatalf("want msg_type text, got %v", got)
	}

	content, ok := payload["content"].(string)
	if !ok {
		t.Fatalf("content is %T, want string", payload["content"])
	}
	contentPayload := decodeMap(t, []byte(content))
	if got := contentPayload["text"]; got != "hello" {
		t.Fatalf("want text hello, got %v", got)
	}
}

func TestApp_SendTextMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		openID  string
		text    string
		wantErr string
	}{
		{name: "empty open_id", text: "hello", wantErr: "open_id is required"},
		{name: "empty text", openID: "ou_xxx", wantErr: "text content is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			app := newTestApp(t, rt)

			err := app.SendTextMessage(context.Background(), tt.openID, tt.text)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if len(rt.requests) != 0 {
				t.Fatalf("want no request, got %d", len(rt.requests))
			}
		})
	}
}

func TestApp_SendImageMessage(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "news-feishu-image-*.png")
	if err != nil {
		t.Fatalf("create temp image: %v", err)
	}
	if _, err := file.Write([]byte("image-data")); err != nil {
		t.Fatalf("write temp image: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp image: %v", err)
	}

	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok","data":{"image_key":"img_xxx"}}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}
	app := newTestApp(t, rt)

	err = app.SendImageMessage(context.Background(), "ou_xxx", file.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(rt.requests))
	}
	if rt.requests[0].url != FeishuUploadImageAPI {
		t.Fatalf("want upload url %s, got %s", FeishuUploadImageAPI, rt.requests[0].url)
	}
	if rt.requests[1].url != MessageAPI {
		t.Fatalf("want message url %s, got %s", MessageAPI, rt.requests[1].url)
	}

	payload := decodeMap(t, rt.requests[1].body)
	if got := payload["msg_type"]; got != "image" {
		t.Fatalf("want msg_type image, got %v", got)
	}
	content, ok := payload["content"].(string)
	if !ok {
		t.Fatalf("content is %T, want string", payload["content"])
	}
	contentPayload := decodeMap(t, []byte(content))
	if got := contentPayload["image_key"]; got != "img_xxx" {
		t.Fatalf("want image_key img_xxx, got %v", got)
	}
}

func TestApp_SendImageMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		openID  string
		path    string
		wantErr string
	}{
		{name: "empty open_id", path: "image.png", wantErr: "open_id is required"},
		{name: "empty path", openID: "ou_xxx", wantErr: "image path is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			app := newTestApp(t, rt)

			err := app.SendImageMessage(context.Background(), tt.openID, tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if len(rt.requests) != 0 {
				t.Fatalf("want no request, got %d", len(rt.requests))
			}
		})
	}
}

func TestApp_SendMessageAPIError(t *testing.T) {
	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":999,"msg":"denied"}`},
		},
	}
	app := newTestApp(t, rt)

	err := app.SendTextMessage(context.Background(), "ou_xxx", "hello")
	if err == nil || !strings.Contains(err.Error(), "code=999, msg=denied") {
		t.Fatalf("want api error, got %v", err)
	}
}

func TestWebhookSendVariants(t *testing.T) {
	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}
	bot, err := New(news.Config{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		HTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}
	if bot.Platform() != news.PlatformFeishu || bot.Stats() == nil {
		t.Fatal("webhook metadata mismatch")
	}
	if err := bot.SendText(context.Background(), "hello", news.WithAtAll()); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if err := bot.SendMarkdown(context.Background(), "title", "body"); err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if err := bot.SendRichText(context.Background(), BuildRichText("title", "body", nil, true)); err != nil {
		t.Fatalf("send rich text: %v", err)
	}
	if err := bot.SendImage(context.Background(), &news.ImageMessage{ImageKey: "img_xxx"}); err != nil {
		t.Fatalf("send image: %v", err)
	}
	if len(rt.requests) != 4 {
		t.Fatalf("want 4 requests, got %d", len(rt.requests))
	}
}

func TestWebhookErrorsAndHelpers(t *testing.T) {
	if _, err := New(news.Config{}); err == nil {
		t.Fatal("expected missing webhook url")
	}
	rt := &recordingRoundTripper{responses: []roundTripResponse{{status: http.StatusOK, body: `{"code":1,"msg":"bad"}`}}}
	bot, err := New(news.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test", HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}
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

	msg := BuildRichTextLines("title", []news.RichTextTag{{Tag: "text", Text: "line"}})
	if msg.Title != "title" || len(msg.Content) != 1 {
		t.Fatalf("unexpected rich text lines: %+v", msg)
	}
}

func TestAccessTokenHelpers(t *testing.T) {
	token := &AccessToken{AppAccessToken: "app", TenantAccessToken: "tenant"}
	if token.AppToken() != "Bearer app" || token.TenantToken() != "Bearer tenant" {
		t.Fatal("token helper mismatch")
	}
	if _, err := GetAccessToken(context.Background(), "", "secret"); err == nil {
		t.Fatal("expected token validation error")
	}
	if _, err := token.UploadImageWithToken(context.Background(), "missing-file"); err == nil {
		t.Fatal("expected upload error")
	}
}

func TestGetAccessToken(t *testing.T) {
	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok","app_access_token":"app","tenant_access_token":"tenant","expire":7200}`},
			{status: http.StatusOK, body: `{"code":1,"msg":"bad"}`},
		},
	}
	client := &http.Client{Transport: rt}
	token, err := GetAccessToken(context.Background(), "cli_xxx", "secret", client)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if token.AppAccessToken != "app" || token.TenantAccessToken != "tenant" || token.Expire != 7200 {
		t.Fatalf("unexpected token: %+v", token)
	}
	if _, err := GetAccessToken(context.Background(), "cli_xxx", "secret", client); err == nil {
		t.Fatal("expected api error")
	}
}

func TestAccessTokenDownloadImage(t *testing.T) {
	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `image-data`},
			{status: http.StatusBadGateway, body: `bad gateway`},
		},
	}
	client := &http.Client{Transport: rt}
	token := &AccessToken{TenantAccessToken: "tenant"}
	path := t.TempDir() + "/image.png"
	if err := token.DownloadImage(context.Background(), "img_xxx", path, client); err != nil {
		t.Fatalf("download image: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded image: %v", err)
	}
	if string(data) != "image-data" {
		t.Fatalf("unexpected image data %q", data)
	}
	if err := token.DownloadImage(context.Background(), "img_xxx", path, client); err == nil {
		t.Fatal("expected status error")
	}
}

func TestWebhookSendImageFromFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "news-feishu-image-*.png")
	if err != nil {
		t.Fatalf("create temp image: %v", err)
	}
	if _, err := file.Write([]byte("image-data")); err != nil {
		t.Fatalf("write temp image: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp image: %v", err)
	}

	rt := &recordingRoundTripper{
		responses: []roundTripResponse{
			{status: http.StatusOK, body: `{"code":0,"msg":"ok","data":{"image_key":"img_xxx"}}`},
			{status: http.StatusOK, body: `{"code":0,"msg":"ok"}`},
		},
	}
	bot, err := New(news.Config{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		HTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}
	if err := bot.SendImageFromFile(context.Background(), "tenant", file.Name()); err != nil {
		t.Fatalf("send image from file: %v", err)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(rt.requests))
	}
}

func newTestApp(t *testing.T, rt http.RoundTripper) *App {
	t.Helper()

	app, err := NewApp(&AccessToken{TenantAccessToken: "tenant-token"}, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app
}

func decodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode json %s: %v", string(data), err)
	}
	return got
}

type recordingRoundTripper struct {
	requests  []recordedRequest
	responses []roundTripResponse
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	rt.requests = append(rt.requests, recordedRequest{
		method: req.Method,
		url:    req.URL.String(),
		header: req.Header.Clone(),
		body:   body,
	})

	if len(rt.responses) == 0 {
		rt.responses = append(rt.responses, roundTripResponse{
			status: http.StatusOK,
			body:   `{"code":0,"msg":"ok"}`,
		})
	}
	resp := rt.responses[0]
	rt.responses = rt.responses[1:]

	return &http.Response{
		StatusCode: resp.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(resp.body)),
		Request:    req,
	}, nil
}

type recordedRequest struct {
	method string
	url    string
	header http.Header
	body   []byte
}

type roundTripResponse struct {
	status int
	body   string
}
