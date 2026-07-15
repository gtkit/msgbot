// Package internal 提供 msgbot 各平台共享的工具函数。
package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

// PostJSON 发送带 JSON 请求体的 HTTP POST 请求并返回响应体。
// 它负责处理 context 取消、请求创建与响应读取。
// 响应体限制为 1MB，以防意外的超大响应。
func PostJSON(ctx context.Context, client *http.Client, url string, body []byte) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", SanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	return ReadResponse(resp, 1<<20)
}

// HTTPError 表示非 2xx 的 HTTP 响应。它携带状态码，使调用方无需解析
// 错误字符串即可对失败进行分类（例如判断是否重试）。响应体保留用于
// 诊断，RetryAfter 在服务端返回 Retry-After 头时保存其解析后的值。
type HTTPError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

// maxBodyInError 是错误字符串中包含的响应体最大字节数。响应体上限为 1MiB，
// 若原样拼进错误串会造成超长错误（并可能带出上游网关/平台返回的无关内容），
// 因此在 Error() 中截断，仅保留开头用于诊断。
const maxBodyInError = 512

// Error 实现 error 接口。措辞（前缀 "unexpected status <code>"）保持稳定，
// 便于依赖匹配的调用方；响应体过长时被截断。
func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > maxBodyInError {
		// 按 rune 边界截断，避免切断多字节 UTF-8 字符。
		truncated := body[:maxBodyInError]
		for len(truncated) > 0 && !utf8.ValidString(truncated) {
			truncated = truncated[:len(truncated)-1]
		}
		body = truncated + "…(truncated)"
	}
	return fmt.Sprintf("unexpected status %d: %s", e.StatusCode, body)
}

// parseRetryAfter 解析 Retry-After 头的值，它可能是秒数或 HTTP 日期。
// 当值缺失或无法解析时返回 0。
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// ReadResponse 读取并校验 HTTP 响应体。
// 关闭 resp.Body 仍由调用方负责。
// 非 2xx 状态会连同 *HTTPError 一并返回响应体。
func ReadResponse(resp *http.Response, maxBody int64) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("read response: response is nil")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("read response: body is nil")
	}
	if maxBody <= 0 {
		return nil, fmt.Errorf("read response: max body must be positive")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxBody {
		return nil, fmt.Errorf("read response: body exceeds %d bytes", maxBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(data),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return data, nil
}
