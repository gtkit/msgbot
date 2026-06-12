// Package tokencache 提供各平台 access token 缓存的泛型核心：
// 读穿透编排、TTL 分段、缓存故障降级、in-flight 并发去重与默认内存缓存。
// 仅供 feishu/wecom/dingtalk 三个平台包内部使用，公开 API 不暴露本包类型。
package tokencache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Cache 是各平台 TokenCache 接口的泛型底层形态。
// 平台包的公开接口与之结构一致，接口值可隐式转换。
type Cache[T any] interface {
	Get(ctx context.Context) (*T, error)
	Set(ctx context.Context, t *T, ttl time.Duration) error
}

// Key 生成 in-flight 去重 key：platform 与 id 明文参与，
// secret 以 sha256 摘要参与——生成的 key 不包含明文 secret。
func Key(platform, id, secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return platform + "\x00" + id + "\x00" + hex.EncodeToString(sum[:])
}

// ttlFor 按分段规则计算回写 TTL，返回非正值表示不应写缓存：
//
//	expire <= 0        → 0（不缓存）
//	0 < expire <= 600  → expire/2
//	expire > 600       → expire - 300
//
// 阈值 600 保证两个分段衔接处不倒挂（600/2 = 600-300）。
func ttlFor(expireSeconds int) time.Duration {
	switch {
	case expireSeconds <= 0:
		return 0
	case expireSeconds <= 600:
		return time.Duration(expireSeconds/2) * time.Second
	default:
		return time.Duration(expireSeconds-300) * time.Second
	}
}

// Fetch 读穿透获取 token，行为契约：
//
//  1. cache 命中直接返回，不发上游请求；
//  2. cache.Get 返回错误按未命中处理（降级直连，不中断流程）；
//  3. 未命中经 g 按 key 去重：同 key 并发只发一次上游请求，
//     回写只执行一次，等待者共享结果；成为发起者后、发起上游请求前
//     会再查一次缓存（双重检查），覆盖"外层未命中但进组前缓存已被
//     前一个发起者填充"的竞态窗口；
//  4. 回写 TTL 按分段规则计算，非正值不写缓存；cache.Set 错误被忽略；
//  5. cache 为 nil 时跳过缓存读写，但仍参与去重（防冷启动击穿不依赖缓存存在）。
//
// fetch 返回 token 与其有效期秒数，由发起者的 ctx 控制。
func Fetch[T any](ctx context.Context, cache Cache[T], g *Group[T], key string,
	fetch func(ctx context.Context) (*T, int, error),
) (*T, error) {
	if cache != nil {
		if t, err := cache.Get(ctx); err == nil && t != nil {
			return t, nil
		}
	}
	return g.Do(ctx, key, func() (*T, error) {
		// 双重检查：等待进组期间缓存可能已被上一个发起者填充.
		if cache != nil {
			if t, err := cache.Get(ctx); err == nil && t != nil {
				return t, nil
			}
		}
		t, expire, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		if cache != nil {
			if ttl := ttlFor(expire); ttl > 0 {
				_ = cache.Set(ctx, t, ttl) // Set 失败降级为直连获取，不影响本次返回.
			}
		}
		return t, nil
	})
}
