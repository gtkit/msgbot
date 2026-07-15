// Package feishu 实现了飞书（Lark）自定义机器人 webhook 的 msgbot.Provider 接口。
// 支持 text、rich text（post）、image 和 markdown（interactive 卡片）消息类型，
// 并可选签名。
//
// 所有方法均可安全并发使用。
package feishu

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

// 编译期接口检查。
var _ msgbot.Provider = (*Webhook)(nil)

// Webhook 是飞书 webhook 机器人 Provider。
// 所有字段在构造后不可变，可安全并发使用。
type Webhook struct {
	cfg   msgbot.Config
	stats msgbot.Stats
}

// New 创建一个新的飞书 webhook Provider。
func New(cfg msgbot.Config) (*Webhook, error) {
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("feishu: webhook URL is required")
	}
	if err := internal.ValidateHTTPURL(cfg.WebhookURL); err != nil {
		return nil, fmt.Errorf("feishu: invalid webhook URL: %w", err)
	}
	cfg.Freeze()
	return &Webhook{cfg: cfg}, nil
}

// Platform 返回平台标识。
func (w *Webhook) Platform() msgbot.Platform { return msgbot.PlatformFeishu }

// Stats 返回 Provider 的发送统计信息。
func (w *Webhook) Stats() *msgbot.Stats { return &w.stats }

// atEscaper 转义会破坏飞书 <at> 标签结构的字符。
var atEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// escapeAt 转义用户标识符，以便安全地拼接进 <at> 标签。
func escapeAt(s string) string { return atEscaper.Replace(s) }

// SendText 发送纯文本消息到飞书群。
// 被 @ 的用户标识符在放入 <at> 标签前会先经过转义。
func (w *Webhook) SendText(ctx context.Context, text string, opts ...msgbot.SendOption) error {
	if text == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "SendText", "text content is empty", nil)
	}
	o := msgbot.ApplySendOptions(opts)

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

// SendMarkdown 以 interactive 卡片形式发送 markdown 消息。
// 飞书 webhook 原生不支持 markdown msg_type，
// 因此将内容包装进带 markdown 渲染的 interactive 卡片中。
// @ 使用 interactive 卡片的 <at id=...></at> 语法，
// 与文本消息的 <at user_id="...">...</at> 语法不同。
func (w *Webhook) SendMarkdown(ctx context.Context, title, content string, opts ...msgbot.SendOption) error {
	if content == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "SendMarkdown", "markdown content is empty", nil)
	}
	o := msgbot.ApplySendOptions(opts)

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

// cardMentions 将发送选项渲染为 interactive 卡片的 <at> 元素。
func cardMentions(o *msgbot.SendOptions) string {
	var parts []string
	if o.AtAll {
		parts = append(parts, "<at id=all></at>")
	}
	for _, uid := range o.AtUserIDs {
		parts = append(parts, "<at id="+escapeAt(uid)+"></at>")
	}
	return strings.Join(parts, " ")
}

// SendRichText 发送富文本（post）消息到飞书群。
// 这是飞书原生的富文本格式，以结构化布局支持文本、链接、
// @ 以及图片。
func (w *Webhook) SendRichText(ctx context.Context, msg *msgbot.RichTextMessage) error {
	if msg == nil {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "SendRichText", "rich text message is nil", nil)
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

// SendImage 发送图片消息到飞书群。
// 必须设置 ImageKey 字段（通过飞书 open API 上传获得）。
func (w *Webhook) SendImage(ctx context.Context, img *msgbot.ImageMessage) error {
	if img == nil || img.ImageKey == "" {
		return msgbot.ValidationError(msgbot.PlatformFeishu, "SendImage", "image_key is required", nil)
	}

	return w.send(ctx, "SendImage", map[string]any{
		"msg_type": "image",
		"content":  map[string]any{"image_key": img.ImageKey},
	})
}

// send 通过共享发送路径分发 payload，在配置了 Secret 时应用飞书签名。
// 签名在每次尝试时重新生成，使重试携带全新的 timestamp；payload 会被克隆，
// 以避免重试累积陈旧的 timestamp/sign 字段。
func (w *Webhook) send(ctx context.Context, op string, payload map[string]any) error {
	return w.cfg.Send(ctx, &w.stats, msgbot.PlatformFeishu, op, func() (string, any, error) {
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

// BuildRichText 创建一条简单的富文本消息，包含标题、正文文本、
// 可选的超链接以及可选的 @all。
func BuildRichText(title, text string, link *msgbot.RichTextTag, atAll bool) *msgbot.RichTextMessage {
	var elems []msgbot.RichTextTag
	if text != "" {
		elems = append(elems, msgbot.RichTextTag{Tag: "text", Text: text})
	}
	if link != nil {
		elems = append(elems, *link)
	}
	if atAll {
		elems = append(elems, msgbot.RichTextTag{Tag: "at", UserID: "all"})
	}
	return &msgbot.RichTextMessage{
		Title:   title,
		Content: [][]msgbot.RichTextTag{elems},
	}
}

// BuildRichTextLines 从多行文本构造 RichTextMessage，
// 每行以一个 RichTextTag 元素切片的形式提供。
func BuildRichTextLines(title string, lines ...[]msgbot.RichTextTag) *msgbot.RichTextMessage {
	content := make([][]msgbot.RichTextTag, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 {
			content = append(content, line)
		}
	}
	return &msgbot.RichTextMessage{Title: title, Content: content}
}
