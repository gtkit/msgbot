package feishu

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// messageAPIBase 是飞书应用消息 API 地址，receive_id_type 由发送时按
// ReceiveIDType 拼接。
const messageAPIBase = "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type="

// MessageAPI 是面向 open_id 收件人的飞书应用消息 API 地址。
const MessageAPI = messageAPIBase + string(ReceiveIDOpenID)

// ReceiveIDType 标识飞书应用消息中 receive_id 的类型，
// 对应飞书开放平台的 receive_id_type 查询参数。
type ReceiveIDType string

const (
	ReceiveIDOpenID  ReceiveIDType = "open_id"
	ReceiveIDUserID  ReceiveIDType = "user_id"
	ReceiveIDUnionID ReceiveIDType = "union_id"
	ReceiveIDEmail   ReceiveIDType = "email"
	ReceiveIDChatID  ReceiveIDType = "chat_id"
)

// valid 报告该类型是否为飞书接受的取值。取值来自白名单，因此拼进 URL
// 查询串时不存在注入面。
func (t ReceiveIDType) valid() bool {
	switch t {
	case ReceiveIDOpenID, ReceiveIDUserID, ReceiveIDUnionID, ReceiveIDEmail, ReceiveIDChatID:
		return true
	}
	return false
}

// TokenSource 为一次 App 操作解析当前的 AccessToken。它天然地
// 与刷新过期 token 的 GetAccessTokenCached 配合使用。
//
// 实现必须可被多个 goroutine 安全并发调用——App 的并发安全性以此为前提；
// GetAccessTokenCached 搭配 MemoryTokenCache 已满足该要求。
type TokenSource func(context.Context) (*AccessToken, error)

// App 发送飞书应用消息。
// 所有字段在构造后不可变，可被多个 goroutine 安全并发使用。
type App struct {
	source TokenSource
	client *http.Client   // 由调用方提供；nil 表示使用分级的默认客户端。
	sw     *msgbot.Switch // 运行期发送开关；nil 表示始终启用。
}

// AppConfig 配置飞书应用消息客户端。用法与 msgbot.Config 一致：以结构体
// 字面量构造，传入 NewAppWithConfig 之后对它的修改不影响已创建的 App。
//
// Token 与 Source 二选一，必须恰好提供一个：两者都为空或都提供都会返回错误，
// 而不是静默采用某一个——静默的优先级规则会让调用方以为自己配的那个生效了。
type AppConfig struct {
	// Token 是静态 access token，按原样使用且不会刷新，过期后需重建 App。
	// 适合一次性或短生命周期场景。
	Token *AccessToken
	// Source 在每次操作时解析当前 token，因此可刷新过期 token，
	// 长期运行的服务用它。实现必须可被多个 goroutine 安全并发调用。
	Source TokenSource
	// HTTPClient 可选：自定义客户端；为 nil 时按操作类型使用分级默认客户端
	//（消息 10s、图片上传 30s）。
	HTTPClient *http.Client
	// Switch 可选：运行期发送开关，为 nil 时始终启用。与三平台 webhook
	// provider 共享同一实例即可被一次 Disable 同时静音。
	Switch *msgbot.Switch
}

// staticSource 把一个静态 token 包装成 TokenSource。token 在此处被快照，
// 调用方后续修改原对象不会影响已创建的 App。
func staticSource(token *AccessToken) TokenSource {
	snapshot := *token
	return func(context.Context) (*AccessToken, error) {
		return &snapshot, nil
	}
}

// NewAppWithConfig 按 AppConfig 创建飞书应用消息客户端。它是唯一能配置
// 发送开关的构造函数；NewApp 与 NewAppWithTokenSource 创建的 App 不受开关控制。
func NewAppWithConfig(cfg AppConfig) (*App, error) {
	const op = "NewAppWithConfig"
	switch {
	case cfg.Token == nil && cfg.Source == nil:
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, op, "exactly one of Token or Source is required, got neither", nil)
	case cfg.Token != nil && cfg.Source != nil:
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, op, "exactly one of Token or Source is required, got both", nil)
	}

	source := cfg.Source
	if source == nil {
		if cfg.Token.TenantAccessToken == "" {
			return nil, msgbot.ValidationError(msgbot.PlatformFeishu, op, "tenant access token is empty", nil)
		}
		source = staticSource(cfg.Token)
	}
	return &App{source: source, client: cfg.HTTPClient, sw: cfg.Switch}, nil
}

// NewApp 从一个静态 AccessToken 创建飞书应用消息客户端。
// token 按原样使用且不会刷新；当 token 可能过期时，请使用
// NewAppWithTokenSource，以便每次操作都能获取新的 token。
//
// 需要发送开关时改用 NewAppWithConfig：本函数创建的 App 不受开关控制。
func NewApp(token *AccessToken, client ...*http.Client) (*App, error) {
	if token == nil {
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, "NewApp", "access token is nil", nil)
	}
	if token.TenantAccessToken == "" {
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, "NewApp", "tenant access token is empty", nil)
	}
	return NewAppWithTokenSource(staticSource(token), client...)
}

