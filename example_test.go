package msgbot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gtkit/msgbot"
)

func ExampleWithAtAll() {
	opts := msgbot.ApplySendOptions([]msgbot.SendOption{
		msgbot.WithAtAll(),
		msgbot.WithAtUsers("ou_xxx"),
	})

	fmt.Println(opts.AtAll)
	fmt.Println(opts.AtUserIDs[0])
	// Output:
	// true
	// ou_xxx
}

func ExampleNewManager() {
	mgr := msgbot.NewManager()

	fmt.Println(mgr.Default() == nil)
	// Output: true
}

func ExampleError() {
	err := msgbot.ValidationError(msgbot.PlatformFeishu, "SendText", "text content is empty", nil)

	var e *msgbot.Error
	if errors.As(err, &e) {
		fmt.Println(e.Kind)
		fmt.Println(e.Retryable)
	}
	// Output:
	// validation
	// false
}

func ExampleRetryPolicy() {
	// Retry 需显式开启；零值保持单次尝试（at-most-once）发送。
	cfg := msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 2, Jitter: true}}

	fmt.Println(cfg.Retry.MaxRetries)
	// Output: 2
}

// stubRoundTripper 返回固定响应，仅用于示例，避免真实网络。
type stubRoundTripper struct{}

func (stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"errcode":0}`)),
	}, nil
}

// ExampleConfig_Send 演示 Provider 扩展 API：在本库之外实现自定义 webhook
// Provider 时，用 Config.Send 复用统一的发送、重试、结构化错误与统计逻辑。
func ExampleConfig_Send() {
	cfg := msgbot.Config{
		WebhookURL: "https://example.com/hook",
		HTTPClient: &http.Client{Transport: stubRoundTripper{}},
	}
	cfg.Freeze()

	var stats msgbot.Stats
	err := cfg.Send(context.Background(), &stats, msgbot.Platform("custom"), "SendText",
		func() (string, any, error) {
			// 每次尝试重新构造 URL 与 payload（此处可做签名等时间敏感处理）。
			return cfg.WebhookURL, map[string]any{"msgtype": "text"}, nil
		})

	fmt.Println(err)
	fmt.Println(stats.TotalSent())
	// Output:
	// <nil>
	// 1
}
