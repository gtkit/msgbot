// Package wecom implements the news.Provider interface for
// WeCom (WeChat Work) group robot webhooks. It supports text,
// markdown, and image message types. Rich text degrades to markdown.
//
// All methods are safe for concurrent use.
package wecom

import (
	"context"
	"fmt"

	news "github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// compile-time interface check.
var _ news.Provider = (*Webhook)(nil)

// Webhook is a WeCom webhook robot provider.
// All fields are immutable after construction; safe for concurrent use.
type Webhook struct {
	cfg   news.Config
	stats news.Stats
}

// New creates a new WeCom webhook provider.
func New(cfg news.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("wecom: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("wecom: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform returns the platform identifier.
func (w *Webhook) Platform() news.Platform { return news.PlatformWeCom }

// Stats returns the provider's send statistics.
func (w *Webhook) Stats() *news.Stats { return &w.stats }

// SendText sends a plain text message to the WeCom group.
// AtAll and AtUserIDs are merged into mentioned_list, so both @all and specific
// users can be mentioned in the same message.
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return news.ValidationError(news.PlatformWeCom, "SendText", "text content is empty", nil)
	}
	o := news.ApplySendOptions(opts)

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

// SendMarkdown sends a markdown message to the WeCom group.
// WeCom supports: headers, bold, links, quotes, and colored text via <font>.
//
// WeCom markdown cannot mention specific users, so any @ options are ignored
// (a debug log records this). The message is still sent so a Multi broadcast
// stays best-effort. Use SendText when a mention must notify a user.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return news.ValidationError(news.PlatformWeCom, "SendMarkdown", "markdown content is empty", nil)
	}
	o := news.ApplySendOptions(opts)
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

// SendRichText converts a RichTextMessage to markdown and sends it.
// WeCom does not natively support Feishu-style rich text.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return news.ValidationError(news.PlatformWeCom, "SendRichText", "rich text message is nil", nil)
	}
	md := news.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, "", md)
}

// SendImage sends an image message to the WeCom group.
// Both Base64 and MD5 fields must be set in the ImageMessage.
func (w *Webhook) SendImage(ctx context.Context, img *news.ImageMessage) error {
	if img == nil || img.Base64 == "" || img.MD5 == "" {
		return news.ValidationError(news.PlatformWeCom, "SendImage", "both base64 and md5 are required for image", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msgtype": "image",
		"image": map[string]any{
			"base64": img.Base64,
			"md5":    img.MD5,
		},
	})
}

// send dispatches the payload through the shared send path. WeCom webhook
// requires no request signing.
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, news.PlatformWeCom, op, func() (string, any, error) {
		return w.cfg.WebhookURL, payload, nil
	})
}