// NewAppWithTokenSource 创建一个飞书应用消息客户端，它在每次操作时从 source
// 解析 token，因此无需重建 App 即可刷新过期的 token。可将 feishu.GetAccessTokenCached
// 包装在闭包中传入，从而将刷新与缓存结合起来。
//
// 需要发送开关时改用 NewAppWithConfig：本函数创建的 App 不受开关控制。
func NewAppWithTokenSource(source TokenSource, client ...*http.Client) (*App, error) {
	if source == nil {
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, "NewAppWithTokenSource", "token source is nil", nil)
	}
	return &App{source: source, client: internal.PickClient(nil, client)}, nil
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

// muted 报告发送是否已被开关静音。检查点在参数校验之后、解析 token 之前：
// 校验错误是编程错误，静音期间也应照常暴露；而 token 解析与后续上传都是真实的
// 飞书 API 调用，静音时必须全部跳过。
func (a *App) muted() bool { return !a.sw.Enabled() }

// token 为一次操作解析并校验当前的 token。
func (a *App) token(ctx context.Context) (*AccessToken, error) {
	t, err := a.source(ctx)
	if err != nil {
		return nil, fmt.Errorf("feishu: resolve token: %w", err)
	}
	if t == nil || t.TenantAccessToken == "" {
		return nil, msgbot.ValidationError(msgbot.PlatformFeishu, "App.token", "token source returned an empty tenant access token", nil)
	}
	return t, nil
}

// SendTextMessage 向飞书 open_id 发送一条文本应用消息。
// 需要按其他收件人类型（user_id / union_id / email / chat_id）投递时，
// 使用 SendTextMessageTo。
func (a *App) SendTextMessage(ctx context.Context, openID, text string) error {
	return a.SendTextMessageTo(ctx, ReceiveIDOpenID, openID, text)
}

// SendTextMessageTo 按指定的收件人类型发送一条文本应用消息。
// idType 非白名单取值、receiveID 或 text 为空时，在本地即返回校验错误，
// 不解析 token、也不发出任何请求。
//
// 若 App 由 NewAppWithConfig 配置了已关闭的 Switch，则在参数校验通过后
// 直接返回 nil：不解析 token、不发请求。
func (a *App) SendTextMessageTo(ctx context.Context, idType ReceiveIDType, receiveID, text string) error {
	if !idType.valid() {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendTextMessage", "unsupported receive id type", nil)
	}
	if receiveID == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendTextMessage", "receive_id is required", nil)
	}
	if text == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendTextMessage", "text content is empty", nil)
	}
	if a.muted() {
		return nil
	}

	token, err := a.token(ctx)
	if err != nil {
		return err
	}

	content, err := internal.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("feishu: marshal text content: %w", err)
	}

	return a.send(ctx, idType, token, map[string]any{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    string(content),
	})
}

// SendImageMessage 上传本地图片并将其发送到飞书 open_id。
// 需要按其他收件人类型投递时，使用 SendImageMessageTo。
func (a *App) SendImageMessage(ctx context.Context, openID, path string) error {
	return a.SendImageMessageTo(ctx, ReceiveIDOpenID, openID, path)
}

// SendImageMessageTo 上传本地图片并按指定的收件人类型发送。
// token 只解析一次，并在上传与发送中复用，因此这两个请求
// 绝不会分别使用不同的 token。
//
// 若 App 由 NewAppWithConfig 配置了已关闭的 Switch，则在参数校验通过后
// 直接返回 nil：不解析 token、不上传图片、不发请求。
func (a *App) SendImageMessageTo(ctx context.Context, idType ReceiveIDType, receiveID, path string) error {
	if !idType.valid() {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendImageMessage", "unsupported receive id type", nil)
	}
	if receiveID == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendImageMessage", "receive_id is required", nil)
	}
	if path == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "App.SendImageMessage", "image path is required", nil)
	}
	if a.muted() {
		return nil
	}

	token, err := a.token(ctx)
	if err != nil {
		return err
	}

	resp, err := UploadImageFromFile(ctx, token.TenantAccessToken, path, a.uploadClient())
	if err != nil {
		return fmt.Errorf("feishu: upload image: %w", err)
	}

	content, err := internal.Marshal(map[string]string{"image_key": resp.ImageKey()})
	if err != nil {
		return fmt.Errorf("feishu: marshal image content: %w", err)
	}

	return a.send(ctx, idType, token, map[string]any{
		"receive_id": receiveID,
		"msg_type":   "image",
		"content":    string(content),
	})
}

// send 使用给定的 token 进行鉴权，POST 一个消息 payload。
func (a *App) send(ctx context.Context, idType ReceiveIDType, token *AccessToken, payload map[string]any) error {
	body, err := internal.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messageAPIBase+string(idType), bytes.NewReader(body))
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
	if err := internal.Unmarshal(data, &respInfo); err != nil {
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
