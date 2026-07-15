// Package dingtalk implements the news.Provider interface for
// DingTalk custom robot webhooks. It supports text, markdown, link,
// ActionCard, and FeedCard message types with optional signing.
//
// All methods are safe for concurrent use.
package dingtalk

import (
	"context"
	"fmt"
	"strings"

	news "github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// compile-time interface check.
var _ news.Provider = (*Webhook)(nil)

// Webhook is a DingTalk webhook robot provider.
// All fields are immutable after construction; safe for concurrent use.
type Webhook struct {
	cfg   news.Config
	stats news.Stats
}

// New creates a new DingTalk webhook provider.
func New(cfg news.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("dingtalk: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("dingtalk: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform returns the platform identifier.
func (w *Webhook) Platform() news.Platform { return news.PlatformDingTalk }

// Stats returns the provider's send statistics.
func (w *Webhook) Stats() *news.Stats { return &w.stats }

// SendText sends a plain text message to the DingTalk group.
// A DingTalk mention notifies a user only when @<mobile> appears in the body,
// so mentioned mobiles are appended to the content in addition to at.atMobiles.
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendText", "text content is empty", nil)
	}
	o := news.ApplySendOptions(opts)

	content := text
	if mentions := atMobiles(o.AtUserIDs); mentions != "" {
		content += " " + mentions
	}

	return w.send(ctx, "SendText", map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": content},
		"at":      buildAt(o),
	})
}

// SendMarkdown sends a markdown message to the DingTalk group.
// DingTalk supports: headers, bold, links, images, ordered/unordered lists, quotes.
// The title is required by DingTalk. Mentioned mobiles are appended to the body
// (@<mobile>) so the mention notifies, in addition to at.atMobiles.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendMarkdown", "markdown content is empty", nil)
	}
	if title == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendMarkdown", "markdown title is required", nil)
	}
	o := news.ApplySendOptions(opts)

	text := content
	if mentions := atMobiles(o.AtUserIDs); mentions != "" {
		text += "\n\n" + mentions
	}

	return w.send(ctx, "SendMarkdown", map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": title,
			"text":  text,
		},
		"at": buildAt(o),
	})
}

// SendRichText converts a RichTextMessage to markdown and sends it.
// DingTalk does not natively support Feishu-style rich text. The title is
// required by DingTalk markdown.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return news.ValidationError(news.PlatformDingTalk, "SendRichText", "rich text message is nil", nil)
	}
	if msg.Title == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendRichText", "rich text title is required", nil)
	}
	md := news.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, msg.Title, md)
}

// SendImage embeds an image URL in a markdown message.
// DingTalk webhook does not have a dedicated image msg_type;
// images are sent via markdown ![alt](picURL).
func (w *Webhook) SendImage(ctx context.Context, img *news.ImageMessage) error {
	if img == nil || img.PicURL == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendImage", "picURL is required for image", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": "image",
			"text":  fmt.Sprintf("![image](%s)", img.PicURL),
		},
	})
}

// atMobiles renders mentioned mobiles as "@m1 @m2" for injection into the body.
func atMobiles(mobiles []string) string {
	if len(mobiles) == 0 {
		return ""
	}
	parts := make([]string, len(mobiles))
	for i, m := range mobiles {
		parts[i] = "@" + m
	}
	return strings.Join(parts, " ")
}

// SendLink sends a link message (DingTalk-specific).
func (w *Webhook) SendLink(ctx context.Context, title, text, messageURL, picURL string) error {
	if title == "" || text == "" || messageURL == "" {
		return news.ValidationError(news.PlatformDingTalk, "SendLink", "title, text, and messageURL are required for link", nil)
	}

	return w.send(ctx, "SendLink", map[string]any{
		"msgtype": "link",
		"link": map[string]any{
			"title":      title,
			"text":       text,
			"messageUrl": messageURL,
			"picUrl":     picURL,
		},
	})
}

// ActionCard represents a DingTalk ActionCard message configuration.
type ActionCard struct {
	Title          string   // Card title.
	Text           string   // Card body in markdown.
	SingleTitle    string   // Single button text (whole-card jump).
	SingleURL      string   // Single button URL.
	BtnOrientation string   // "0" for vertical, "1" for horizontal.
	Buttons        []Button // Independent buttons (exclusive with SingleTitle).
}

// Button represents a button in a DingTalk ActionCard.
type Button struct {
	Title     string // Button text.
	ActionURL string // Button target URL.
}

// SendActionCard sends an ActionCard message (DingTalk-specific).
func (w *Webhook) SendActionCard(ctx context.Context, card *ActionCard) error {
	if card == nil {
		return news.ValidationError(news.PlatformDingTalk, "SendActionCard", "action card is nil", nil)
	}

	ac := map[string]any{
		"title":          card.Title,
		"text":           card.Text,
		"btnOrientation": card.BtnOrientation,
	}

	if len(card.Buttons) > 0 {
		btns := make([]map[string]any, 0, len(card.Buttons))
		for _, b := range card.Buttons {
			btns = append(btns, map[string]any{
				"title":     b.Title,
				"actionURL": b.ActionURL,
			})
		}
		ac["btns"] = btns
	} else {
		ac["singleTitle"] = card.SingleTitle
		ac["singleURL"] = card.SingleURL
	}

	return w.send(ctx, "SendActionCard", map[string]any{
		"msgtype":    "actionCard",
		"actionCard": ac,
	})
}

// FeedLink represents one item in a DingTalk FeedCard.
type FeedLink struct {
	Title      string // Item title.
	MessageURL string // Item URL.
	PicURL     string // Item thumbnail URL.
}

// SendFeedCard sends a FeedCard message (DingTalk-specific).
func (w *Webhook) SendFeedCard(ctx context.Context, links []FeedLink) error {
	if len(links) == 0 {
		return news.ValidationError(news.PlatformDingTalk, "SendFeedCard", "feed card requires at least one link", nil)
	}

	items := make([]map[string]any, 0, len(links))
	for _, l := range links {
		items = append(items, map[string]any{
			"title":      l.Title,
			"messageURL": l.MessageURL,
			"picURL":     l.PicURL,
		})
	}

	return w.send(ctx, "SendFeedCard", map[string]any{
		"msgtype":  "feedCard",
		"feedCard": map[string]any{"links": items},
	})
}

// buildAt constructs the "at" section of a DingTalk message payload.
func buildAt(o *news.SendOptions) map[string]any {
	at := map[string]any{"isAtAll": o.AtAll}
	if len(o.AtUserIDs) > 0 {
		at["atMobiles"] = o.AtUserIDs
	}
	return at
}

// send dispatches the payload through the shared send path, signing the URL
// when a secret is configured. Signing is regenerated on every attempt so the
// timestamp stays within DingTalk's validity window across retries.
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, news.PlatformDingTalk, op, func() (string, any, error) {
		url := w.cfg.WebhookURL
		if w.cfg.Secret != "" {
			signed, err := internal.DingTalkSignedURL(url, w.cfg.Secret)
			if err != nil {
				return "", nil, fmt.Errorf("sign url: %w", err)
			}
			url = signed
		}
		return url, payload, nil
	})
}
