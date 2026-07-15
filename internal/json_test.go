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
	// < > & 必须保持字面（JSON 内的 " 正常转义为 \" 与 HTML 转义无关，不参与判断）。
	if !strings.Contains(s, `<at user_id=`) || !strings.Contains(s, `>&x`) {
		t.Fatalf("< / > / & should stay literal: %s", s)
	}
	// 上面的字面子串只有在未被 HTML 转义时才会出现，已足以证明没有转义。
	if strings.HasSuffix(s, "\n") {
		t.Fatalf("trailing newline must be trimmed: %q", s)
	}
}

func TestMarshalNoTrailingNewline(t *testing.T) {
	got, err := Marshal(map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.HasSuffix(string(got), "\n") {
		t.Fatalf("output must not end with newline: %q", string(got))
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	type payload struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	in := payload{A: 1, B: "x"}
	b, err := Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out payload
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestUnmarshalPlatformResponse(t *testing.T) {
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := Unmarshal([]byte(`{"code":0,"msg":"ok"}`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Code != 0 || r.Msg != "ok" {
		t.Fatalf("decoded wrong: %+v", r)
	}
}
