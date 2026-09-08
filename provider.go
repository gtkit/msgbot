// Package msgbot 提供统一的多平台消息发送能力，
// 支持飞书（Lark）、企业微信（WeChat Work）与钉钉（DingTalk）的 webhook 机器人。
// 所有 Provider 实现均可安全并发使用。
package msgbot

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gtkit/msgbot/internal"
)

// Provider 定义了通过 webhook 向不同 IM 平台发送消息的统一接口。
type Provider interface {
	// SendText 向 webhook 端点发送纯文本消息。
	SendText(ctx context.Context, text string, opts ...SendOption) error
	// SendMarkdown 向 webhook 端点发送 markdown 格式消息。
	SendMarkdown(ctx context.Context, title, content string, opts ...SendOption) error
	// SendRichText 向 webhook 端点发送富文本（post）消息。
	// 飞书原生支持该格式，其他平台降级为 markdown。
	SendRichText(ctx context.Context, msg *RichTextMessage) error
	// SendImage 向 webhook 端点发送图片消息。
	// 飞书使用 image_key，企业微信使用 base64+md5，钉钉使用 markdown 中的 picURL。
	SendImage(ctx context.Context, img *ImageMessage) error
	// Platform 返回该 provider 的平台标识。
	Platform() Platform
}

// Platform 表示一个受支持的 IM 平台。
type Platform string

const (
	PlatformFeishu   Platform = "feishu"
	PlatformWeCom    Platform = "wecom"
	PlatformDingTalk Platform = "dingtalk"
	// PlatformWebhook 标识 webhook 包提供的通用 HTTP(S) 端点 provider。
	PlatformWebhook Platform = "webhook"
)

// Switch 是可在运行期翻转的发送开关。零值即启用，因此 nil 的 *Switch
// 也被视为启用。Disable 之后，所有共享该实例的 provider 的 Send* 调用都
// 直接返回 nil：不发出请求、不计入 Stats，并记一条 debug 日志。
//
// 它的作用域就是「共享该实例的那些 provider」——本包没有全局开关，
// 想让哪些 provider 一起静音，就把同一个 *Switch 放进它们的 Config。
// 状态存放在 atomic.Bool 中，可被多个 goroutine 并发读写。
type Switch struct {
	off atomic.Bool
}

// NewSwitch 返回一个处于启用状态的开关。
func NewSwitch() *Switch { return &Switch{} }

// Enable 恢复发送。对 nil 接收者调用会 panic——静默忽略一次 Enable
// 会让调用方以为已恢复发送。
func (s *Switch) Enable() { s.off.Store(false) }

// Disable 静音发送。对 nil 接收者调用会 panic——静默忽略一次 Disable
// 会让调用方以为已停发，实际仍在发送。
func (s *Switch) Disable() { s.off.Store(true) }

// Enabled 报告当前是否允许发送。nil 接收者返回 true，
// 使「未配置开关」与「开关处于启用态」是同一件事。
func (s *Switch) Enabled() bool { return s == nil || !s.off.Load() }

// SendOption 为消息应用可选参数。
type SendOption func(*SendOptions)

// SendOptions 保存解析后的发送选项。
type SendOptions struct {
	AtAll     bool     // 是否 @所有人。
	AtUserIDs []string // 需要 @ 的平台专属用户标识。
}

// WithAtAll 启用群内 @所有人。
func WithAtAll() SendOption {
	return func(o *SendOptions) {
		o.AtAll = true
	}
}

// WithAtUsers 指定需要 @ 的用户。
// 飞书：user_id 或 open_id 列表；企业微信：userid 列表；钉钉：手机号列表。
func WithAtUsers(ids ...string) SendOption {
	return func(o *SendOptions) {
		o.AtUserIDs = append(o.AtUserIDs, ids...)
	}
}

// ApplySendOptions 将一组 SendOption 解析为 SendOptions 结构体。
func ApplySendOptions(opts []SendOption) *SendOptions {
	o := &SendOptions{}
	for _, fn := range opts {
		fn(o)
	}
	return o
}

// RichTextMessage 表示富文本（post）消息，主要供飞书使用。
// 其他平台降级为 markdown。
type RichTextMessage struct {
	Title   string          // 消息标题。
	Content [][]RichTextTag // 富文本元素的行集合。
}

// RichTextTag 表示富文本行中的单个元素。
type RichTextTag struct {
	Tag    string // 元素类型："text"、"a"、"at"、"img"。
	Text   string // 文本内容（用于 "text" 和 "a" 标签）。
	Href   string // 超链接 URL（用于 "a" 标签）。
	UserID string // 需要 @ 的用户 ID（用于 "at" 标签；"all" 表示所有人）。
	ImgKey string // 图片 key（用于 "img" 标签，仅飞书）。
}

// ImageMessage 表示待发送的图片。
// 不同平台需要填写不同字段。
type ImageMessage struct {
	ImageKey string // 飞书：通过飞书 API 上传后得到的 image_key。
	Base64   string // 企业微信：base64 编码的图片数据（无换行、无前缀）。
	MD5      string // 企业微信：原始图片字节的 md5 哈希。
	PicURL   string // 钉钉：可公开访问的图片 URL。
}

