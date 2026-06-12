package tokencache

import (
	"context"
	"sync/atomic"
	"time"
)

// entry 缓存条目：值与过期截止时间.
type entry[T any] struct {
	val      T
	deadline time.Time
}

// Memory 进程内默认缓存：基于 atomic.Pointer，读写路径零锁、并发安全。
// Set/Get 双向按值浅拷贝，调用方修改返回值不影响缓存内容；
// 若 T 含 map/slice/pointer 字段，引用指向的内容不做深拷贝
// （当前三个平台的 AccessToken 均为纯值字段，不受影响）。
// 零值即可使用。
type Memory[T any] struct {
	p atomic.Pointer[entry[T]]
}

// Get 返回未过期的缓存值副本；无缓存或已过期返回 (nil, nil).
func (m *Memory[T]) Get(_ context.Context) (*T, error) {
	e := m.p.Load()
	if e == nil || !time.Now().Before(e.deadline) {
		return nil, nil
	}
	cp := e.val
	return &cp, nil
}

// Set 写入值副本并记录过期截止时间；t 为 nil 或 ttl 非正时不写入.
func (m *Memory[T]) Set(_ context.Context, t *T, ttl time.Duration) error {
	if t == nil || ttl <= 0 {
		return nil
	}
	m.p.Store(&entry[T]{val: *t, deadline: time.Now().Add(ttl)})
	return nil
}
