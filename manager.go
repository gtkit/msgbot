package msgbot

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
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

// Manager 管理多个 provider，并提供便捷的访问方式，
// 便于在 Gin 或任意 HTTP 框架中使用。
// provider 映射在构造后不可变；默认平台可通过 SetDefault 原子地修改，
// 因此并发读写默认平台是安全的，无需额外加锁。
//
// 有两种注册方式：NewManager 按平台索引，一个平台一个 provider；
// NewNamedManager 额外按名字索引，因此同一平台可以有多个目标
// （例如 P0 群、值班群、归档群各一个飞书机器人）。
type Manager struct {
	providers map[Platform]Provider
	named     map[string]Provider // 仅 NewNamedManager 填充。
	names     []string            // 具名注册顺序。
	all       []Provider          // 注册顺序，供 All 与 Multi 使用。
	defaults  atomic.Value        // 存储 Platform。
}

// NamedProvider 是一个带名字的 provider 注册项，供 NewNamedManager 使用。
type NamedProvider struct {
	Name     string
	Provider Provider
}

// Named 构造一个 NamedProvider 注册项。
func Named(name string, provider Provider) NamedProvider {
	return NamedProvider{Name: name, Provider: provider}
}

// NewManager 从给定的 provider 创建一个新的 Manager。
// 第一个非 nil 的 provider 成为默认平台。
//
// nil 及带类型的 nil provider 会被跳过。当两个 provider 报告相同平台时，
// 后者覆盖前者——被覆盖者也不会出现在 All 中。需要同平台多个目标时，
// 改用 NewNamedManager。
func NewManager(providers ...Provider) *Manager {
	m := &Manager{
		providers: make(map[Platform]Provider, len(providers)),
	}

	// 先收集非 nil provider 及其平台，同时记录每个平台最后一次出现的下标。
	// Platform() 由调用方实现，每个 provider 只调用一次——分两遍各调一次，
	// 返回值不一致就会让 all 算错。
	type entry struct {
		provider Provider
		platform Platform
	}
	entries := make([]entry, 0, len(providers))
	survivor := make(map[Platform]int, len(providers))
	for _, provider := range providers {
		if isNilProvider(provider) {
			continue
		}
		e := entry{provider: provider, platform: provider.Platform()}
		survivor[e.platform] = len(entries)
		entries = append(entries, e)
	}

	m.all = make([]Provider, 0, len(survivor))
	for i, e := range entries {
		m.providers[e.platform] = e.provider
		if m.defaults.Load() == nil {
			m.defaults.Store(e.platform)
		}
		// 只保留平台索引里的存活者。用下标而非直接比较 provider 值：
		// Provider 是接口，调用方的实现若落在不可比较的动态类型上，
		// == 会在运行期 panic。
		if survivor[e.platform] == i {
			m.all = append(m.all, e.provider)
		}
	}
	return m
}

// NewNamedManager 从具名注册项创建 Manager，使同一平台的多个 provider
// 能以互不覆盖的名字共存。至少需要一个注册项；空名字、重名与 nil provider
// 均返回错误——名字是调用方给的标识，撞名一定是配置错误，静默覆盖会让
// 某个目标永远收不到消息。
//
// 具名 provider 同时进入平台索引，因此 Get / Feishu / WeCom / DingTalk /
// Default 照常可用；同平台有多个时，平台索引沿用「后者覆盖前者」的规则。
//
// 注意 Default 的解析方式没变：默认「平台」是第一个注册项的平台，返回的
// provider 是该平台在平台索引中的占位者。所以同平台多目标时 Default 返回的是
// 该平台最后注册的那个，而不是第一个注册项本身——具名注册下应改用 GetNamed
// 精确取用。
//
// 注册项用切片而非 map 传入，是为了让 Names / All / Multi / Default 的
// 结果在每次运行时都确定——map 迭代顺序是随机的。
func NewNamedManager(providers ...NamedProvider) (*Manager, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("msgbot: at least one named provider is required")
	}
	m := &Manager{
		providers: make(map[Platform]Provider, len(providers)),
		named:     make(map[string]Provider, len(providers)),
		names:     make([]string, 0, len(providers)),
		all:       make([]Provider, 0, len(providers)),
	}
	for i, np := range providers {
		if np.Name == "" {
			return nil, fmt.Errorf("msgbot: named provider at index %d has an empty name", i)
		}
		if isNilProvider(np.Provider) {
			return nil, fmt.Errorf("msgbot: named provider %q is nil", np.Name)
		}
		if _, dup := m.named[np.Name]; dup {
			return nil, fmt.Errorf("msgbot: duplicate provider name %q", np.Name)
		}

		platform := np.Provider.Platform()
		m.named[np.Name] = np.Provider
		m.names = append(m.names, np.Name)
		m.all = append(m.all, np.Provider)
		m.providers[platform] = np.Provider
		if m.defaults.Load() == nil {
			m.defaults.Store(platform)
		}
	}
	return m, nil
}

// GetNamed 返回该名字注册的 provider，未注册时返回 nil。
func (m *Manager) GetNamed(name string) Provider {
	return m.named[name]
}

// Names 按注册顺序返回全部具名 provider 的名字。
// 返回的是副本，调用方修改它不会影响 Manager。
// 由 NewManager 创建的 Manager 没有具名注册，返回 nil。
func (m *Manager) Names() []string {
	return slices.Clone(m.names)
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

// All 按注册顺序返回所有已注册的 provider，返回的是副本。
// 具名注册下同平台的多个 provider 都会出现；NewManager 下被同平台后者
// 覆盖掉的 provider 不会出现。
func (m *Manager) All() []Provider {
	return slices.Clone(m.all)
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
