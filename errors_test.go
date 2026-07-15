package msgbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gtkit/msgbot/internal"
)

func TestErrorFormatAndUnwrap(t *testing.T) {
	cause := errors.New("dial failed")
	e := &Error{
		Platform:   PlatformFeishu,
		Operation:  "SendText",
		Kind:       KindTransport,
		HTTPStatus: 0,
		Message:    "boom",
		Err:        cause,
	}
	got := e.Error()
	for _, want := range []string{"msgbot", "feishu", "SendText", "transport", "boom", "dial failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error string %q missing %q", got, want)
		}
	}
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap must expose the cause for errors.Is")
	}
}

func TestErrorPreservesContextCause(t *testing.T) {
	e := classifySendError(PlatformWeCom, "SendText", fmt.Errorf("send: %w", context.Canceled))
	if e.Kind != KindTransport {
		t.Fatalf("want transport kind, got %s", e.Kind)
	}
	if e.Retryable {
		t.Fatal("context cancellation must not be retryable")
	}
	if !errors.Is(e, context.Canceled) {
		t.Fatal("errors.Is(context.Canceled) must hold through the structured error")
	}
}

func TestClassifySendErrorHTTP(t *testing.T) {
	he := &internal.HTTPError{StatusCode: 503, Body: "unavailable", RetryAfter: 2 * time.Second}
	e := classifySendError(PlatformDingTalk, "SendText", fmt.Errorf("post: %w", he))
	if e.Kind != KindHTTP || e.HTTPStatus != 503 || !e.Retryable {
		t.Fatalf("unexpected classification: %+v", e)
	}
	if e.RetryAfter != 2*time.Second {
		t.Fatalf("want RetryAfter propagated, got %s", e.RetryAfter)
	}

	he400 := &internal.HTTPError{StatusCode: 400, Body: "bad"}
	if e := classifySendError(PlatformDingTalk, "SendText", he400); e.Retryable {
		t.Fatal("400 must not be retryable")
	}
}

func TestClassifySendErrorTransport(t *testing.T) {
	e := classifySendError(PlatformWeCom, "SendText", errors.New("connection reset"))
	if e.Kind != KindTransport || !e.Retryable {
		t.Fatalf("plain transport error should be retryable transport, got %+v", e)
	}
}

func TestHTTPRetryable(t *testing.T) {
	retryable := []int{408, 425, 429, 500, 502, 503, 504}
	for _, s := range retryable {
		if !httpRetryable(s) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	for _, s := range []int{400, 401, 403, 404, 200} {
		if httpRetryable(s) {
			t.Errorf("status %d should not be retryable", s)
		}
	}
}

func TestPlatformRetryable(t *testing.T) {
	cases := []struct {
		platform Platform
		code     int
		want     bool
	}{
		{PlatformFeishu, 11232, true},
		{PlatformFeishu, 9499, false},
		{PlatformWeCom, 45009, true},
		{PlatformWeCom, 45033, true},
		{PlatformWeCom, 40001, false},
		{PlatformDingTalk, 130101, true},
		{PlatformDingTalk, 300001, false},
		{Platform("unknown"), 11232, false},
	}
	for _, c := range cases {
		if got := platformRetryable(c.platform, c.code); got != c.want {
			t.Errorf("platformRetryable(%s, %d) = %v, want %v", c.platform, c.code, got, c.want)
		}
	}
}

func TestValidationError(t *testing.T) {
	e := ValidationError(PlatformFeishu, "SendText", "text content is empty", nil)
	if e.Kind != KindValidation || e.Retryable {
		t.Fatalf("validation error must be non-retryable KindValidation, got %+v", e)
	}
	var target *Error
	if !errors.As(error(e), &target) {
		t.Fatal("errors.As must recover *Error")
	}
}
