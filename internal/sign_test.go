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
