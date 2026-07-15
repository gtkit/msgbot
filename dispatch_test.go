package msgbot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scriptedStep is one programmed transport result.
type scriptedStep struct {
	err    error
	status int
	body   string
}

// scriptedTransport returns a programmed sequence of results, repeating the
// last step once exhausted. Not safe for concurrent use; tests are sequential.
type scriptedTransport struct {
	calls int
	steps []scriptedStep
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	i := s.calls
	s.calls++
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	st := s.steps[i]
	if st.err != nil {
		return nil, st.err
	}
	return &http.Response{
		StatusCode: st.status,
		Body:       io.NopCloser(strings.NewReader(st.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func newSendConfig(tr *scriptedTransport, retry RetryPolicy) *Config {
	cfg := &Config{HTTPClient: &http.Client{Transport: tr}, Retry: retry}
	cfg.Freeze()
	return cfg
}

func okBuild() (string, any, error) {
	return "https://example.com/hook", map[string]any{"msgtype": "text"}, nil
}

const fastRetry = time.Millisecond

func TestSendRetriesTransportError(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{
		{err: errors.New("dial failed")},
		{status: 200, body: `{"errcode":0}`},
	}}
	cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 2, InitialDelay: fastRetry, MaxDelay: fastRetry})

	var stats Stats
	if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if tr.calls != 2 {
		t.Fatalf("want 2 attempts, got %d", tr.calls)
	}
	if stats.TotalSent() != 1 {
		t.Fatalf("want 1 sent, got %d", stats.TotalSent())
	}
}

func TestSendRetriesHTTP503(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{
		{status: 503, body: "unavailable"},
		{status: 200, body: `{"errcode":0}`},
	}}
	cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 2, InitialDelay: fastRetry, MaxDelay: fastRetry})

	var stats Stats
	if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if tr.calls != 2 {
		t.Fatalf("want 2 attempts, got %d", tr.calls)
	}
}

func TestSendRetriesPlatformRateLimitCode(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{
		{status: 200, body: `{"errcode":45009,"errmsg":"freq"}`},
		{status: 200, body: `{"errcode":0}`},
	}}
	cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 2, InitialDelay: fastRetry, MaxDelay: fastRetry})

	var stats Stats
	if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("expected success after rate-limit retry, got %v", err)
	}
	if tr.calls != 2 {
		t.Fatalf("want 2 attempts, got %d", tr.calls)
	}
}

func TestSendDefaultNoRetry(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{{status: 503, body: "down"}}}
	cfg := newSendConfig(tr, RetryPolicy{})

	var stats Stats
	err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild)
	if err == nil {
		t.Fatal("want error")
	}
	if tr.calls != 1 {
		t.Fatalf("default policy must not retry, got %d attempts", tr.calls)
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindHTTP || e.HTTPStatus != 503 {
		t.Fatalf("want KindHTTP 503, got %+v", err)
	}
	if stats.TotalError() != 1 {
		t.Fatalf("want 1 error recorded, got %d", stats.TotalError())
	}
}

func TestSendValidationDoesNotSend(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 3, InitialDelay: fastRetry, MaxDelay: fastRetry})

	build := func() (string, any, error) {
		return "", nil, ValidationError(PlatformWeCom, "SendText", "bad input", nil)
	}
	err := cfg.Send(context.Background(), new(Stats), PlatformWeCom, "SendText", build)
	if tr.calls != 0 {
		t.Fatalf("validation failure must not send, got %d attempts", tr.calls)
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindValidation {
		t.Fatalf("want KindValidation, got %+v", err)
	}
}

func TestSendClassifiesContextCancel(t *testing.T) {
	tr := &scriptedTransport{steps: []scriptedStep{{err: context.Canceled}}}
	cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 3, InitialDelay: fastRetry, MaxDelay: fastRetry})

	err := cfg.Send(context.Background(), new(Stats), PlatformWeCom, "SendText", okBuild)
	if tr.calls != 1 {
		t.Fatalf("context cancellation must not retry, got %d attempts", tr.calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled preserved, got %v", err)
	}
}
