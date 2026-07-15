package internal

import (
	"strings"
	"testing"
)

func TestMarshalDoesNotEscapeHTML(t *testing.T) {
	got, err := Marshal(map[string]any{"text": `<at user_id="all">&x`})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, `<at user_id=`) {
		t.Fatalf("< should stay literal: %s", s)
	}
	// 不应出现 HTML 转义后的形式。
	if strings.Contains(s, `<`) || strings.Contains(s, `&`) || strings.Contains(s, `>`) {
		t.Fatalf("must not HTML-escape: %s", s)
	}
	if strings.HasSuffix(s, "\n") {
		t.Fatalf("trailing newline must be trimmed: %q", s)
	}
}
