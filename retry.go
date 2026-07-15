package msgbot

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// RetryPolicy 控制发送失败后如何重试。零值禁用重试（MaxRetries: 0），
// 保持单次尝试的发送语义。
//
// 启用重试会使投递变为 at-least-once：请求可能在客户端观察到失败之前就已在
// 平台端成功，因此一次重试可能产生重复消息。仅在可接受重复、或下游会去重时
// 才启用。
type RetryPolicy struct {
	// MaxRetries 是首次尝试之后的额外重试次数。
	// 0 禁用重试。负值按 0 处理。
	MaxRetries int
	// InitialDelay 是首次重试前的基础退避时长。
	// 为零且启用重试时默认为 200ms。
	InitialDelay time.Duration
	// MaxDelay 限制单次退避间隔的上限。为零时默认为 3s。
	MaxDelay time.Duration
	// Jitter 为每次退避加入随机 jitter，以避免 thundering herd。
	Jitter bool
}

// do 按策略带重试地执行 fn。只有被分类为可重试的错误才会重试；校验错误、
// 401/403、不可重试的平台码、解码错误，以及已取消/已超时的 context 会立即返回。
// 总等待时长受 ctx 约束；当 ctx 在退避期间结束时，返回 errors.Join(最后一次错误,
// ctx.Err())——既能被 errors.Is(context.Canceled/DeadlineExceeded) 命中，也保留
// 最后一次尝试的原因。
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
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

// backoff 计算下一次尝试前的等待时长。服务端建议的 Retry-After（携带在结构化
// 错误上）优先生效，并被完整尊重（不受 maxDelay 截断，总时长由 do 中的 ctx
// 兜底）；否则退避时长从 initial 起按指数增长，被限制在 maxDelay 内，并可选地
// 加入 equal jitter（一半固定、一半随机）。
func backoff(initial, maxDelay time.Duration, attempt int, err error, jitter bool) time.Duration {
	if e, ok := errors.AsType[*Error](err); ok && e.RetryAfter > 0 {
		return e.RetryAfter
	}

	d := initial << attempt // 指数增长；attempt 从 0 开始。
	if d <= 0 || d > maxDelay {
		d = maxDelay
	}
	if jitter && d > 1 {
		half := d / 2
		d = half + time.Duration(rand.Int64N(int64(half)))
	}
	return d
}
