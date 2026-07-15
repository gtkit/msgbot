package msgbot

import "strings"

// RichTextToMarkdown 将 RichTextMessage 转换为 markdown 字符串。
// 供不原生支持富文本（post）格式的平台使用。
//
// 降级约定："at" 元素渲染为纯 "@" 文本，不会产生真正的平台通知；
// "img" 元素会被丢弃，因为其 key 是飞书专属的，在其他平台无法解析。
func RichTextToMarkdown(msg *RichTextMessage) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	if msg.Title != "" {
		b.WriteString("### ")
		b.WriteString(msg.Title)
		b.WriteString("\n\n")
	}
	for _, line := range msg.Content {
		for _, tag := range line {
			switch tag.Tag {
			case "text":
				b.WriteString(tag.Text)
			case "a":
				b.WriteString("[")
				b.WriteString(tag.Text)
				b.WriteString("](")
				b.WriteString(tag.Href)
				b.WriteString(")")
			case "at":
				if tag.UserID == "all" {
					b.WriteString("@所有人")
				} else {
					b.WriteString("@")
					b.WriteString(tag.UserID)
				}
			}
		}
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
