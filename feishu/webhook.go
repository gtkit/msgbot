// Package feishu implements the news.Provider interface for
// Feishu (Lark) custom robot webhooks. It supports text, rich text (post),
// image, and markdown (interactive card) message types with optional signing.
//
// All methods are safe for concurrent use.
package feishu

import (
	"context"
	"fmt"
	json "github.com/gtkit/json/v2"
	"strconv"
	"time"

	news "github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// compile-time interface check.
var _ news.Provider = (*Webhook)(nil)

// Webhook is a Feishu webhook robot provider.
// All fields are immutable after construction; safe for concurrent use.
type Webhook struct {
	cfg   news.Config
	stats news.Stats
}

// New creates a new Feishu webhook provider.
func New(cfg news.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("feishu: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("feishu: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform returns the platform identifier.
func (w *Webhook) Platform() news.Platform { return news.PlatformFeishu }

// Stats returns the provider's send statistics.
func (w *Webhook) Stats() *news.Stats { return &w.stats }

// SendText sends a plain text message to the Feishu group.
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return fmt.Errorf("feishu: text content is empty")
	}
	o := news.ApplySendOptions(opts)

	content := text
	if o.AtAll {
		content += " <at user_id=\"all\">所有人</at>"
	}
	for _, uid := range o.AtUserIDs {
		content += fmt.Sprintf(" <at user_id=\"%s\">%s</at>", uid, uid)
	}

	return w.send(ctx, map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": content},
	})
}

// SendMarkdown sends a markdown message as an interactive card.
// Feishu webhook does not natively support markdown msg_type;
// this wraps content in an interactive card with markdown rendering.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return fmt.Errorf("feishu: markdown content is empty")
	}

	return w.send(ctx, map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title": map[string]any{
					"tag":     "plain_text",
					"content": title,
				},
			},
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	})
}

// SendRichText sends a rich text (post) message to the Feishu group.
// This is Feishu's native rich text format supporting text, links,
// mentions, and images in a structured layout.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return fmt.Errorf("feishu: rich text message is nil")
	}

	lines := make([]any, 0, len(msg.Content))
	for _, line := range msg.Content {
		elements := make([]map[string]any, 0, len(line))
		for _, tag := range line {
			elem := map[string]any{"tag": tag.Tag}
			switch tag.Tag {
			case "text":
				elem["text"] = tag.Text
			case "a":
				elem["text"] = tag.Text
				elem["href"] = tag.Href
			case "at":
				elem["user_id"] = tag.UserID
			case "img":
				elem["image_key"] = tag.ImgKey
			}
			elements = append(elements, elem)
		}
		lines = append(lines, elements)
	}

	return w.send(ctx, map[string]any{
		"msg_type": "post",
		"content": map[string]any{
			"post": map[string]any{
				"zh_cn": map[string]any{
					"title":   msg.Title,
					"content": lines,
				},
			},
		},
	})
}

// SendImage sends an image message to the Feishu group.
// The ImageKey field must be set (obtained by uploading via Feishu open API).
func (w *Webhook) SendImage(ctx context.Context, img *news.ImageMessage) error {
	if img == nil || img.ImageKey == "" {
		return fmt.Errorf("feishu: image_key is required")
	}

	return w.send(ctx, map[string]any{
		"msg_type": "image",
		"content":  map[string]any{"image_key": img.ImageKey},
	})
}

// send marshals the payload, applies signing if configured, and posts to webhook.
func (w *Webhook) send(ctx context.Context, payload map[string]any) error {
	if w.cfg.Secret != "" {
		ts := time.Now().Unix()
		sign, err := internal.FeishuSign(w.cfg.Secret, ts)
		if err != nil {
			return fmt.Errorf("feishu: generate sign: %w", err)
		}
		payload["timestamp"] = strconv.FormatInt(ts, 10)
		payload["sign"] = sign
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal payload: %w", err)
	}

	w.cfg.LogDebug(ctx, "feishu: sending message", "endpoint", internal.URLOriginForLog(w.cfg.WebhookURL))

	data, err := internal.PostJSON(ctx, w.cfg.GetHTTPClient(), w.cfg.WebhookURL, body)
	if err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "feishu: send failed", "error", err)
		return fmt.Errorf("feishu: %w", err)
	}

	var resp news.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "feishu: decode response failed", "error", err)
		return fmt.Errorf("feishu: decode response: %w", err)
	}
	if apiErr := resp.Err(); apiErr != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "feishu: api error", "error", apiErr)
		return fmt.Errorf("feishu: %w", apiErr)
	}

	w.stats.IncSent()
	return nil
}

// BuildRichText creates a simple rich text message with title, body text,
// an optional hyperlink, and an optional @all mention.
func BuildRichText(title, text string, link *news.RichTextTag, atAll bool) *news.RichTextMessage {
	var elems []news.RichTextTag
	if text != "" {
		elems = append(elems, news.RichTextTag{Tag: "text", Text: text})
	}
	if link != nil {
		elems = append(elems, *link)
	}
	if atAll {
		elems = append(elems, news.RichTextTag{Tag: "at", UserID: "all"})
	}
	return &news.RichTextMessage{
		Title:   title,
		Content: [][]news.RichTextTag{elems},
	}
}

// BuildRichTextLines constructs a RichTextMessage from multiple text lines,
// each provided as a slice of RichTextTag elements.
func BuildRichTextLines(title string, lines ...[]news.RichTextTag) *news.RichTextMessage {
	content := make([][]news.RichTextTag, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 {
			content = append(content, line)
		}
	}
	return &news.RichTextMessage{Title: title, Content: content}
}
