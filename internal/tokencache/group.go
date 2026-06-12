package tokencache

import (
	"context"
	"sync"
)

// call 表示一次进行中的上游请求，done 关闭后 val/err 不再变化.
type call[T any] struct {
	done chan struct{}
	val  *T
	err  error
}

// Group 进程内 in-flight 去重，ctx 语义：
//
//   - 上游请求由发起者（第一个进入的调用方）的 ctx 控制；
//   - 等待者监听自己的 ctx，可提前退出并获得 ctx.Err()，
//     不影响发起者与其他等待者；
//   - 发起者失败时，当前这一批等待者共享该失败，但 in-flight
//     记录随即清除——失败不粘滞，下一个调用发起全新请求。
//
// Group 只保存进行中的请求，空闲时不持有任何状态。零值不可用，
// 必须通过 NewGroup 创建。
type Group[T any] struct {
	mu sync.Mutex
	m  map[string]*call[T]
}

// NewGroup 创建一个去重组。每个平台包持有自己的实例，按包隔离。
func NewGroup[T any]() *Group[T] {
	return &Group[T]{m: make(map[string]*call[T])}
}

// Do 执行或等待 key 对应的请求：无在途请求时调用 fn 并广播结果，
// 有在途请求时等待共享其结果。
func (g *Group[T]) Do(ctx context.Context, key string, fn func() (*T, error)) (*T, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		select {
		case <-c.done:
			return c.val, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &call[T]{done: make(chan struct{})}
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()

	// 先清除记录再放行等待者：等待者被唤醒时 key 已不在途，
	// 观察到失败后立即重试的调用一定会发起全新请求。
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	close(c.done)

	return c.val, c.err
}
