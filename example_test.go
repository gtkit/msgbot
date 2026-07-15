package msgbot_test

import (
	"errors"
	"fmt"

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
	// Retry is opt-in; the zero value keeps single-attempt (at-most-once) sends.
	cfg := msgbot.Config{Retry: msgbot.RetryPolicy{MaxRetries: 2, Jitter: true}}

	fmt.Println(cfg.Retry.MaxRetries)
	// Output: 2
}
