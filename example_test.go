package msgbot_test

import (
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
