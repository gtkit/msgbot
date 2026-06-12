package dingtalk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gtkit/msgbot/internal/tokencache"
)

// TokenCache access_token 缓存后端，由调用方实现存取逻辑（内存、Redis 均可）。
// 实现必须并发安全。
//
// 重要：一个 TokenCache 实例只能服务一组凭证（appKey/appSecret）。
// 多组凭证必须各自创建独立实例，否则 token 会互相覆盖。
// Redis 实现应在构造时绑定唯一 key（如 "dingtalk:token:" + appKey），
// 用 json 序列化 *AccessToken、过期淘汰交给 Redis TTL，
// 完整示例见 feishu.TokenCache 文档。
type TokenCache interface {
	// Get 返回缓存的 token；未命中或已过期返回 (nil, nil).
	Get(ctx context.Context) (*AccessToken, error)
	// Set 写入 token，ttl 由库按有效期算好传入.
	Set(ctx context.Context, t *AccessToken, ttl time.Duration) error
}

// MemoryTokenCache 进程内默认 TokenCache 实现：
// 基于 atomic.Pointer，读写零锁，并发安全。
type MemoryTokenCache struct {
	mem tokencache.Memory[AccessToken]
}

// NewMemoryTokenCache 创建进程内 token 缓存。
// 一个实例只能服务一组凭证，多个应用各自创建。
func NewMemoryTokenCache() *MemoryTokenCache {
	return &MemoryTokenCache{}
}

// Get 实现 TokenCache.
func (m *MemoryTokenCache) Get(ctx context.Context) (*AccessToken, error) {
	return m.mem.Get(ctx)
}

// Set 实现 TokenCache.
func (m *MemoryTokenCache) Set(ctx context.Context, t *AccessToken, ttl time.Duration) error {
	return m.mem.Set(ctx, t, ttl)
}

// tokenGroup 包级 in-flight 去重组：冷启动并发取 token 时同凭证只发一次请求.
var tokenGroup = tokencache.NewGroup[AccessToken]()

// GetAccessTokenCached 带缓存地获取 access_token：缓存命中直接返回；
// 未命中请求钉钉，按有效期算好 TTL（提前 300 秒）回写缓存后返回。
//
// 行为契约：
//   - 缓存后端故障自动降级为直连获取（Get 错误按未命中处理、Set 错误忽略），
//     不阻断调用；错误被静默吞掉，需要观测时应在自定义 TokenCache 实现内记录。
//   - 同凭证并发未命中时只向钉钉发一次请求（防冷启动击穿）。
//   - cache 为 nil 时不持久缓存（等价于 GetAccessToken），但仍保留并发去重。
func GetAccessTokenCached(ctx context.Context, appKey, appSecret string,
	cache TokenCache, client ...*http.Client) (*AccessToken, error) {
	// 与 GetAccessToken 一致的前置校验：空凭证不允许被缓存命中掩盖.
	if appKey == "" || appSecret == "" {
		return nil, fmt.Errorf("dingtalk: appkey and appsecret are required")
	}
	return tokencache.Fetch(ctx, cache, tokenGroup,
		tokencache.Key("dingtalk", appKey, appSecret),
		func(ctx context.Context) (*AccessToken, int, error) {
			t, err := GetAccessToken(ctx, appKey, appSecret, client...)
			if err != nil {
				return nil, 0, err
			}
			return t, t.ExpiresIn, nil
		})
}
