package internal

import (
	"net/http"
	"time"
)

// 非 webhook 的 HTTP 调用在回退到下方共享 client 时使用的默认超时。
// 调用方传入的 *http.Client 或 context 截止时间始终优先于这些值。
const (
	// DefaultTimeout 限定 token 与应用消息请求的超时。
	DefaultTimeout = 10 * time.Second
	// DefaultUploadTimeout 限定图片上传与下载请求的超时，这类请求的
	// 延迟特征与小型 JSON 调用不同。
	DefaultUploadTimeout = 30 * time.Second
)

// 包级共享 client，使连接在多次调用间复用。
// http.DefaultClient 没有超时，绝不能用作回退。
var (
	defaultClient = &http.Client{Timeout: DefaultTimeout}
	uploadClient  = &http.Client{Timeout: DefaultUploadTimeout}
)

// DefaultClient 返回使用标准超时的共享 client，当调用方未传入 client 时，
// 用作 token 与消息请求的回退。
func DefaultClient() *http.Client { return defaultClient }

// DefaultUploadClient 返回使用较长超时的共享 client，当调用方未传入 client 时，
// 用作图片上传与下载的回退。
func DefaultUploadClient() *http.Client { return uploadClient }

// PickClient 返回调用方传入的第一个非 nil client，否则返回 fallback。
func PickClient(fallback *http.Client, client []*http.Client) *http.Client {
	if len(client) > 0 && client[0] != nil {
		return client[0]
	}
	return fallback
}
