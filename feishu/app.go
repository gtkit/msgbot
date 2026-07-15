package feishu

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	json "github.com/gtkit/json/v2"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

const (
	// MessageAPI 是面向 open_id 收件人的飞书应用消息 API 地址。
	MessageAPI = "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id"
)

// TokenSource 为一次 App 操作解析当前的 AccessToken。它天然地
// 与刷新过期 token 的 GetAccessTokenCached 配合使用。
//
// 实现必须可被多个 goroutine 安全并发调用——App 的并发安全性以此为前提；
// GetAccessTokenCached 搭配 MemoryTokenCache 已满足该要求。
type TokenSource func(context.Context) (*AccessToken, error)

// App 向 open_id 收件人发送飞书应用消息。
// 可被多个 goroutine 安全并发使用。
type App struct {
	source TokenSource
	client *http.Client // 由调用方提供；nil 表示使用分级的默认客户端。
}

// NewApp 从一个静态 AccessToken 创建飞书应用消息客户端。
// token 按原样使用且不会刷新；当 token 可能过期时，请使用
// NewAppWithTokenSource，以便每次操作都能获取新的 token。
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

// NewAppWithTokenSource 创建一个飞书应用消息客户端，它在每次操作时从 source
// 解析 token，因此无需重建 App 即可刷新过期的 token。可将 feishu.GetAccessTokenCached
// 包装在闭包中传入，从而将刷新与缓存结合起来。
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

// messageClient 返回用于小型消息请求的客户端：若调用方提供了客户端则使用它，
// 否则使用共享的 10s 超时客户端。
func (a *App) messageClient() *http.Client {
	if a.client != nil {
		return a.client
	}
	return internal.DefaultClient()
}

// uploadClient 返回用于图片上传的客户端：若调用方提供了客户端则使用它，
// 否则使用共享的 30s 超时客户端。
func (a *App) uploadClient() *http.Client {
	if a.client != nil {
		return a.client
	}
	return internal.DefaultUploadClient()
}

// token 为一次操作解析并校验当前的 token。
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

// SendTextMessage 向飞书 open_id 发送一条文本应用消息。
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

// SendImageMessage 上传本地图片并将其发送到飞书 open_id。
// token 只解析一次，并在上传与发送中复用，因此这两个请求
// 绝不会分别使用不同的 token。
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

// send 使用给定的 token 进行鉴权，POST 一个消息 payload。
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
		return msgbot.WrapError(msgbot.PlatformFeishu, "App.Send", internal.SanitizeRequestError(err))
	}
	defer func() { _ = httpResp.Body.Close() }()

	data, err := internal.ReadResponse(httpResp, 1<<20)
	if err != nil {
		return msgbot.WrapError(msgbot.PlatformFeishu, "App.Send", err)
	}

	var respInfo appMessageResp
	if err := json.Unmarshal(data, &respInfo); err != nil {
		return msgbot.DecodeError(msgbot.PlatformFeishu, "App.Send", "decode message response", err)
	}
	if respInfo.Code != 0 {
		return msgbot.PlatformError(msgbot.PlatformFeishu, "App.Send", respInfo.Code, respInfo.Msg)
	}

	return nil
}

type appMessageResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