// Logger 定义了用于调试的最小日志接口。它刻意保持精简，
// 使调用方只需几行代码即可适配任意日志库，而无需本包依赖某个具体实现。
// 为 nil 时不输出任何调试日志。
type Logger interface {
	// DebugContext 记录一条带 context 的调试日志。
	DebugContext(ctx context.Context, msg string, args ...any)
	// ErrorContext 记录一条带 context 的错误日志。
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Config 保存 webhook provider 的通用配置。
// Config 设计为按值传递；传入 New() 之后的修改
// 不会影响已创建的 provider。
type Config struct {
	WebhookURL string        // 必填：机器人的 webhook URL。
	Secret     string        // 可选：用于请求校验的签名密钥。
	HTTPClient *http.Client  // 可选：自定义 HTTP 客户端；为 nil 时使用默认值。
	Timeout    time.Duration // 可选：请求超时；HTTPClient 为 nil 时默认为 10s。
	Logger     Logger        // 可选：用于调试/错误日志的 logger；为 nil 时禁用日志。
	Retry      RetryPolicy   // 可选：发送失败的重试策略；零值禁用重试。

	// Switch 可选：运行期发送开关，为 nil 时始终启用。多个 provider 共享
	// 同一实例即可被一次 Disable 同时静音；静音期间 Send* 返回 nil 且不计入 Stats。
	// 与其他字段不同，Switch 在构造后仍可翻转——provider 持有的是指针。
	Switch *Switch

	// frozen 是解析后的 HTTP 客户端，由 Freeze() 设置一次。
	// 冻结后，GetHTTPClient() 始终返回同一个实例。
	frozen *http.Client
}

// Freeze 一次性解析 HTTP 客户端并将其保存在内部。
// 必须在 provider 构造（New）期间调用，以确保
// GetHTTPClient() 不会在热路径上分配新的客户端。
func (c *Config) Freeze() {
	if c.HTTPClient != nil {
		c.frozen = c.HTTPClient
		return
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	c.frozen = &http.Client{Timeout: timeout}
}

// GetHTTPClient 返回已冻结的 HTTP 客户端。
// 调用前必须已执行 Freeze()（所有 New 函数都会这样做）。
func (c *Config) GetHTTPClient() *http.Client {
	if c.frozen != nil {
		return c.frozen
	}
	// 兜底：未经 Freeze 就被直接使用（例如外部直接调用 Config.Send）时，
	// 返回带默认超时的共享 client，绝不落到无超时的 http.DefaultClient。
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return internal.DefaultClient()
}

// Muted 报告发送是否已被 Switch 静音。面向 Provider 扩展使用：在实际发送
// 之前还会额外发起网络请求的便捷方法，应先用它提前返回。
func (c *Config) Muted() bool { return !c.Switch.Enabled() }

// LogDebug 在已配置 logger 时记录一条调试日志。
func (c *Config) LogDebug(ctx context.Context, msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.DebugContext(ctx, msg, args...)
	}
}

// LogError 在已配置 logger 时记录一条错误日志。
func (c *Config) LogError(ctx context.Context, msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.ErrorContext(ctx, msg, args...)
	}
}

// Response 表示 IM 平台返回的通用 API 响应。
type Response struct {
	Code    int    `json:"code,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
	Msg     string `json:"msg,omitempty"`
}

// code 返回有效的业务码，优先取 Code，其次取 ErrCode。
func (r *Response) code() int {
	if r.Code != 0 {
		return r.Code
	}
	return r.ErrCode
}

// message 返回有效的消息文本，优先取 Msg，其次取 ErrMsg。
func (r *Response) message() string {
	if r.Msg != "" {
		return r.Msg
	}
	return r.ErrMsg
}

// Err 在响应表示失败时返回非 nil 的 error。
func (r *Response) Err() error {
	if code := r.code(); code != 0 {
		return fmt.Errorf("api error: code=%d, msg=%s", code, r.message())
	}
	return nil
}

// Stats 使用 atomic 操作跟踪 provider 级别的指标，
// 实现无锁、并发安全的访问，且不带任何 mutex 开销。
//
// 三类计数互斥，一次发送任务只会落到其中一个：成功计 sent，失败计 error，
// 被开关静音计 muted。静音既不算成功也不算失败，因此不会污染由
// sent/error 算出的成功率。
type Stats struct {
	totalSent  atomic.Int64
	totalError atomic.Int64
	totalMuted atomic.Int64
}

// IncSent 以 atomic 方式递增成功发送计数器。
func (s *Stats) IncSent() { s.totalSent.Add(1) }

// IncError 以 atomic 方式递增错误计数器。
func (s *Stats) IncError() { s.totalError.Add(1) }

// IncMuted 以 atomic 方式递增静音计数器。
func (s *Stats) IncMuted() { s.totalMuted.Add(1) }

// TotalSent 返回成功发送的消息总数。
func (s *Stats) TotalSent() int64 { return s.totalSent.Load() }

// TotalError 返回发送失败的消息总数（按发送任务计，重试多次仅计一次）。
func (s *Stats) TotalError() int64 { return s.totalError.Load() }

// TotalMuted 返回因发送开关关闭而被丢弃的消息总数（按发送任务计）。
// 它是静音期间唯一无需任何配置即可读取的痕迹——静音时 Send* 返回 nil，
// 在调用点与成功无法区分，而 sent/error 两个计数都不会变动。
func (s *Stats) TotalMuted() int64 { return s.totalMuted.Load() }
