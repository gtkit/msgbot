package msgbot

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gtkit/msgbot/internal"
)

// ErrorKind 对失败进行分类，使调用方无需匹配错误字符串即可按失败类型分支处理。
type ErrorKind string

const (
	// KindValidation 表示本地输入在发出任何请求前就被拒绝。
	KindValidation ErrorKind = "validation"
	// KindTransport 表示网络/传输层失败（拨号、连接重置、超时、取消）。
	KindTransport ErrorKind = "transport"
	// KindHTTP 表示收到了非 2xx 的 HTTP 响应。
	KindHTTP ErrorKind = "http"
	// KindPlatform 表示平台返回 HTTP 200 但带有业务错误码。
	KindPlatform ErrorKind = "platform"
	// KindDecode 表示响应体无法解码。
	KindDecode ErrorKind = "decode"
)

// Error 是出站操作返回的结构化错误。使用 errors.As(err, &e)（其中 e 为 *Error）
// 检查它，然后根据 Kind、HTTPStatus、Code 和 Retryable 分支处理。被包裹的原因
// 会为 errors.Is / errors.As 保留，因此 errors.Is(err, context.Canceled) 等
// 判断可穿透它生效。
//
// 覆盖范围：三平台 webhook 发送、access token 获取、飞书图片上传/下载与
// 飞书 App 消息发送均返回 *Error。少数纯本地文件/参数错误（如打开文件失败）
// 仍以 fmt.Errorf 返回。
//
// Error 的消息中永远不包含 webhook URL、凭据或签名。
type Error struct {
	Platform   Platform      // 来源平台，非平台特定时为空。
	Operation  string        // 逻辑操作，例如 "SendText"。
	Kind       ErrorKind     // 失败分类。
	HTTPStatus int           // KindHTTP 时的 HTTP 状态码，否则为 0。
	Code       int           // KindPlatform 时的平台业务码，否则为 0。
	Message    string        // 不含敏感信息的可读详情。
	RetryAfter time.Duration // 服务端建议的退避时长，无则为 0。
	Retryable  bool          // 重试该操作是否可能成功。
	Err        error         // 被包裹的原因，可能为 nil。
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("msgbot")
	if e.Platform != "" {
		b.WriteString("/")
		b.WriteString(string(e.Platform))
	}
	if e.Operation != "" {
		b.WriteString(" ")
		b.WriteString(e.Operation)
	}
	b.WriteString(": ")
	b.WriteString(string(e.Kind))
	if e.HTTPStatus != 0 {
		b.WriteString(" (status ")
		b.WriteString(strconv.Itoa(e.HTTPStatus))
		b.WriteString(")")
	}
	if e.Code != 0 {
		b.WriteString(" (code ")
		b.WriteString(strconv.Itoa(e.Code))
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap 返回被包裹的原因，供 errors.Is / errors.As 使用。
func (e *Error) Unwrap() error { return e.Err }

// ValidationError 为本地被拒绝的输入返回一个 validation 类别的 *Error。
// provider 包使用它，使校验失败可被分类（Kind == KindValidation）且
// 永不重试。cause 可以为 nil。
func ValidationError(platform Platform, op, msg string, cause error) *Error {
	return &Error{Platform: platform, Operation: op, Kind: KindValidation, Message: msg, Err: cause}
}

// WrapError 把一次外发 HTTP 调用的底层错误分类为结构化 *Error 并保留原因。
// 这是面向 Provider 扩展的构造器：token、图片、飞书 App 等路径用它，使其失败
// 与 webhook 路径一样可分类（transport / http / 已取消 ctx）。err 为 nil 时返回 nil。
func WrapError(platform Platform, op string, err error) *Error {
	if err == nil {
		return nil
	}
	return classifySendError(platform, op, err)
}

// PlatformError 为 HTTP 200 但带业务错误码的响应构造 KindPlatform 的 *Error，
// 当 code 属于已知限流码时标记为可重试。面向 Provider 扩展使用。
func PlatformError(platform Platform, op string, code int, msg string) *Error {
	return &Error{
		Platform:  platform,
		Operation: op,
		Kind:      KindPlatform,
		Code:      code,
		Message:   msg,
		Retryable: platformRetryable(platform, code),
	}
}

// DecodeError 为无法解析的响应体构造 KindDecode 的 *Error。面向 Provider 扩展使用。
func DecodeError(platform Platform, op, msg string, cause error) *Error {
	return &Error{Platform: platform, Operation: op, Kind: KindDecode, Message: msg, Err: cause}
}

// isRetryable 报告 err 是否为标记了可重试的结构化 *Error。
func isRetryable(err error) bool {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Retryable
	}
	return false
}

// classifySendError 将来自内部 HTTP 辅助函数的原始错误转换为结构化 *Error，
// 并保留其原因。context 取消与超时属于永不重试的传输失败；已识别的非 2xx
// 状态归为 KindHTTP，并基于状态码决定是否重试；其他情况均视为可重试的传输失败。
func classifySendError(platform Platform, op string, err error) *Error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Platform: platform, Operation: op, Kind: KindTransport, Retryable: false, Err: err}
	}
	if he, ok := errors.AsType[*internal.HTTPError](err); ok {
		return &Error{
			Platform:   platform,
			Operation:  op,
			Kind:       KindHTTP,
			HTTPStatus: he.StatusCode,
			RetryAfter: he.RetryAfter,
			Retryable:  httpRetryable(he.StatusCode),
			Err:        err,
		}
	}
	return &Error{Platform: platform, Operation: op, Kind: KindTransport, Retryable: true, Err: err}
}

// httpRetryable 报告某个 HTTP 状态码是否值得重试：408、425、429，或任意 5xx。
func httpRetryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return status >= 500 && status <= 599
}

// platformRetryable 报告通过 HTTP 200 返回的平台业务错误码是否为值得重试的
// 限流/瞬时错误码。以下错误码摘录自各平台官方文档：
//   - 飞书 11232：消息创建触发系统频率限制。
//   - 企业微信 45009：API 调用频率限制；45033：并发调用限制。
//   - 钉钉 130101：发送过快（超过单机器人每分钟 20 条消息）。
func platformRetryable(platform Platform, code int) bool {
	switch platform {
	case PlatformFeishu:
		return code == 11232
	case PlatformWeCom:
		return code == 45009 || code == 45033
	case PlatformDingTalk:
		return code == 130101
	}
	return false
}
