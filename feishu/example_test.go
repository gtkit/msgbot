package feishu_test

import (
	"fmt"

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
