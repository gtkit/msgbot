package msgbot

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// Multi is a multi-platform message dispatcher that broadcasts messages
// to one or more Provider implementations concurrently.
// It is safe for concurrent use by multiple goroutines.
type Multi struct {
	providers []Provider
}

// NewMulti creates a dispatcher that fans out messages to all given providers.
// At least one provider is required.
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

// SendText broadcasts a text message to all providers concurrently.
func (m *Multi) SendText(ctx context.Context, text string, opts ...SendOption) error {
	return m.broadcast(func(p Provider) error {
		return p.SendText(ctx, text, opts...)
	})
}

// SendMarkdown broadcasts a markdown message to all providers concurrently.
func (m *Multi) SendMarkdown(ctx context.Context, title, content string, opts ...SendOption) error {
	return m.broadcast(func(p Provider) error {
		return p.SendMarkdown(ctx, title, content, opts...)
	})
}

// SendRichText broadcasts a rich text message to all providers concurrently.
func (m *Multi) SendRichText(ctx context.Context, msg *RichTextMessage) error {
	return m.broadcast(func(p Provider) error {
		return p.SendRichText(ctx, msg)
	})
}

// SendImage broadcasts an image message to all providers concurrently.
func (m *Multi) SendImage(ctx context.Context, img *ImageMessage) error {
	return m.broadcast(func(p Provider) error {
		return p.SendImage(ctx, img)
	})
}

// broadcast executes fn on all providers concurrently and collects errors.
func (m *Multi) broadcast(fn func(Provider) error) error {
	// Fast path: single provider avoids goroutine overhead.
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

// Manager manages multiple named providers and provides convenient
// access patterns for use in Gin or any HTTP framework.
// All fields are immutable after construction — no locks needed.
type Manager struct {
	providers map[Platform]Provider
	defaults  atomic.Value // stores Platform.
}

// NewManager creates a new Manager from the given providers.
// The first non-nil provider becomes the default platform.
//
// Nil and typed-nil providers are skipped. When two providers report the same
// platform, the later one overwrites the earlier one.
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

// Get returns the provider for the given platform, or nil if not registered.
func (m *Manager) Get(platform Platform) Provider {
	return m.providers[platform]
}

// Default returns the default provider.
func (m *Manager) Default() Provider {
	p, _ := m.defaults.Load().(Platform)
	return m.providers[p]
}

// SetDefault changes the default platform atomically. It returns an error if
// the platform has no registered provider, leaving the current default
// unchanged, so Default() can never resolve to a nil provider.
func (m *Manager) SetDefault(platform Platform) error {
	if _, ok := m.providers[platform]; !ok {
		return fmt.Errorf("msgbot: platform %q is not registered", platform)
	}
	m.defaults.Store(platform)
	return nil
}

// Feishu returns the Feishu provider, or nil if not registered.
func (m *Manager) Feishu() Provider { return m.providers[PlatformFeishu] }

// WeCom returns the WeCom provider, or nil if not registered.
func (m *Manager) WeCom() Provider { return m.providers[PlatformWeCom] }

// DingTalk returns the DingTalk provider, or nil if not registered.
func (m *Manager) DingTalk() Provider { return m.providers[PlatformDingTalk] }

// All returns all registered providers as a slice.
func (m *Manager) All() []Provider {
	result := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		result = append(result, p)
	}
	return result
}

// Multi creates a Multi dispatcher from all registered providers.
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
