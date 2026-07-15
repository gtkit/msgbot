package msgbot

import "strings"

// RichTextToMarkdown converts a RichTextMessage to a markdown string.
// This is used by platforms that do not natively support rich text (post) format.
//
// Degradation contract: "at" elements render as plain "@" text and do NOT
// produce a real platform notification, and "img" elements are dropped because
// their key is Feishu-specific and cannot be resolved on other platforms.
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
