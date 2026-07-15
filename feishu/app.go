package feishu

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	json "github.com/gtkit/json/v2"

	"github.com/gtkit/msgbot/internal"
)

const (
	// MessageAPI is the Feishu application message API endpoint for open_id recipients.
	MessageAPI = "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id"
)

// TokenSource resolves the current AccessToken for an App operation. It pairs
// naturally with GetAccessTokenCached, which refreshes an expired token.
type TokenSource func(context.Context) (*AccessToken, error)

// App sends Feishu application messages to open_id recipients.
// It is safe for concurrent use by multiple goroutines.
type App struct {
	source TokenSource
	client *http.Client // caller-supplied; nil means use the tiered default clients.
}

// NewApp creates a Feishu application-message client from a static AccessToken.
// The token is used as-is and does not refresh; when the token may expire, use
// NewAppWithTokenSource so each operation can obtain a fresh token.
func NewApp(token *AccessToken, client ...*http.Client) (*App, error) {
	if token == nil {
		return nil, fmt.Errorf("feishu: access token is nil")
	}
	if token.TenantAccessToken == "" {
		return nil, fmt.Errorf("feishu: tenant access token is empty")
	}
	snapshot := *token
	return NewAppWithTokenSource(func(context.Context) (*AccessToken, error) {
		return &snapshot, nil
	}, client...)
}

// NewAppWithTokenSource creates a Feishu application-message client that resolves
// its token from source on each operation, so an expired token can be refreshed
// without rebuilding the App. Pass feishu.GetAccessTokenCached wrapped in a
// closure to combine refresh with caching.
func NewAppWithTokenSource(source TokenSource, client ...*http.Client) (*App, error) {
	if source == nil {
		return nil, fmt.Errorf("feishu: token source is nil")
	}
	var c *http.Client
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &App{source: source, client: c}, nil
}

// messageClient returns the client for small message requests: the caller's
// client if provided, otherwise the shared 10s-timeout client.
func (a *App) messageClient() *http.Client {
	if a.client != nil {
		return a.client
	}
	return internal.DefaultClient()
}

// uploadClient returns the client for image upload: the caller's client if
// provided, otherwise the shared 30s-timeout client.
func (a *App) uploadClient() *http.Client {
	if a.client != nil {
		return a.client
	}
	return internal.DefaultUploadClient()
}

// token resolves and validates the current token for one operation.
func (a *App) token(ctx context.Context) (*AccessToken, error) {
	t, err := a.source(ctx)
	if err != nil {
		return nil, fmt.Errorf("feishu: resolve token: %w", err)
	}
	if t == nil || t.TenantAccessToken == "" {
		return nil, fmt.Errorf("feishu: token source returned an empty tenant access token")
	}
	return t, nil
}

// SendTextMessage sends a text application message to a Feishu open_id.
func (a *App) SendTextMessage(ctx context.Context, openID, text string) error {
	if openID == "" {
		return fmt.Errorf("feishu: open_id is required")
	}
	if text == "" {
		return fmt.Errorf("feishu: text content is empty")
	}

	token, err := a.token(ctx)
	if err != nil {
		return err
	}

	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("feishu: marshal text content: %w", err)
	}

	return a.send(ctx, token, map[string]any{
		"receive_id": openID,
		"msg_type":   "text",
		"content":    string(content),
	})
}

// SendImageMessage uploads a local image and sends it to a Feishu open_id.
// The token is resolved once and reused for both the upload and the send, so
// the two requests never split across different tokens.
func (a *App) SendImageMessage(ctx context.Context, openID, path string) error {
	if openID == "" {
		return fmt.Errorf("feishu: open_id is required")
	}
	if path == "" {
		return fmt.Errorf("feishu: image path is required")
	}

	token, err := a.token(ctx)
	if err != nil {
		return err
	}

	resp, err := UploadImageFromFile(ctx, token.TenantAccessToken, path, a.uploadClient())
	if err != nil {
		return fmt.Errorf("feishu: upload image: %w", err)
	}

	content, err := json.Marshal(map[string]string{"image_key": resp.ImageKey()})
	if err != nil {
		return fmt.Errorf("feishu: marshal image content: %w", err)
	}

	return a.send(ctx, token, map[string]any{
		"receive_id": openID,
		"msg_type":   "image",
		"content":    string(content),
	})
}

// send posts a message payload authenticated with the given token.
func (a *App) send(ctx context.Context, token *AccessToken, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, MessageAPI, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feishu: create message request: %w", err)
	}
	req.Header.Set("Authorization", token.TenantToken())
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	httpResp, err := a.messageClient().Do(req)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", internal.SanitizeRequestError(err))
	}
	defer func() { _ = httpResp.Body.Close() }()

	data, err := internal.ReadResponse(httpResp, 1<<20)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", err)
	}

	var respInfo appMessageResp
	if err := json.Unmarshal(data, &respInfo); err != nil {
		return fmt.Errorf("feishu: decode message response: %w", err)
	}
	if respInfo.Code != 0 {
		return fmt.Errorf("feishu: send message: code=%d, msg=%s", respInfo.Code, respInfo.Msg)
	}

	return nil
}

type appMessageResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
