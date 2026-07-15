package msgbot

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// RetryPolicy controls how a failed send is retried. The zero value disables
// retry (MaxRetries: 0), preserving single-attempt send semantics.
//
// Enabling retry makes delivery at-least-once: a request may have already
// succeeded on the platform before the client observed a failure, so a retry
// can produce a duplicate message. Enable it only when duplicates are
// acceptable or deduplicated downstream.
type RetryPolicy struct {
	// MaxRetries is the number of additional attempts after the first.
	// 0 disables retry. Negative values are treated as 0.
	MaxRetries int
	// InitialDelay is the base backoff before the first retry.
	// Defaults to 200ms when zero and retry is enabled.
	InitialDelay time.Duration
	// MaxDelay caps a single backoff interval. Defaults to 3s when zero.
	MaxDelay time.Duration
	// Jitter adds randomized jitter to each backoff to avoid thundering herds.
	Jitter bool
}

// do runs fn with retries according to the policy. Only errors classified as
// retryable are retried; validation, 401/403, non-retryable platform codes,
// decode errors, and canceled/expired contexts are returned immediately. The
// total wait is bounded by ctx; when ctx ends during a backoff, the last
// attempt's error is returned.
func (p RetryPolicy) do(ctx context.Context, fn func(context.Context) error) error {
	maxRetries := max(p.MaxRetries, 0)
	initial := p.InitialDelay
	if initial <= 0 {
		initial = 200 * time.Millisecond
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 3 * time.Second
	}

	var err error
	for attempt := 0; ; attempt++ {
		err = fn(ctx)
		if err == nil {
			return nil
		}
		if attempt >= maxRetries || !isRetryable(err) {
			return err
		}
		delay := backoff(initial, maxDelay, attempt, err, p.Jitter)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

// backoff computes the wait before the next attempt. A server-advised
// Retry-After (carried on the structured error) takes precedence; otherwise the
// delay grows exponentially from initial, is capped at maxDelay, and optionally
// gets equal jitter (half fixed, half random).
func backoff(initial, maxDelay time.Duration, attempt int, err error, jitter bool) time.Duration {
	if e, ok := errors.AsType[*Error](err); ok && e.RetryAfter > 0 {
		if e.RetryAfter > maxDelay {
			return maxDelay
		}
		return e.RetryAfter
	}

	d := initial << attempt // exponential; attempt starts at 0.
	if d <= 0 || d > maxDelay {
		d = maxDelay
	}
	if jitter && d > 1 {
		half := d / 2
		d = half + time.Duration(rand.Int64N(int64(half)))
	}
	return d
}
