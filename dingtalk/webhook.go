// Package dingtalk implements the news.Provider interface for
// DingTalk custom robot webhooks. It supports text, markdown, link,
// ActionCard, and FeedCard message types with optional signing.
//
// All methods are safe for concurrent use.
package dingtalk

import (
	"context"
	"fmt"
	json "github.com/gtkit/json/v2"

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
func (w *Webhook) SendText(ctx context.Context, text string, opts ...news.SendOption) error {
	if text == "" {
		return fmt.Errorf("dingtalk: text content is empty")
	}
	o := news.ApplySendOptions(opts)

	return w.send(ctx, map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": text},
		"at":      buildAt(o),
	})
}

// SendMarkdown sends a markdown message to the DingTalk group.
// DingTalk supports: headers, bold, links, images, ordered/unordered lists, quotes.
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...news.SendOption) error {
	if content == "" {
		return fmt.Errorf("dingtalk: markdown content is empty")
	}
	o := news.ApplySendOptions(opts)

	return w.send(ctx, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": title,
			"text":  content,
		},
		"at": buildAt(o),
	})
}

// SendRichText converts a RichTextMessage to markdown and sends it.
// DingTalk does not natively support Feishu-style rich text.
func (w *Webhook) SendRichText(ctx context.Context, msg *news.RichTextMessage) error {
	if msg == nil {
		return fmt.Errorf("dingtalk: rich text message is nil")
	}
	md := news.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, msg.Title, md)
}

// SendImage embeds an image URL in a markdown message.
// DingTalk webhook does not have a dedicated image msg_type;
// images are sent via markdown ![alt](picURL).
func (w *Webhook) SendImage(ctx context.Context, img *news.ImageMessage) error {
	if img == nil || img.PicURL == "" {
		return fmt.Errorf("dingtalk: picURL is required for image")
	}

	return w.send(ctx, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": "image",
			"text":  fmt.Sprintf("![image](%s)", img.PicURL),
		},
	})
}

// SendLink sends a link message (DingTalk-specific).
func (w *Webhook) SendLink(ctx context.Context, title, text, messageURL, picURL string) error {
	if title == "" || text == "" || messageURL == "" {
		return fmt.Errorf("dingtalk: title, text, and messageURL are required for link")
	}

	return w.send(ctx, map[string]any{
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
		return fmt.Errorf("dingtalk: action card is nil")
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

	return w.send(ctx, map[string]any{
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
		return fmt.Errorf("dingtalk: feed card requires at least one link")
	}

	items := make([]map[string]any, 0, len(links))
	for _, l := range links {
		items = append(items, map[string]any{
			"title":      l.Title,
			"messageURL": l.MessageURL,
			"picURL":     l.PicURL,
		})
	}

	return w.send(ctx, map[string]any{
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

// send marshals and posts payload, appending signing params when configured.
func (w *Webhook) send(ctx context.Context, payload map[string]any) error {
	webhookURL := w.cfg.WebhookURL

	if w.cfg.Secret != "" {
		var err error
		webhookURL, err = internal.DingTalkSignedURL(webhookURL, w.cfg.Secret)
		if err != nil {
			return fmt.Errorf("dingtalk: %w", err)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal payload: %w", err)
	}

	w.cfg.LogDebug(ctx, "dingtalk: sending message", "endpoint", internal.URLOriginForLog(webhookURL))

	data, err := internal.PostJSON(ctx, w.cfg.GetHTTPClient(), webhookURL, body)
	if err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "dingtalk: send failed", "error", err)
		return fmt.Errorf("dingtalk: %w", err)
	}

	var resp news.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "dingtalk: decode response failed", "error", err)
		return fmt.Errorf("dingtalk: decode response: %w", err)
	}
	if apiErr := resp.Err(); apiErr != nil {
		w.stats.IncError()
		w.cfg.LogError(ctx, "dingtalk: api error", "error", apiErr)
		return fmt.Errorf("dingtalk: %w", apiErr)
	}

	w.stats.IncSent()
	return nil
}
