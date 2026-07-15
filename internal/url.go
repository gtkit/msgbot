package internal

import (
	"errors"
	"fmt"
	"net/url"
)

// URLOriginForLog 仅返回 URL 中非敏感的 origin 部分。
// webhook 的 path 与 query 字符串通常包含 access token 和签名。
func URLOriginForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[redacted]"
	}
	return parsed.Scheme + "://" + parsed.Host
}

// ValidateHTTPURL 校验一个绝对的 HTTP(S) 端点，且不在返回的错误中
// 回显可能敏感的原始 URL。
func ValidateHTTPURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("must be an absolute HTTP(S) URL with a host")
	}
	return nil
}

// SanitizeRequestError 从 net/url 传输错误中移除请求 URL，同时保留底层
// 原因以支持 errors.Is 和 errors.As 检查。
func SanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s request failed: %w", urlErr.Op, SanitizeRequestError(urlErr.Err))
	}
	return err
}
