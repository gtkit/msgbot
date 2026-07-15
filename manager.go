package msgbot

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// Multi 是一个多平台消息分发器，可将消息并发广播到一个或多个 Provider 实现。
// 它可安全地被多个 goroutine 并发使用。
type Multi struct {
	providers []Provider
}

// NewMulti 创建一个分发器，将消息扇出到所有给定的 provider。
// 至少需要一个 provider。
func NewMulti(providers ...Provider) (*Multi, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("msgbot: at least one provider is required")
	}
	snapshot := make([]Provider, len(providers))
	for i, provider := range providers {
		if isNilProvider(provider) {
			return nil, fmt.Errorf("msgbot: provider at index %d is nil", i)
		}
		snapshot[i] = provider
	}
	return &Multi{providers: snapshot}, nil
}

// SendText 将文本消息并发广播到所有 provider。
func (m *Multi) SendText(ctx context.Context, text string, opts ...SendOption) error {
	return m.broadcast(func(p Provider) error {
		return p.SendText(ctx, text, opts...)
	})
}

// SendMarkdown 将 markdown 消息并发广播到所有 provider。
func (m *Multi) SendMarkdown(ctx context.Context, title, content string, opts ...SendOption) error {
	return m.broadcast(func(p Provider) error {
		return p.SendMarkdown(ctx, title, content, opts...)
	})
}

// SendRichText 将富文本消息并发广播到所有 provider。
func (m *Multi) SendRichText(ctx context.Context, msg *RichTextMessage) error {
	return m.broadcast(func(p Provider) error {
		return p.SendRichText(ctx, msg)
	})
}

// SendImage 将图片消息并发广播到所有 provider。
func (m *Multi) SendImage(ctx context.Context, img *ImageMessage) error {
	return m.broadcast(func(p Provider) error {
		return p.SendImage(ctx, img)
	})
}

// broadcast 在所有 provider 上并发执行 fn 并收集错误。
func (m *Multi) broadcast(fn func(Provider) error) error {
	// 快速路径：单个 provider 时避免 goroutine 开销。
	if len(m.providers) == 1 {
		return fn(m.providers[0])
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, p := range m.providers {
		wg.Go(func() {
			if err := fn(p); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	return errors.Join(errs...)
}

// Manager 管理多个具名 provider，并提供便捷的访问方式，
// 便于在 Gin 或任意 HTTP 框架中使用。
// provider 映射在构造后不可变；默认平台可通过 SetDefault 原子地修改，
// 因此并发读写默认平台是安全的，无需额外加锁。
type Manager struct {
	providers map[Platform]Provider
	defaults  atomic.Value // 存储 Platform。
}

// NewManager 从给定的 provider 创建一个新的 Manager。
// 第一个非 nil 的 provider 成为默认平台。
//
// nil 及带类型的 nil provider 会被跳过。当两个 provider 报告相同平台时，
// 后者覆盖前者。
func NewManager(providers ...Provider) *Manager {
	m := &Manager{
		providers: make(map[Platform]Provider, len(providers)),
	}
	for _, provider := range providers {
		if isNilProvider(provider) {
			continue
		}
		platform := provider.Platform()
		m.providers[platform] = provider
		if m.defaults.Load() == nil {
			m.defaults.Store(platform)
		}
	}
	return m
}

// Get 返回给定平台的 provider，未注册时返回 nil。
func (m *Manager) Get(platform Platform) Provider {
	return m.providers[platform]
}

// Default 返回默认 provider。
func (m *Manager) Default() Provider {
	p, _ := m.defaults.Load().(Platform)
	return m.providers[p]
}

// SetDefault 以 atomic 方式变更默认平台。若该平台没有已注册的 provider，
// 则返回错误并保持当前默认值不变，从而保证 Default() 永远不会解析到
// nil provider。
func (m *Manager) SetDefault(platform Platform) error {
	if _, ok := m.providers[platform]; !ok {
		return fmt.Errorf("msgbot: platform %q is not registered", platform)
	}
	m.defaults.Store(platform)
	return nil
}

// Feishu 返回飞书 provider，未注册时返回 nil。
func (m *Manager) Feishu() Provider { return m.providers[PlatformFeishu] }

// WeCom 返回企业微信 provider，未注册时返回 nil。
func (m *Manager) WeCom() Provider { return m.providers[PlatformWeCom] }

// DingTalk 返回钉钉 provider，未注册时返回 nil。
func (m *Manager) DingTalk() Provider { return m.providers[PlatformDingTalk] }

// All 以切片形式返回所有已注册的 provider。
func (m *Manager) All() []Provider {
	result := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		result = append(result, p)
	}
	return result
}

// Multi 从所有已注册的 provider 创建一个 Multi 分发器。
func (m *Manager) Multi() (*Multi, error) {
	return NewMulti(m.All()...)
}

func isNilProvider(provider Provider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
