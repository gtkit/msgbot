// Package feishu implements the news.Provider interface for
// Feishu (Lark) custom robot webhooks. It supports text, rich text (post),
// image, and markdown (interactive card) message types with optional signing.
//
// All methods are safe for concurrent use.
package feishu

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
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

// atEscaper escapes characters that would break a Feishu <at> tag structure.
var atEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// escapeAt escapes a user identifier for safe interpolation into an <at> tag.
func escapeAt(s string) string { return atEscaper.Replace(s) }

// SendText sends a plain text message to the Feishu group.
// Mentioned user identifiers are escaped before being placed in <at> tags.
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return news.ValidationError(news.PlatformFeishu, "SendText", "text content is empty", nil)
	}
	o := news.ApplySendOptions(opts)

	var b strings.Builder
	b.WriteString(text)
	if o.AtAll {
		b.WriteString(` <at user_id="all">所有人</at>`)
	}
	for _, uid := range o.AtUserIDs {
		esc := escapeAt(uid)
		b.WriteString(` <at user_id="`)
		b.WriteString(esc)
		b.WriteString(`">`)
		b.WriteString(esc)
		b.WriteString(`</at>`)
	}

	return w.send(ctx, "SendText", map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": b.String()},
	})
}

// SendMarkdown sends a markdown message as an interactive card.
// Feishu webhook does not natively support markdown msg_type;
// this wraps content in an interactive card with markdown rendering.
// Mentions use the interactive-card <at id=...></at> syntax, which differs
// from the text-message <at user_id="...">...</at> syntax.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return news.ValidationError(news.PlatformFeishu, "SendMarkdown", "markdown content is empty", nil)
	}
	o := news.ApplySendOptions(opts)

	md := content
	if mentions := cardMentions(o); mentions != "" {
		md += "\n" + mentions
	}

	return w.send(ctx, "SendMarkdown", map[string]any{
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
					"content": md,
				},
			},
		},
	})
}

// cardMentions renders send options as interactive-card <at> elements.
func cardMentions(o *news.SendOptions) string {
	var parts []string
	if o.AtAll {
		parts = append(parts, "<at id=all></at>")
	}
	for _, uid := range o.AtUserIDs {
		parts = append(parts, "<at id="+escapeAt(uid)+"></at>")
	}
	return strings.Join(parts, " ")
}

// SendRichText sends a rich text (post) message to the Feishu group.
// This is Feishu's native rich text format supporting text, links,
// mentions, and images in a structured layout.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return news.ValidationError(news.PlatformFeishu, "SendRichText", "rich text message is nil", nil)
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

	return w.send(ctx, "SendRichText", map[string]any{
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
		return news.ValidationError(news.PlatformFeishu, "SendImage", "image_key is required", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msg_type": "image",
		"content":  map[string]any{"image_key": img.ImageKey},
	})
}

// send dispatches the payload through the shared send path, applying Feishu
// signing when a secret is configured. Signing is regenerated on every attempt
// so retries carry a fresh timestamp, and the payload is cloned so retries do
// not accumulate stale timestamp/sign fields.
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, news.PlatformFeishu, op, func() (string, any, error) {
		if w.cfg.Secret == "" {
			return w.cfg.WebhookURL, payload, nil
		}
		ts := time.Now().Unix()
		sign, err := internal.FeishuSign(w.cfg.Secret, ts)
		if err != nil {
			return "", nil, fmt.Errorf("generate sign: %w", err)
		}
		signed := maps.Clone(payload)
		signed["timestamp"] = strconv.FormatInt(ts, 10)
		signed["sign"] = sign
		return w.cfg.WebhookURL, signed, nil
	})
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
