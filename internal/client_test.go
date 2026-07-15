package internal

import (
	"net/http"
	"testing"
	"time"
)

func TestDefaultClients(t *testing.T) {
	if DefaultClient().Timeout != DefaultTimeout {
		t.Fatalf("default client timeout = %s, want %s", DefaultClient().Timeout, DefaultTimeout)
	}
	if DefaultUploadClient().Timeout != DefaultUploadTimeout {
		t.Fatalf("upload client timeout = %s, want %s", DefaultUploadClient().Timeout, DefaultUploadTimeout)
	}
}

func TestPickClient(t *testing.T) {
	fallback := DefaultClient()
	custom := &http.Client{Timeout: time.Second}

	if got := PickClient(fallback, nil); got != fallback {
		t.Fatal("nil variadic should return fallback")
	}
	if got := PickClient(fallback, []*http.Client{nil}); got != fallback {
		t.Fatal("nil client should return fallback")
	}
	if got := PickClient(fallback, []*http.Client{custom}); got != custom {
		t.Fatal("custom client should win over fallback")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{name: "empty", in: "", want: 0},
		{name: "seconds", in: "5", want: 5 * time.Second},
		{name: "negative", in: "-3", want: 0},
		{name: "garbage", in: "soon", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.in); got != tt.want {
				t.Fatalf("parseRetryAfter(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}

	// HTTP-date 形式：未来时间产生正的 duration。
	future := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 {
		t.Fatalf("future HTTP-date should yield positive duration, got %s", got)
	}
	// 过去的日期不产生等待。
	past := time.Now().Add(-2 * time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Fatalf("past HTTP-date should yield 0, got %s", got)
	}
}

func TestHTTPErrorMessage(t *testing.T) {
	e := &HTTPError{StatusCode: 429, Body: "slow down"}
	if e.Error() != "unexpected status 429: slow down" {
		t.Fatalf("unexpected message: %q", e.Error())
	}
}
