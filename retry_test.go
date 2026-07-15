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
	if calls != 3 { // 首次尝试 + 2 次重试。
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
	lastErr := &Error{Kind: KindHTTP, HTTPStatus: 503, Retryable: true}
	p := RetryPolicy{MaxRetries: 5, InitialDelay: time.Hour, MaxDelay: time.Hour}
	err := p.do(ctx, func(context.Context) error {
		calls++
		return lastErr
	})
	if calls != 1 {
		t.Fatalf("cancelled context must stop before a long backoff, got %d calls", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want errors.Is(context.Canceled), got %v", err)
	}
	if !errors.Is(err, lastErr) {
		t.Fatal("last attempt error must be preserved in the chain")
	}
}

func TestBackoffHonorsRetryAfter(t *testing.T) {
	const bigCap = time.Hour
	err := &Error{Retryable: true, RetryAfter: 5 * time.Second}
	got := backoff(time.Millisecond, 10*time.Second, bigCap, 0, err, false)
	if got != 5*time.Second {
		t.Fatalf("want RetryAfter honored, got %s", got)
	}
	// Retry-After 完整生效，不被 MaxDelay 截断（总时长由 ctx 兜底）。
	full := backoff(time.Millisecond, 2*time.Second, bigCap, 0, err, false)
	if full != 5*time.Second {
		t.Fatalf("want RetryAfter honored in full (not capped by MaxDelay), got %s", full)
	}
}

func TestRetryAfterBoundedWithoutDeadline(t *testing.T) {
	// 服务端返回一个极大的 Retry-After，调用方用无 deadline 的 context。
	// MaxRetryAfter 安全上限必须防止近乎永久的阻塞——这里设 20ms 使测试可控。
	var calls int
	p := RetryPolicy{MaxRetries: 1, MaxRetryAfter: 20 * time.Millisecond}
	start := time.Now()
	_ = p.do(context.Background(), func(context.Context) error {
		calls++
		return &Error{Kind: KindPlatform, Code: 1, Retryable: true, RetryAfter: time.Hour}
	})
	if calls != 2 {
		t.Fatalf("want first attempt + 1 retry = 2 calls, got %d", calls)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("huge Retry-After must be capped by MaxRetryAfter, waited %s", elapsed)
	}
}

func TestBackoffRetryAfterCappedBySafetyLimit(t *testing.T) {
	err := &Error{Retryable: true, RetryAfter: time.Hour}
	// maxRetryAfter 为正上限时，超大 Retry-After 被截断。
	capped := backoff(time.Millisecond, time.Second, 30*time.Second, 0, err, false)
	if capped != 30*time.Second {
		t.Fatalf("want RetryAfter capped at 30s safety limit, got %s", capped)
	}
	// maxRetryAfter < 0 表示不设上限，完整尊重 Retry-After。
	uncapped := backoff(time.Millisecond, time.Second, -1, 0, err, false)
	if uncapped != time.Hour {
		t.Fatalf("want RetryAfter uncapped when limit is negative, got %s", uncapped)
	}
}

func TestBackoffExponential(t *testing.T) {
	initial := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	plain := errors.New("x")
	if d := backoff(initial, maxDelay, 30*time.Second, 0, plain, false); d != initial {
		t.Fatalf("attempt 0 want %s, got %s", initial, d)
	}
	if d := backoff(initial, maxDelay, 30*time.Second, 2, plain, false); d != 4*initial {
		t.Fatalf("attempt 2 want %s, got %s", 4*initial, d)
	}
	if d := backoff(initial, maxDelay, 30*time.Second, 20, plain, false); d != maxDelay {
		t.Fatalf("large attempt should cap at %s, got %s", maxDelay, d)
	}
}
