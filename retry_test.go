package msgbot

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryDisabledByDefault(t *testing.T) {
	var calls int
	err := RetryPolicy{}.do(context.Background(), func(context.Context) error {
		calls++
		return &Error{Kind: KindHTTP, HTTPStatus: 503, Retryable: true}
	})
	if calls != 1 {
		t.Fatalf("default policy must not retry, got %d calls", calls)
	}
	if err == nil {
		t.Fatal("want error returned")
	}
}

func TestRetryRetriesRetryable(t *testing.T) {
	var calls int
	p := RetryPolicy{MaxRetries: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	err := p.do(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return &Error{Kind: KindHTTP, HTTPStatus: 503, Retryable: true}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 attempts, got %d", calls)
	}
}

func TestRetryStopsAtMax(t *testing.T) {
	var calls int
	p := RetryPolicy{MaxRetries: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	err := p.do(context.Background(), func(context.Context) error {
		calls++
		return &Error{Kind: KindHTTP, HTTPStatus: 503, Retryable: true}
	})
	if calls != 3 { // first attempt + 2 retries.
		t.Fatalf("want 3 attempts, got %d", calls)
	}
	if err == nil {
		t.Fatal("want final error")
	}
}

func TestRetrySkipsNonRetryable(t *testing.T) {
	var calls int
	p := RetryPolicy{MaxRetries: 5, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	err := p.do(context.Background(), func(context.Context) error {
		calls++
		return ValidationError(PlatformFeishu, "SendText", "bad", nil)
	})
	if calls != 1 {
		t.Fatalf("validation error must not retry, got %d calls", calls)
	}
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	p := RetryPolicy{MaxRetries: 5, InitialDelay: time.Hour, MaxDelay: time.Hour}
	err := p.do(ctx, func(context.Context) error {
		calls++
		return &Error{Kind: KindHTTP, HTTPStatus: 503, Retryable: true}
	})
	if calls != 1 {
		t.Fatalf("cancelled context must stop before a long backoff, got %d calls", calls)
	}
	if err == nil {
		t.Fatal("want error")
	}
}

func TestBackoffHonorsRetryAfter(t *testing.T) {
	err := &Error{Retryable: true, RetryAfter: 5 * time.Second}
	got := backoff(time.Millisecond, 10*time.Second, 0, err, false)
	if got != 5*time.Second {
		t.Fatalf("want RetryAfter honored, got %s", got)
	}
	// Capped by MaxDelay.
	capped := backoff(time.Millisecond, 2*time.Second, 0, err, false)
	if capped != 2*time.Second {
		t.Fatalf("want RetryAfter capped at MaxDelay, got %s", capped)
	}
}

func TestBackoffExponential(t *testing.T) {
	initial := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	plain := errors.New("x")
	if d := backoff(initial, maxDelay, 0, plain, false); d != initial {
		t.Fatalf("attempt 0 want %s, got %s", initial, d)
	}
	if d := backoff(initial, maxDelay, 2, plain, false); d != 4*initial {
		t.Fatalf("attempt 2 want %s, got %s", 4*initial, d)
	}
	if d := backoff(initial, maxDelay, 20, plain, false); d != maxDelay {
		t.Fatalf("large attempt should cap at %s, got %s", maxDelay, d)
	}
}
