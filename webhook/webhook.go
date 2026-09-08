// Package webhook 实现了面向任意 HTTP(S) 端点的通用 msgbot.Provider。
//
// 请求体由调用方通过 PayloadBuilder 决定，发送路径复用 msgbot.Config.Send，
// 因此重试策略、错误分类、日志脱敏、Stats 计数与发送开关的行为都与三个平台
// provider 完全一致。请求固定为 POST + application/json; charset=utf-8。
//
// 适用于 Grafana、Alertmanager、Slack incoming webhook 以及自建告警端点。
// 所有方法均可安全并发使用。
package webhook

import (
	"context"
	"fmt"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// 编译期接口检查。
var _ msgbot.Provider = (*Webhook)(nil)

// MessageKind 标识 Message 承载的消息类型，与 Provider 的四个发送方法一一对应。
type MessageKind string

const (
	KindText     MessageKind = "text"
	KindMarkdown MessageKind = "markdown"
	KindRichText MessageKind = "richtext"
	KindImage    MessageKind = "image"
)

// Message 把 Provider 的四种发送方法归一化成一个结构体，使调用方只需实现
// 一个 PayloadBuilder。按 Kind 判别应读取哪些字段：
//
//	KindText     → Text
//	KindMarkdown → Title、Content
//	KindRichText → RichText
//	KindImage    → Image
//
// Options 始终非 nil。RichText 与 Image 在对应 Kind 下始终非 nil。
type Message struct {
	Kind     MessageKind
	Text     string
	Title    string
	Content  string
	RichText *msgbot.RichTextMessage
	Image    *msgbot.ImageMessage
	Options  *msgbot.SendOptions
}

// PayloadBuilder 把一条归一化消息转换为待序列化为 JSON 的请求体。
// 返回错误会使本次发送立即失败且不重试（归类为 msgbot.KindValidation），
// 因此对不支持的 Kind 直接返回错误即可。
type PayloadBuilder func(*Message) (any, error)

// Webhook 是通用 webhook Provider。
// 所有字段在构造后不可变，可安全并发使用。
type Webhook struct {
	cfg   msgbot.Config
	build PayloadBuilder
	stats msgbot.Stats
}

// New 创建一个通用 webhook Provider。WebhookURL 非法或为空、build 为 nil
// 时返回 error。
func New(cfg msgbot.Config, build PayloadBuilder) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("webhook: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("webhook: invalid webhook URL: %w", err)
	}
	if build == nil {
		return nil, fmt.Errorf("webhook: payload builder is required")
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg, build: build}, nil
}

// Platform 返回平台标识。
func (w *Webhook) Platform() msgbot.Platform { return msgbot.PlatformWebhook }

// Stats 返回 Provider 的发送统计信息。
func (w *Webhook) Stats() *msgbot.Stats { return &w.stats }

// SendText 以 KindText 调用 payload 构造函数并发送结果。
func (w *Webhook) SendText(ctx context.Context, text string, opts ...msgbot.SendOption) error {
	return w.send(ctx, "SendText", &Message{
		Kind:    KindText,
		Text:    text,
		Options: msgbot.ApplySendOptions(opts),
	})
}

// SendMarkdown 以 KindMarkdown 调用 payload 构造函数并发送结果。
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...msgbot.SendOption) error {
	return w.send(ctx, "SendMarkdown", &Message{
		Kind:    KindMarkdown,
		Title:   title,
		Content: content,
		Options: msgbot.ApplySendOptions(opts),
	})
}

// SendRichText 以 KindRichText 调用 payload 构造函数并发送结果。
func (w *Webhook) SendRichText(ctx context.Context, msg *msgbot.RichTextMessage) error {
	if msg == nil {
		return msgbot.ValidationError(msgbot.PlatformWebhook, "SendRichText", "rich text message is nil", nil)
	}
	return w.send(ctx, "SendRichText", &Message{
		Kind:     KindRichText,
		RichText: msg,
		Options:  &msgbot.SendOptions{},
	})
}

// SendImage 以 KindImage 调用 payload 构造函数并发送结果。
func (w *Webhook) SendImage(ctx context.Context, img *msgbot.ImageMessage) error {
	if img == nil {
		return msgbot.ValidationError(msgbot.PlatformWebhook, "SendImage", "image message is nil", nil)
	}
	return w.send(ctx, "SendImage", &Message{
		Kind:    KindImage,
		Image:   img,
		Options: &msgbot.SendOptions{},
	})
}

// send 在每次尝试时调用 build，与平台 provider 的重签名时机保持一致。
func (w *Webhook) send(ctx context.Context, op string, msg *Message) error {
	return w.cfg.Send(ctx, &w.stats, msgbot.PlatformWebhook, op, func() (string, any, error) {
		payload, err := w.build(msg)
		if err != nil {
			return "", nil, err
		}
		return w.cfg.WebhookURL, payload, nil
	})
}
