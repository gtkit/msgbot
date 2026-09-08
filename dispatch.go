package msgbot

import (
	"context"
	"errors"

	"github.com/gtkit/msgbot/internal"
)

// —— Provider 扩展 API ——
//
// 下面的 BuildRequest 与 Config.Send，连同 Config.Freeze / GetHTTPClient /
// LogDebug / LogError 以及 errors.go 中的 WrapError / PlatformError /
// DecodeError / ValidationError，共同构成一组面向「实现自定义平台 Provider」
// 的扩展 API。终端用户无需接触它们——直接调用 feishu/wecom/dingtalk 的
// Send* 方法即可。仅当你要在本库之外实现一个新的 webhook Provider 时才使用，
// 用法见 ExampleConfig_Send。

// BuildRequest 产出一次发送尝试所需的内容：目标 URL 及待序列化为 JSON 的
// payload。它在每次尝试时都会执行一次，因此时间敏感的签名（如钉钉的时间戳或
// 飞书的 sign）会在每次重试时重新生成。返回错误将立即使发送失败且不重试。
type BuildRequest func() (url string, payload any, err error)

// Send 使用 config 的 HTTP 客户端和重试策略投递 JSON payload，随后解码通用的
// 平台 Response，并将任何失败分类为 *Error。它在 stats 上记录成功/错误计数。
// platform 与 op 用于标注最终的错误和调试日志。
//
// 边界安全：stats 为 nil 时跳过统计而不 panic；即便 config 从未 Freeze，
// GetHTTPClient 也会返回带超时的客户端，绝不落到无超时的 http.DefaultClient。
//
// 这是各平台 webhook provider 共用的发送路径；终端用户通常调用 provider 的
// Send* 方法，而非直接调用 Send。
func (c *Config) Send(ctx context.Context, stats *Stats, platform Platform, op string, build BuildRequest) error {
	// Switch 静音：这是全部 webhook 发送的唯一收口点，因此在这里短路即可覆盖
	// 三个平台的所有消息类型。既不成功也不失败，故 sent/error 都不动，
	// 单独计入 muted——否则静音就是一条完全无痕迹的静默丢弃。
	// 计数放在日志之前，使它不依赖日志分支是否被执行。
	if c.Muted() {
		if stats != nil {
			stats.IncMuted()
		}
		c.LogDebug(ctx, string(platform)+": muted by switch, message dropped", "op", op)
		return nil
	}

	// stats 每个发送任务只记录一次（而非每次重试尝试）；单次尝试的失败仍会
	// 记录日志以便诊断。
	err := c.Retry.do(ctx, func(ctx context.Context) error {
		url, payload, err := build()
		if err != nil {
			if e, ok := errors.AsType[*Error](err); ok {
				return e
			}
			// 只挂 Err，不再把 err.Error() 复制进 Message——两者都填会让
			// Error() 把同一句原因打印两遍。
			return &Error{Platform: platform, Operation: op, Kind: KindValidation, Err: err}
		}

		body, err := internal.Marshal(payload)
		if err != nil {
			return &Error{Platform: platform, Operation: op, Kind: KindValidation, Message: "marshal payload", Err: err}
		}

		c.LogDebug(ctx, string(platform)+": sending message", "endpoint", internal.URLOriginForLog(url))

		data, err := internal.PostJSON(ctx, c.GetHTTPClient(), url, body)
		if err != nil {
			e := classifySendError(platform, op, err)
			c.LogError(ctx, string(platform)+": send failed", "error", e)
			return e
		}

		var resp Response
		if err := internal.Unmarshal(data, &resp); err != nil {
			c.LogError(ctx, string(platform)+": decode response failed", "error", err)
			return &Error{Platform: platform, Operation: op, Kind: KindDecode, Message: "decode response", Err: err}
		}
		if code := resp.code(); code != 0 {
			e := &Error{
				Platform:  platform,
				Operation: op,
				Kind:      KindPlatform,
				Code:      code,
				Message:   resp.message(),
				Retryable: platformRetryable(platform, code),
			}
			c.LogError(ctx, string(platform)+": api error", "error", e)
			return e
		}
		return nil
	})

	if err != nil {
		if stats != nil {
			stats.IncError()
		}
		return err
	}
	if stats != nil {
		stats.IncSent()
	}
	return nil
}
