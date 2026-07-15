// Package wecom implements the news.Provider interface for
// WeCom (WeChat Work) group robot webhooks. It supports text,
// markdown, and image message types. Rich text degrades to markdown.
//
// All methods are safe for concurrent use.
package wecom

import (
	"context"
	"fmt"
	json "github.com/gtkit/json/v2"

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
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return fmt.Errorf("wecom: text content is empty")
	}
	o := news.ApplySendOptions(opts)

	textNode := map[string]any{"content": text}
	if o.AtAll {
		textNode["mentioned_list"] = []string{"@all"}
	} else if len(o.AtUserIDs) > 0 {
		textNode["mentioned_list"] = o.AtUserIDs
	}

	return w.send(ctx, map[string]any{
		"msgtype": "text",
		"text":    textNode,
	})
}

// SendMarkdown sends a markdown message to the WeCom group.
// WeCom supports: headers, bold, links, quotes, and colored text via <font>.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return fmt.Errorf("wecom: markdown content is empty")
	}

	md := content
	if title != "" {
		md = "### " + title + "\n" + content
	}

	return w.send(ctx, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": md},
	})
}

// SendRichText converts a RichTextMessage to markdown and sends it.
// WeCom does not natively support Feishu-style rich text.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return fmt.Errorf("wecom: rich text message is nil")
	}
	md := news.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, "", md)
}

// SendImage sends an image message to the WeCom group.
// Both Base64 and MD5 fields must be set in the ImageMessage.
func (w *Webhook) SendImage(ctx context.Context, img *news.ImageMessage) error {
	if img == nil || img.Base64 == "" || img.MD5 == "" {
		return fmt.Errorf("wecom: both base64 and md5 are required for image")
	}

	return w.send(ctx, map[string]any{
		"msgtype": "image",
		"image": map[string]any{
			"base64": img.Base64,
			"md5":    img.MD5,
		},
	})
}

// send marshals and posts the payload to the WeCom webhook URL.
func (w *Webhook) send(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("wecom: marshal payload: %w", err)
	}

	w.cfg.LogDebug(ctx, "wecom: sending message", "endpoint", internal.URLOriginForLog(w.cfg.WebhookURL))

	data, err := internal.PostJSON(ctx, w.cfg.GetHTTPClient(), w.cfg.WebhookURL, body)
	if err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "wecom: send failed", "error", err)
		return fmt.Errorf("wecom: %w", err)
	}

	var resp news.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "wecom: decode response failed", "error", err)
		return fmt.Errorf("wecom: decode response: %w", err)
	}
	if apiErr := resp.Err(); apiErr != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "wecom: api error", "error", apiErr)
		return fmt.Errorf("wecom: %w", apiErr)
	}

	w.stats.IncSent()
	return nil
}
