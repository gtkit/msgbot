// Package wecom 实现了企业微信（WeChat Work）群机器人 webhook 的 msgbot.Provider 接口。
// 支持 text、markdown 和 image 消息类型。富文本会降级为 markdown。
//
// 所有方法均可安全并发使用。
package wecom

import (
	"context"
	"fmt"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// 编译期接口检查。
var _ msgbot.Provider = (*Webhook)(nil)

// Webhook 是企业微信 webhook 机器人 Provider。
// 所有字段在构造后不可变，可安全并发使用。
type Webhook struct {
	cfg   msgbot.Config
	stats msgbot.Stats
}

// New 创建一个新的企业微信 webhook Provider。
func New(cfg msgbot.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("wecom: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("wecom: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform 返回平台标识。
func (w *Webhook) Platform() msgbot.Platform { return msgbot.PlatformWeCom }

// Stats 返回 Provider 的发送统计信息。
func (w *Webhook) Stats() *msgbot.Stats { return &w.stats }

// SendText 发送纯文本消息到企业微信群。
// AtAll 与 AtUserIDs 会合并进 mentioned_list，因此可以在同一条消息中
// 同时 @all 和 @指定用户。
func (w *Webhook) SendText(ctx context.Context, text string, opts ...msgbot.SendOption) error {
	if text == "" {
		return msgbot.ValidationError(msgbot.PlatformWeCom, "SendText", "text content is empty", nil)
	}
	o := msgbot.ApplySendOptions(opts)

	textNode := map[string]any{"content": text}
	var mentioned []string
	if o.AtAll {
		mentioned = append(mentioned, "@all")
	}
	mentioned = append(mentioned, o.AtUserIDs...)
	if len(mentioned) > 0 {
		textNode["mentioned_list"] = mentioned
	}

	return w.send(ctx, "SendText", map[string]any{
		"msgtype": "text",
		"text":    textNode,
	})
}

// SendMarkdown 发送 markdown 消息到企业微信群。
// 企业微信支持：标题、加粗、链接、引用，以及通过 <font> 实现的彩色文本。
//
// 企业微信 markdown 无法 @ 指定用户，因此任何 @ 选项都会被忽略
// （并记录一条 debug 日志）。消息仍会被发送，以保证 Multi 广播尽力而为。
// 当 @ 必须通知到用户时，请改用 SendText。
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...msgbot.SendOption) error {
	if content == "" {
		return msgbot.ValidationError(msgbot.PlatformWeCom, "SendMarkdown", "markdown content is empty", nil)
	}
	o := msgbot.ApplySendOptions(opts)
	if o.AtAll || len(o.AtUserIDs) > 0 {
		w.cfg.LogDebug(ctx, "wecom: markdown cannot mention users; @ options ignored", "op", "SendMarkdown")
	}

	md := content
	if title != "" {
		md = "### " + title + "\n" + content
	}

	return w.send(ctx, "SendMarkdown", map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": md},
	})
}

// SendRichText 将 RichTextMessage 转换为 markdown 后发送。
// 企业微信原生不支持飞书风格的富文本。
func (w *Webhook) SendRichText(ctx context.Context, msg *msgbot.RichTextMessage) error {
	if msg == nil {
		return msgbot.ValidationError(msgbot.PlatformWeCom, "SendRichText", "rich text message is nil", nil)
	}
	md := msgbot.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, "", md)
}

// SendImage 发送图片消息到企业微信群。
// ImageMessage 中的 Base64 和 MD5 字段都必须设置。
func (w *Webhook) SendImage(ctx context.Context, img *msgbot.ImageMessage) error {
	if img == nil || img.Base64 == "" || img.MD5 == "" {
		return msgbot.ValidationError(msgbot.PlatformWeCom, "SendImage", "both base64 and md5 are required for image", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msgtype": "image",
		"image": map[string]any{
			"base64": img.Base64,
			"md5":    img.MD5,
		},
	})
}

// send 通过共享发送路径分发 payload。企业微信 webhook
// 不需要请求签名。
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, msgbot.PlatformWeCom, op, func() (string, any, error) {
		return w.cfg.WebhookURL, payload, nil
	})
}
