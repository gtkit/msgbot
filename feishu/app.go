package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	json "github.com/gtkit/json/v2"
)

const (
	// MessageAPI is the Feishu application message API endpoint for open_id recipients.
	MessageAPI = "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id"
)

// App sends Feishu application messages to open_id recipients.
// It is safe for concurrent use by multiple goroutines.
type App struct {
	token  AccessToken
	client *http.Client
}

// NewApp creates a Feishu application-message client from an AccessToken.
// Callers should cache the token according to AccessToken.Expire.
func NewApp(token *AccessToken, client ...*http.Client) (*App, error) {
	if token == nil {
		return nil, fmt.Errorf("feishu: access token is nil")
	}
	if token.TenantAccessToken == "" {
		return nil, fmt.Errorf("feishu: tenant access token is empty")
	}

	httpClient := http.DefaultClient
	if len(client) > 0 && client[0] != nil {
		httpClient = client[0]
	}

	return &App{
		token:  *token,
		client: httpClient,
	}, nil
}

// SendTextMessage sends a text application message to a Feishu open_id.
func (a *App) SendTextMessage(ctx context.Context, openID, text string) error {
	if openID == "" {
		return fmt.Errorf("feishu: open_id is required")
	}
	if text == "" {
		return fmt.Errorf("feishu: text content is empty")
	}

	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("feishu: marshal text content: %w", err)
	}

	return a.send(ctx, map[string]any{
		"receive_id": openID,
		"msg_type":   "text",
		"content":    string(content),
	})
}

// SendImageMessage uploads a local image and sends it to a Feishu open_id.
func (a *App) SendImageMessage(ctx context.Context, openID, path string) error {
	if openID == "" {
		return fmt.Errorf("feishu: open_id is required")
	}
	if path == "" {
		return fmt.Errorf("feishu: image path is required")
	}

	resp, err := UploadImageFromFile(ctx, a.token.TenantAccessToken, path, a.client)
	if err != nil {
		return fmt.Errorf("feishu: upload image: %w", err)
	}

	content, err := json.Marshal(map[string]string{"image_key": resp.ImageKey()})
	if err != nil {
		return fmt.Errorf("feishu: marshal image content: %w", err)
	}

	return a.send(ctx, map[string]any{
		"receive_id": openID,
		"msg_type":   "image",
		"content":    string(content),
	})
}

func (a *App) send(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, MessageAPI, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feishu: create message request: %w", err)
	}
	req.Header.Set("Authorization", a.token.TenantToken())
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	httpResp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	const maxBody = 1 << 20
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("feishu: read message response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("feishu: send message unexpected status %d: %s", httpResp.StatusCode, string(data))
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
