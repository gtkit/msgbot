package feishu_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/feishu"
)

func ExampleNewApp() {
	token := &feishu.AccessToken{
		TenantAccessToken: "t-tenant_access_token",
		Expire:            7200,
	}

	app, err := feishu.NewApp(token)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(app != nil)
	// Output: true
}

func ExampleApp_SendTextMessageTo() {
	// 收件人不是 open_id 时指定类型；非白名单取值在本地即被拒。
	app, err := feishu.NewApp(&feishu.AccessToken{TenantAccessToken: "tenant-token"})
	if err != nil {
		return
	}

	err = app.SendTextMessageTo(context.Background(), feishu.ReceiveIDType("nope"), "oc_xxx", "群通知")

	var e *msgbot.Error
	if errors.As(err, &e) {
		fmt.Println(e.Kind)
	}
	// Output: validation
}
