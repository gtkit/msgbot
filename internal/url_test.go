package internal

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestURLOriginForLog(t *testing.T) {
	const raw = "https://example.com/hooks/secret-token?access_token=secret&sign=value"
	if got := URLOriginForLog(raw); got != "https://example.com" {
		t.Fatalf("unexpected origin %q", got)
	}
	if got := URLOriginForLog("not a url"); got != "[redacted]" {
		t.Fatalf("unexpected invalid URL result %q", got)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	if err := ValidateHTTPURL("https://example.com/hook/secret?token=value"); err != nil {
		t.Fatalf("valid URL: %v", err)
	}
	for _, rawURL := range []string{"/relative/path", "ftp://example.com/hook", "https:///missing-host"} {
		if err := ValidateHTTPURL(rawURL); err == nil {
			t.Fatalf("expected invalid URL error for %q", rawURL)
		}
	}
}

func TestSanitizeRequestError(t *testing.T) {
	cause := errors.New("dial failed")
	err := &url.Error{Op: "Post", URL: "https://example.com/hook/secret?token=value", Err: cause}
	got := SanitizeRequestError(err)
	if !errors.Is(got, cause) {
		t.Fatalf("underlying cause was not preserved: %v", got)
	}
	if strings.Contains(got.Error(), "secret") || strings.Contains(got.Error(), "token=value") {
		t.Fatalf("sanitized error leaked URL: %v", got)
	}
}
