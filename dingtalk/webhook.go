// Package dingtalk 实现了钉钉自定义机器人 webhook 的 msgbot.Provider 接口。
// 支持 text、markdown、link、ActionCard 和 FeedCard 消息类型，并可选签名。
//
// 所有方法均可安全并发使用。
package dingtalk

import (
	"context"
	"fmt"
	"strings"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// 编译期接口检查。
var _ msgbot.Provider = (*Webhook)(nil)

// Webhook 是钉钉 webhook 机器人 Provider。
// 所有字段在构造后不可变，可安全并发使用。
type Webhook struct {
	cfg   msgbot.Config
	stats msgbot.Stats
}

// New 创建一个新的钉钉 webhook Provider。
func New(cfg msgbot.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("dingtalk: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("dingtalk: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform 返回平台标识。
func (w *Webhook) Platform() msgbot.Platform { return msgbot.PlatformDingTalk }

// Stats 返回 Provider 的发送统计信息。
func (w *Webhook) Stats() *msgbot.Stats { return &w.stats }

// SendText 发送纯文本消息到钉钉群。
// 钉钉只有在正文中出现 @<mobile> 时才会 @ 通知到用户，
// 因此除了 at.atMobiles 之外，被 @ 的手机号也会追加到 content 中。
func (w *Webhook) SendText(ctx context.Context, text string, opts ...msgbot.SendOption) error {
	if text == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendText", "text content is empty", nil)
	}
	o := msgbot.ApplySendOptions(opts)

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

// SendMarkdown 发送 markdown 消息到钉钉群。
// 钉钉支持：标题、加粗、链接、图片、有序/无序列表、引用。
// title 是钉钉必填项。除了 at.atMobiles 之外，被 @ 的手机号也会追加到正文
// （@<mobile>）中，使 @ 能够通知到用户。
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...msgbot.SendOption) error {
	if content == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendMarkdown", "markdown content is empty", nil)
	}
	if title == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendMarkdown", "markdown title is required", nil)
	}
	o := msgbot.ApplySendOptions(opts)

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

// SendRichText 将 RichTextMessage 转换为 markdown 后发送。
// 钉钉原生不支持飞书风格的富文本。title 是钉钉 markdown
// 的必填项。
func (w *Webhook) SendRichText(ctx context.Context, msg *msgbot.RichTextMessage) error {
	if msg == nil {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendRichText", "rich text message is nil", nil)
	}
	if msg.Title == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendRichText", "rich text title is required", nil)
	}
	md := msgbot.RichTextToMarkdown(msg)
	return w.SendMarkdown(ctx, msg.Title, md)
}

// SendImage 将图片 URL 嵌入 markdown 消息中发送。
// 钉钉 webhook 没有专门的 image msg_type，
// 图片通过 markdown ![alt](picURL) 发送。
func (w *Webhook) SendImage(ctx context.Context, img *msgbot.ImageMessage) error {
	if img == nil || img.PicURL == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendImage", "picURL is required for image", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": "image",
			"text":  fmt.Sprintf("![image](%s)", img.PicURL),
		},
	})
}

// atMobiles 将被 @ 的手机号渲染为 "@m1 @m2"，以便注入正文。
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

// SendLink 发送 link 消息（钉钉特有）。
func (w *Webhook) SendLink(ctx context.Context, title, text, messageURL, picURL string) error {
	if title == "" || text == "" || messageURL == "" {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendLink", "title, text, and messageURL are required for link", nil)
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

// ActionCard 表示钉钉 ActionCard 消息的配置。
type ActionCard struct {
	Title          string   // 卡片标题。
	Text           string   // markdown 格式的卡片正文。
	SingleTitle    string   // 单按钮文本（整卡跳转）。
	SingleURL      string   // 单按钮 URL。
	BtnOrientation string   // "0" 表示竖直排列，"1" 表示水平排列。
	Buttons        []Button // 独立按钮（与 SingleTitle 互斥）。
}

// Button 表示钉钉 ActionCard 中的一个按钮。
type Button struct {
	Title     string // 按钮文本。
	ActionURL string // 按钮跳转 URL。
}

// SendActionCard 发送 ActionCard 消息（钉钉特有）。
func (w *Webhook) SendActionCard(ctx context.Context, card *ActionCard) error {
	if card == nil {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendActionCard", "action card is nil", nil)
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

// FeedLink 表示钉钉 FeedCard 中的一项。
type FeedLink struct {
	Title      string // 条目标题。
	MessageURL string // 条目 URL。
	PicURL     string // 条目缩略图 URL。
}

// SendFeedCard 发送 FeedCard 消息（钉钉特有）。
func (w *Webhook) SendFeedCard(ctx context.Context, links []FeedLink) error {
	if len(links) == 0 {
		return msgbot.ValidationError(msgbot.PlatformDingTalk, "SendFeedCard", "feed card requires at least one link", nil)
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

// buildAt 构造钉钉消息 payload 中的 "at" 部分。
func buildAt(o *msgbot.SendOptions) map[string]any {
	at := map[string]any{"isAtAll": o.AtAll}
	if len(o.AtUserIDs) > 0 {
		at["atMobiles"] = o.AtUserIDs
	}
	return at
}

// send 通过共享发送路径分发 payload，在配置了 Secret 时对 URL 签名。
// 签名在每次尝试时重新生成，使 timestamp 在多次重试中始终处于
// 钉钉的有效期窗口内。
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, msgbot.PlatformDingTalk, op, func() (string, any, error) {
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
