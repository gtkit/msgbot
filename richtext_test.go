package msgbot

import (
	"strings"
	"testing"
)

func TestRichTextToMarkdownDropsImage(t *testing.T) {
	got := RichTextToMarkdown(&RichTextMessage{
		Title: "标题",
		Content: [][]RichTextTag{{
			{Tag: "text", Text: "见图 "},
			{Tag: "img", ImgKey: "img_secret_key"},
		}},
	})
	if strings.Contains(got, "img_secret_key") {
		t.Fatalf("image key must be dropped in degraded markdown: %q", got)
	}
	if !strings.Contains(got, "见图") {
		t.Fatalf("text content should survive: %q", got)
	}
}

func TestRichTextToMarkdownNil(t *testing.T) {
	if got := RichTextToMarkdown(nil); got != "" {
		t.Fatalf("nil message should yield empty string, got %q", got)
	}
}
