package msgbot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/feishu"
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

func ExampleSwitch() {
	// 一个开关可以同时静音多个 provider：把同一实例放进它们的 Config。
	gate := msgbot.NewSwitch()
	cfg := msgbot.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", Switch: gate}

	fmt.Println(cfg.Muted())
	gate.Disable() // 静音期间 Send* 返回 nil，不发请求、不计入 Stats。
	fmt.Println(cfg.Muted())
	gate.Enable()
	fmt.Println(cfg.Muted())
	// Output:
	// false
	// true
	// false
}

func ExampleNewNamedManager() {
	// 同平台多目标：两个飞书机器人以不同名字共存，互不覆盖。
	p0, err := feishu.New(msgbot.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/p0"})
	if err != nil {
		return
	}
	oncall, err := feishu.New(msgbot.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/oncall"})
	if err != nil {
		return
	}

	mgr, err := msgbot.NewNamedManager(
		msgbot.Named("p0", p0),
		msgbot.Named("oncall", oncall),
	)
	if err != nil {
		return
	}

	fmt.Println(mgr.Names())
	fmt.Println(len(mgr.All()))
	// 按平台索引只能取到该平台最后注册的那个，因此精确投递用 GetNamed。
	fmt.Println(mgr.GetNamed("p0") == msgbot.Provider(p0))
	fmt.Println(mgr.Get(msgbot.PlatformFeishu) == msgbot.Provider(oncall))
	// Output:
	// [p0 oncall]
	// 2
	// true
	// true
}
