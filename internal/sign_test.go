package internal

import (
	"net/url"
	"strings"
	"testing"
)

func TestFeishuSign(t *testing.T) {
	got, err := FeishuSign("secret", 1700000000)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got == "" {
		t.Fatal("sign is empty")
	}
}

func TestDingTalkSignedURL(t *testing.T) {
	got, err := DingTalkSignedURL("https://oapi.dingtalk.com/robot/send?access_token=abc", "secret")
	if err != nil {
		t.Fatalf("sign url: %v", err)
	}
	if !strings.Contains(got, "timestamp=") || !strings.Contains(got, "sign=") {
		t.Fatalf("missing signing params: %s", got)
	}
	if _, err := url.ParseRequestURI(got); err != nil {
		t.Fatalf("invalid signed url: %v", err)
	}
}

func TestDingTalkSignedURLQueryHandling(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		wantAccess bool // whether an existing access_token must survive
	}{
		{name: "with existing query", base: "https://oapi.dingtalk.com/robot/send?access_token=abc", wantAccess: true},
		{name: "without query", base: "https://oapi.dingtalk.com/robot/send", wantAccess: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DingTalkSignedURL(tt.base, "secret")
			if err != nil {
				t.Fatalf("sign url: %v", err)
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("parse signed url: %v", err)
			}
			q := parsed.Query()
			if q.Get("timestamp") == "" || q.Get("sign") == "" {
				t.Fatalf("timestamp/sign not in query: %s", got)
			}
			if tt.wantAccess && q.Get("access_token") != "abc" {
				t.Fatalf("existing access_token lost: %s", got)
			}
			// The signing params must be in the query string, never in the path.
			if strings.Contains(parsed.Path, "timestamp") || strings.Contains(parsed.Path, "sign") {
				t.Fatalf("signing params leaked into path: %s", parsed.Path)
			}
		})
	}
}
