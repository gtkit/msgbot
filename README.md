# msgbot

`msgbot` 是多平台 IM 机器人消息推送 Go 库，支持飞书、企业微信和钉钉。

## 安装

```bash
go get github.com/gtkit/msgbot
```

## 快速开始

```go
import (
    "context"

    "github.com/gtkit/msgbot"
    "github.com/gtkit/msgbot/feishu"
)

bot, err := feishu.New(msgbot.Config{
    WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/your-token",
    Secret:     "your-secret",
})
if err != nil {
    return err
}

return bot.SendText(context.Background(), "hello", msgbot.WithAtAll())
```

## API 概览

导航表；每个平台的完整可调用方法见 [方法一览](#方法一览)，所有构造函数见 [构造函数详解](#构造函数详解)。

| API | 说明 |
|-----|------|
| `msgbot.Provider` | 三平台通用发送接口（`SendText`/`SendMarkdown`/`SendRichText`/`SendImage`） |
| `msgbot.Config` | provider 配置（`WebhookURL`/`Secret`/`HTTPClient`/`Timeout`/`Logger`/`Retry`/`Switch`） |
| `msgbot.WithAtAll` / `WithAtUsers` | 发送选项：@所有人 / @指定人 |
| `msgbot.NewSwitch` | 运行期发送开关（`Config.Switch`，关闭时 `Send*` 返回 nil 且不发请求） |
| `msgbot.NewManager` | 管理多个平台 Provider（`Get` 按平台取用，另有 `All`/`Multi`，可选 `SetDefault`/`Default`） |
| `msgbot.NewNamedManager` | 按名字注册 Provider，支持同平台多目标（`GetNamed`/`Names`） |
| `msgbot.NewMulti` | 并发广播到多个 Provider |
| `webhook.New` | 通用 webhook Provider：向任意 HTTP(S) 端点发送自定义 JSON |
| `msgbot.RetryPolicy` | 可选重试策略（`Config.Retry`，默认关闭） |
| `msgbot.Error` / `msgbot.ErrorKind` | 结构化错误，可 `errors.As` 判类型 |
| `feishu.New` / `wecom.New` / `dingtalk.New` | 创建各平台 Webhook Provider |
| `feishu.NewApp` / `feishu.NewAppWithTokenSource` | 创建飞书自建应用消息客户端（静态 / 可刷新 token） |
| `feishu.NewAppWithConfig` | 以 `feishu.AppConfig` 创建应用消息客户端，可配发送开关 |
| `feishu.ReceiveIDType` | 飞书应用消息收件人类型（`open_id`/`user_id`/`union_id`/`email`/`chat_id`） |
| `*.GetAccessToken` / `*.GetAccessTokenCached` | 直连 / 带缓存获取 token（三平台同形态） |
| `*.NewMemoryTokenCache` | 进程内默认 token 缓存（三平台同形态） |
| `feishu.BuildRichText` / `BuildRichTextLines` | 构造飞书富文本消息 |
| `wecom.BuildImageMessage` | 从字节构造企微图片消息 |
| `dingtalk.ActionCard` / `FeedLink` | 钉钉特有的卡片/图文载体 |

## 飞书

飞书自定义机器人 webhook 支持文本、Markdown（互动卡片）、富文本（post）、图片四种消息。

```go
package main

import (
    "context"
    "log"

    "github.com/gtkit/msgbot"
    "github.com/gtkit/msgbot/feishu"
)

func feishuDemo() {
    ctx := context.Background()

    // 创建飞书 webhook 机器人。
    // WebhookURL 是机器人的 hook 地址；Secret 是「加签」安全设置里的密钥，
    // 未开启加签则留空。构造失败（URL 非法等）会返回 error，务必检查。
    fs, err := feishu.New(msgbot.Config{
        WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/your-token",
        Secret:     "your-sign-secret", // 可选：机器人开启「加签」时填写
    })
    if err != nil {
        log.Fatalf("创建飞书机器人失败: %v", err)
    }

    // 1) 纯文本 + @。WithAtAll @所有人；WithAtUsers 传 user_id 或 open_id，
    //    被 @ 的 ID 会自动做转义，含特殊字符也不会破坏消息结构。
    if err := fs.SendText(ctx, "服务已上线", msgbot.WithAtAll()); err != nil {
        log.Printf("发送文本失败: %v", err)
    }

    // 2) Markdown（飞书以「互动卡片」承载，title 作为卡片标题）。
    //    Markdown 场景的 @ 用卡片 <at id=...> 语法，本库已自动处理，
    //    直接传 WithAtUsers 即可 @ 到指定人。
    if err := fs.SendMarkdown(ctx, "部署报告",
        "**状态**: 成功\n**耗时**: 42s",
        msgbot.WithAtUsers("ou_xxxxxx"),
    ); err != nil {
        log.Printf("发送 Markdown 失败: %v", err)
    }

    // 3) 富文本（post）——飞书原生结构化消息，可混排文本、超链接、@、图片。
    //    BuildRichText 是便捷构造器：标题、正文、一个可选超链接、是否 @所有人。
    msg := feishu.BuildRichText(
        "告警",                                                              // 标题
        "服务响应超时，请及时处理 ",                                          // 正文文本
        &msgbot.RichTextTag{Tag: "a", Text: "查看详情", Href: "https://example.com"}, // 可选超链接
        true,                                                                // 末尾追加 @所有人
    )
    if err := fs.SendRichText(ctx, msg); err != nil {
        log.Printf("发送富文本失败: %v", err)
    }

    // 4) 图片。webhook 发图需要先拿到 image_key（上传接口返回），再发送。
    //    最简单的实战方式是 SendImageFromFile：内部先用 tenant_access_token
    //    上传本地图片拿 image_key，再发出去。tenant token 由自建应用换取（见下文）。
    if err := fs.SendImageFromFile(ctx, "tenant-access-token", "/path/to/chart.png"); err != nil {
        log.Printf("发送图片失败: %v", err)
    }
    // 若已持有 image_key，也可直接发：
    // fs.SendImage(ctx, &msgbot.ImageMessage{ImageKey: "img_xxx"})
}
```

### 飞书自建应用消息

```go
token, err := feishu.GetAccessToken(ctx, "cli_xxx", "secret_xxx")
app, err := feishu.NewApp(token)

err = app.SendTextMessage(ctx, "ou_xxx", "应用消息")
err = app.SendImageMessage(ctx, "ou_xxx", "/path/to/image.png")
err = token.DownloadImage(ctx, "img_xxx", "/path/to/save.png")
```

收件人不是 `open_id` 时用 `*To` 方法指定类型，支持 `open_id`、`user_id`、`union_id`、`email`、`chat_id` 五种：

```go
// 按 chat_id 发到群
err = app.SendTextMessageTo(ctx, feishu.ReceiveIDChatID, "oc_xxx", "群通知")
// 按邮箱发给个人
err = app.SendTextMessageTo(ctx, feishu.ReceiveIDEmail, "someone@example.com", "私信")
```

白名单之外的类型（含空值）会在本地返回校验错误，不解析 token、也不发出请求。`SendTextMessage` / `SendImageMessage` 等价于以 `feishu.ReceiveIDOpenID` 调用 `*To` 方法。

建议使用 `feishu.GetAccessTokenCached` 自动缓存 token，见下文 [Access Token 缓存](#access-token-缓存)。

token 约 2 小时过期。若应用长期运行，用 `feishu.NewAppWithTokenSource` 让客户端每次操作自动获取最新 token（配合缓存不会频繁请求）：

```go
var tokenCache = feishu.NewMemoryTokenCache()

app, _ := feishu.NewAppWithTokenSource(func(ctx context.Context) (*feishu.AccessToken, error) {
    return feishu.GetAccessTokenCached(ctx, "cli_xxx", "secret_xxx", tokenCache)
})
// 一次 SendImageMessage 内的上传与发送使用同一个 token，不会中途刷新。
```

`feishu.NewApp(token)` 仍可用，但持有静态 token 快照，过期后需重建客户端。

## 企业微信

企业微信群机器人 webhook 支持文本、Markdown、图片。图片走 base64+md5，无需独立上传接口；机器人 webhook 无需 Secret 加签。

```go
package main

import (
    "context"
    "log"

    "github.com/gtkit/msgbot"
    "github.com/gtkit/msgbot/wecom"
)

func wecomDemo() {
    ctx := context.Background()

    // 创建企业微信群机器人。key 即群机器人 webhook 地址里的 key 参数，
    // 企业微信 webhook 不使用加签，Secret 留空即可。
    wc, err := wecom.New(msgbot.Config{
        WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=your-key",
    })
    if err != nil {
        log.Fatalf("创建企业微信机器人失败: %v", err)
    }

    // 1) 纯文本 + @。企业微信 @ 用 mentioned_list：WithAtAll 对应 @all，
    //    WithAtUsers 传成员 userid（不是手机号）。二者可同时传，会合并。
    if err := wc.SendText(ctx, "值班提醒",
        msgbot.WithAtAll(),
        msgbot.WithAtUsers("zhangsan", "lisi"),
    ); err != nil {
        log.Printf("发送文本失败: %v", err)
    }

    // 2) Markdown。企业微信 Markdown 支持标题、加粗、链接、引用，
    //    以及 <font color="info|comment|warning"> 三种颜色高亮。
    //    ⚠ 企业微信 Markdown 无法 @ 指定人：即使传了 WithAtUsers 也只会被忽略
    //    （消息照常发出，并记一条 debug 日志）。要 @ 到人请改用 SendText。
    if err := wc.SendMarkdown(ctx, "运营日报",
        `实时新增用户反馈<font color="warning">132</font>例，请相关同事注意。`,
    ); err != nil {
        log.Printf("发送 Markdown 失败: %v", err)
    }

    // 3) 图片（本地文件）。SendImageFromFile 内部读取文件并计算 base64+md5 发送。
    if err := wc.SendImageFromFile(ctx, "/path/to/alert.png"); err != nil {
        log.Printf("发送图片失败: %v", err)
    }
    // 若图片已在内存中（[]byte），用 BuildImageMessage 构造后发送：
    // img := wecom.BuildImageMessage(pngBytes)
    // wc.SendImage(ctx, img)
}
```

## 钉钉

钉钉自定义机器人 webhook 支持文本、Markdown、Link、ActionCard、FeedCard；图片只能通过公网 URL 以 Markdown 嵌入（webhook 无上传接口）。

```go
package main

import (
    "context"
    "log"

    "github.com/gtkit/msgbot"
    "github.com/gtkit/msgbot/dingtalk"
)

func dingtalkDemo() {
    ctx := context.Background()

    // 创建钉钉机器人。Secret 是「加签」安全设置里以 SEC 开头的密钥；
    // 钉钉签名带时间戳，本库在每次发送（含重试）时都会重新签名，无需手动处理。
    dt, err := dingtalk.New(msgbot.Config{
        WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=your-token",
        Secret:     "SEC-xxxxxx", // 可选：机器人开启「加签」时填写
    })
    if err != nil {
        log.Fatalf("创建钉钉机器人失败: %v", err)
    }

    // 1) 纯文本 + @。钉钉 @ 用手机号：WithAtUsers 传手机号。
    //    钉钉规则是「正文里出现 @手机号 才会真正高亮通知」，本库已自动把
    //    @手机号 注入正文，同时设置 atMobiles，无需自己拼。
    if err := dt.SendText(ctx, "发布完成", msgbot.WithAtUsers("13800001111")); err != nil {
        log.Printf("发送文本失败: %v", err)
    }

    // 2) Markdown。⚠ 钉钉 Markdown 的 title 为必填（会作为通知摘要），
    //    传空标题会在本地直接返回校验错误、不会发出请求。
    //    同样支持用 WithAtUsers/WithAtAll @人，正文会自动注入 @手机号。
    if err := dt.SendMarkdown(ctx, "杭州天气",
        "#### 杭州天气 @13800001111\n> 9℃ 多云转晴",
        msgbot.WithAtUsers("13800001111"),
    ); err != nil {
        log.Printf("发送 Markdown 失败: %v", err)
    }

    // 3) Link 消息：标题、正文、跳转链接、缩略图 URL（缩略图可留空）。
    if err := dt.SendLink(ctx,
        "版本发布", "v1.1.0 已发布，点击查看更新日志",
        "https://example.com/changelog", "https://example.com/logo.png",
    ); err != nil {
        log.Printf("发送 Link 失败: %v", err)
    }

    // 4) ActionCard：带按钮的卡片。Buttons 非空时为「独立按钮」模式；
    //    若想整卡片跳转，改用 SingleTitle + SingleURL 字段。
    if err := dt.SendActionCard(ctx, &dingtalk.ActionCard{
        Title:          "CI 构建失败", // 通知摘要
        Text:           "### 构建失败\n分支 main 编译未通过",
        BtnOrientation: "0", // "0" 竖排按钮，"1" 横排
        Buttons: []dingtalk.Button{
            {Title: "查看日志", ActionURL: "https://ci.example.com/123"},
            {Title: "重新构建", ActionURL: "https://ci.example.com/123/rebuild"},
        },
    }); err != nil {
        log.Printf("发送 ActionCard 失败: %v", err)
    }

    // 5) FeedCard：多条图文列表。
    if err := dt.SendFeedCard(ctx, []dingtalk.FeedLink{
        {Title: "周报一", MessageURL: "https://example.com/1", PicURL: "https://example.com/1.png"},
        {Title: "周报二", MessageURL: "https://example.com/2", PicURL: "https://example.com/2.png"},
    }); err != nil {
        log.Printf("发送 FeedCard 失败: %v", err)
    }

    // 6) 图片：钉钉 webhook 无上传接口，只能传公网可访问的图片 URL。
    if err := dt.SendImageFromURL(ctx, "https://example.com/alert.png"); err != nil {
        log.Printf("发送图片失败: %v", err)
    }
}
```

## 方法一览

### 通用发送接口（三平台 Provider 共有）

三个平台的 `*Webhook`（以及 `webhook` 包的通用 Provider）都实现 `msgbot.Provider` 接口，以下 4 个发送方法 + `Platform` 签名完全一致，可用 `msgbot.Provider` 统一持有：

| 方法 | 签名要点 | 说明 |
|------|---------|------|
| `SendText` | `(ctx, text string, opts ...SendOption) error` | 发送纯文本，支持 `WithAtAll` / `WithAtUsers` |
| `SendMarkdown` | `(ctx, title, content string, opts ...SendOption) error` | 发送 Markdown（飞书为互动卡片，钉钉 title 必填） |
| `SendRichText` | `(ctx, msg *RichTextMessage) error` | 发送富文本；仅飞书原生，其余降级为 Markdown |
| `SendImage` | `(ctx, img *ImageMessage) error` | 发送图片；各平台所需字段不同（见下） |
| `Platform` | `() Platform` | 返回平台标识（`feishu`/`wecom`/`dingtalk`/`webhook`） |

> 关于 `Stats`：三平台的 `*Webhook` 与 `webhook.Webhook` 还各自提供 `Stats() *msgbot.Stats`（返回 `TotalSent` / `TotalError` 两个进程内累计计数，按「发送任务」计而非每次重试尝试）。但它**不是 `msgbot.Provider` 接口的方法**——需通过具体类型（如 `*feishu.Webhook`）访问，用 `msgbot.Provider` 变量持有时无法调用。

发送选项（`msgbot` 包）：

| 选项 | 说明 |
|------|------|
| `WithAtAll()` | @所有人 |
| `WithAtUsers(ids ...string)` | @指定人；飞书传 user_id/open_id、企微传 userid、钉钉传手机号 |

### 飞书 `feishu`

| 方法 / 函数 | 签名要点 | 说明 |
|------------|---------|------|
| `(*Webhook).SendText/SendMarkdown/SendRichText/SendImage` | 见通用接口 | `SendImage` 需 `ImageMessage.ImageKey`（上传后获得） |
| `(*Webhook).SendImageFromFile` | `(ctx, tenantAccessToken, path string) error` | 便捷方法：先上传本地图片拿 image_key 再发 |
| `(*App).SendTextMessage` | `(ctx, openID, text string) error` | 自建应用按 open_id 发文本 |
| `(*App).SendImageMessage` | `(ctx, openID, path string) error` | 自建应用上传并按 open_id 发图片（单次操作内上传/发送同一 token） |
| `(*App).SendTextMessageTo` | `(ctx, idType ReceiveIDType, receiveID, text string) error` | 按指定收件人类型发文本（如 `chat_id` 发群） |
| `(*App).SendImageMessageTo` | `(ctx, idType ReceiveIDType, receiveID, path string) error` | 按指定收件人类型发图片 |
| `NewAppWithConfig` | `(cfg AppConfig) (*App, error)` | 以 `AppConfig` 创建 App，可配 `Switch` 发送开关 |
| `GetAccessToken` | `(ctx, appID, appSecret string, client ...*http.Client) (*AccessToken, error)` | 直连获取自建应用 token |
| `GetAccessTokenCached` | `(ctx, appID, appSecret string, cache TokenCache, client ...*http.Client) (*AccessToken, error)` | 带缓存获取，见下文 Access Token 缓存 |
| `BuildRichText` | `(title, text string, link *RichTextTag, atAll bool) *RichTextMessage` | 快速构造单行富文本 |
| `BuildRichTextLines` | `(title string, lines ...[]RichTextTag) *RichTextMessage` | 由多行构造富文本 |

#### 高级 / 扩展 API（飞书）

以下面向「上传图片复用 image_key」「下载图片」「直接调用飞书其他开放 API」等进阶场景，普通消息推送用不到：

| 方法 / 函数 | 签名要点 | 说明 |
|------------|---------|------|
| `UploadImageFromFile` | `(ctx, tenantAccessToken, path string, client ...*http.Client) (*UploadImageResp, error)` | 从文件路径上传，返回 image_key（限 10MB） |
| `UploadImageFromReader` | `(ctx, tenantAccessToken, filename string, reader io.Reader, client ...*http.Client) (*UploadImageResp, error)` | 从任意 `io.Reader` 上传（限 10MB） |
| `(*UploadImageResp).ImageKey` | `() string` | 取上传成功后的 image_key |
| `(*AccessToken).UploadImageWithToken` | `(ctx, path string, client ...*http.Client) (*UploadImageResp, error)` | 用该 token 上传本地图片（等价 `UploadImageFromFile`） |
| `(*AccessToken).DownloadImage` | `(ctx, imageKey, savePath string, client ...*http.Client) error` | 下载图片到本地 |
| `(*AccessToken).TenantToken` / `AppToken` | `() string` | 返回带 `Bearer ` 前缀的 tenant / app token（供直接调用飞书其他开放 API） |

### 企业微信 `wecom`

| 方法 / 函数 | 签名要点 | 说明 |
|------------|---------|------|
| `(*Webhook).SendText/SendMarkdown/SendRichText/SendImage` | 见通用接口 | `SendImage` 需 `ImageMessage.Base64` + `MD5`；Markdown 无法 @ 指定人 |
| `(*Webhook).SendImageFromFile` | `(ctx, path string) error` | 读本地文件、自动算 base64+md5 发送（限 2MB） |
| `BuildImageMessage` | `(data []byte) *ImageMessage` | 从字节构造图片消息（base64+md5） |
| `GetAccessToken` | `(ctx, corpID, corpSecret string, client ...*http.Client) (*AccessToken, error)` | 直连获取 token |
| `GetAccessTokenCached` | `(ctx, corpID, corpSecret string, cache TokenCache, client ...*http.Client) (*AccessToken, error)` | 带缓存获取 |
| `(*AccessToken).Token` | `() string` | 返回 access_token 字符串 |

### 钉钉 `dingtalk`

| 方法 / 函数 | 签名要点 | 说明 |
|------------|---------|------|
| `(*Webhook).SendText/SendMarkdown/SendRichText/SendImage` | 见通用接口 | `SendMarkdown` title 必填；`SendImage` 需 `ImageMessage.PicURL`（公网 URL） |
| `(*Webhook).SendLink` | `(ctx, title, text, messageURL, picURL string) error` | 发送 Link 消息（钉钉特有） |
| `(*Webhook).SendActionCard` | `(ctx, card *ActionCard) error` | 发送 ActionCard 按钮卡片 |
| `(*Webhook).SendFeedCard` | `(ctx, links []FeedLink) error` | 发送 FeedCard 图文列表 |
| `(*Webhook).SendImageFromURL` | `(ctx, picURL string) error` | 便捷方法：以公网 URL 发图 |
| `GetAccessToken` | `(ctx, appKey, appSecret string, client ...*http.Client) (*AccessToken, error)` | 直连获取企业内部应用 token |
| `GetAccessTokenCached` | `(ctx, appKey, appSecret string, cache TokenCache, client ...*http.Client) (*AccessToken, error)` | 带缓存获取 |
| `(*AccessToken).Token` | `() string` | 返回 access_token 字符串 |

钉钉特有的消息载体类型：`ActionCard{Title, Text, SingleTitle, SingleURL, BtnOrientation, Buttons}`、`Button{Title, ActionURL}`、`FeedLink{Title, MessageURL, PicURL}`。

## 构造函数详解

### Webhook 机器人：`feishu.New` / `wecom.New` / `dingtalk.New`

三平台签名一致：`func New(cfg msgbot.Config) (*Webhook, error)`。`WebhookURL` 非法或为空会返回 error，务必检查。`msgbot.Config` 字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `WebhookURL` | `string` | **必填**，机器人 hook 地址 |
| `Secret` | `string` | 可选，加签密钥（飞书/钉钉「加签」安全设置；企微 webhook 不使用） |
| `HTTPClient` | `*http.Client` | 可选，自定义客户端；为 nil 时用带超时的默认客户端 |
| `Timeout` | `time.Duration` | 可选，`HTTPClient` 为 nil 时的超时，默认 10s |
| `Logger` | `msgbot.Logger` | 可选，调试/错误日志接口；为 nil 则不记日志 |
| `Retry` | `msgbot.RetryPolicy` | 可选，重试策略，零值关闭（见「错误处理与重试」） |
| `Switch` | `*msgbot.Switch` | 可选，运行期发送开关；为 nil 则始终启用（见「发送开关」） |

```go
fs, err := feishu.New(msgbot.Config{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", Secret: "SEC..."})
wc, err := wecom.New(msgbot.Config{WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"})
dt, err := dingtalk.New(msgbot.Config{WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=xxx", Secret: "SEC..."})
```

### 飞书自建应用：`feishu.NewApp` / `feishu.NewAppWithTokenSource`

- `func NewApp(token *AccessToken, client ...*http.Client) (*App, error)`：从**静态 token** 创建。token 按原样使用、不会刷新，过期后需重建客户端。适合一次性或短生命周期场景。
- `func NewAppWithTokenSource(source TokenSource, client ...*http.Client) (*App, error)`：从 `TokenSource`（`func(context.Context) (*AccessToken, error)`）创建，**每次操作自动解析最新 token**，可刷新过期 token。长期运行的服务推荐用它，配合 `GetAccessTokenCached` 不会频繁请求。`source` 需并发安全。
- `func NewAppWithConfig(cfg AppConfig) (*App, error)`：以结构体字面量配置，是**唯一能配发送开关**的构造函数。`AppConfig` 字段：`Token`（静态 token）与 `Source`（可刷新）**二选一，必须恰好提供一个**（都为空或都提供都返回错误，不搞静默优先级）、`HTTPClient`（可选）、`Switch`（可选，见「发送开关」）。

```go
// 静态 token
token, _ := feishu.GetAccessToken(ctx, "cli_xxx", "secret_xxx")
app, _ := feishu.NewApp(token)

// 可刷新 token（推荐）
var cache = feishu.NewMemoryTokenCache()
app, _ = feishu.NewAppWithTokenSource(func(ctx context.Context) (*feishu.AccessToken, error) {
    return feishu.GetAccessTokenCached(ctx, "cli_xxx", "secret_xxx", cache)
})
```

### 多平台管理：`msgbot.NewManager` / `msgbot.NewMulti`

- `func NewManager(providers ...Provider) *Manager`：注册多个平台 Provider（nil 跳过，同平台后者覆盖前者）。推荐用 `Get(Platform)` 按平台**显式**取用；`All()` 取全部、`Multi() (*Multi, error)` 转广播分发器。如需「默认平台」语义，用 `SetDefault(Platform) error`（未注册平台返回错误）显式指定后由 `Default()` 读取——未显式设置时默认为第一个注册的 Provider（隐式行为，不建议依赖）。另有 `Feishu()`/`WeCom()`/`DingTalk()` 三个固定平台便捷 getter。
- `func NewNamedManager(providers ...NamedProvider) (*Manager, error)`：按名字注册，**同平台可以有多个目标**。用 `Named("p0", fs)` 构造注册项，用 `GetNamed("p0")` 取用，`Names()` 按注册顺序列出全部名字。空参数、空名字、重名、nil provider 均返回错误。具名 provider 同时进入平台索引，`Get`/`Default` 等照常可用；同平台多个时平台索引沿用「后者覆盖前者」，`Default()` 为第一个注册项的平台。
- `func NewMulti(providers ...Provider) (*Multi, error)`：并发广播分发器，至少一个 provider；nil provider 返回错误。方法与 Provider 同名（`SendText`/`SendMarkdown`/`SendRichText`/`SendImage`），部分失败以 `errors.Join` 聚合返回。

`All()` 按注册顺序返回副本，每个注册项一条；`NewManager` 下被同平台后者覆盖掉的 provider 不会出现。`Multi()` 基于 `All()` 构造，因此具名注册下会向每个具名目标各发一次。

### 通用 webhook：`webhook.New`

`func New(cfg msgbot.Config, build PayloadBuilder) (*Webhook, error)`：向任意绝对 HTTP(S) 端点发送 `POST` + `application/json` 请求，请求体由 `build` 决定。`WebhookURL` 非法或为空、`build` 为 nil 时返回 error。`Platform()` 返回 `msgbot.PlatformWebhook`。

### Token 缓存：`*.NewMemoryTokenCache`

三平台各有 `func NewMemoryTokenCache() *MemoryTokenCache`：进程内默认缓存，基于 `atomic.Pointer` 零锁并发安全。**一个实例只服务一组凭证**，多组凭证各自创建。多实例部署需共享 token 时，实现各包的 `TokenCache` 接口接入 Redis（见下文）。

## Access Token 缓存

三平台的 `GetAccessToken` 均为无状态直连请求，token 有效期约 7200 秒（企业微信对 gettoken 有频率限制）。`GetAccessTokenCached` 提供带缓存的获取：缓存命中直接返回；未命中请求平台 API，按有效期算好 TTL（提前 300 秒）回写缓存；同凭证并发未命中时只发一次上游请求（防冷启动击穿）。

```go
// 单进程：内存缓存开箱即用（包级创建一份，重复使用）
var tokenCache = feishu.NewMemoryTokenCache()

token, err := feishu.GetAccessTokenCached(ctx, "cli_xxx", "secret_xxx", tokenCache)
```

企业微信、钉钉形态完全一致：`wecom.GetAccessTokenCached(ctx, corpID, corpSecret, cache)`、`dingtalk.GetAccessTokenCached(ctx, appKey, appSecret, cache)`。

多实例部署需要共享 token 时，实现各包的 `TokenCache` 接口（`Get`/`Set` 两个方法）接入 Redis，完整示例见 `feishu.TokenCache` 的文档注释。

注意事项：

- **一个 `TokenCache` 实例只能服务一组凭证**，多个 app/企业各自创建实例，Redis 实现应在构造时绑定唯一 key；
- 缓存后端故障会自动降级为直连获取，不阻断调用；错误被静默忽略，需要观测时在自定义 `TokenCache` 实现内记录；
- `cache` 传 `nil` 等价于 `GetAccessToken`（不持久缓存，但仍保留并发去重）。

## 多平台广播

```go
multi, _ := msgbot.NewMulti(fs, wc, dt)
err := multi.SendText(ctx, "全平台通知", msgbot.WithAtAll())
```

## 同平台多目标

告警分级常常需要同一平台的多个机器人（P0 群、值班群、归档群）。`NewManager` 以平台为索引，同平台后者会覆盖前者；这种场景用 `NewNamedManager` 按名字注册：

```go
p0, _ := feishu.New(msgbot.Config{WebhookURL: p0Hook})
oncall, _ := feishu.New(msgbot.Config{WebhookURL: oncallHook})
archive, _ := dingtalk.New(msgbot.Config{WebhookURL: archiveHook, Secret: "SEC..."})

mgr, err := msgbot.NewNamedManager(
    msgbot.Named("p0", p0),
    msgbot.Named("oncall", oncall),
    msgbot.Named("archive", archive),
)
if err != nil {
    return err
}

// 按名字精确投递
_ = mgr.GetNamed("p0").SendText(ctx, "P0 故障", msgbot.WithAtAll())

// 广播到全部三个目标（两个飞书群各收到一次）
multi, _ := mgr.Multi()
_ = multi.SendText(ctx, "演练通知")
```

名字是调用方给的标识，因此空名字与重名都返回错误而不是静默覆盖——否则某个群会永远收不到消息。`Names()` 按注册顺序返回全部名字的副本。

具名 provider 同时进入平台索引，`Get`/`Feishu()`/`Default()` 等照常可用，但同平台多目标时它们只能取到该平台**最后**注册的那个（上例中 `Get(PlatformFeishu)` 与 `Default()` 都返回 `oncall`）。这种场景请用 `GetNamed` 精确取用。

## 发送开关

`Config.Switch` 是一个可在运行期翻转的发送开关，用于本地开发、压测与故障降级。关闭期间 `Send*` 直接返回 `nil`：不发出请求、不计入 `Stats`，并记一条 debug 日志（配了 `Logger` 时）说明消息被静音。

```go
gate := msgbot.NewSwitch() // 创建即启用

// 多个 provider 共享同一个开关，一次 Disable 全部静音
fs, _ := feishu.New(msgbot.Config{WebhookURL: feishuHook, Switch: gate})
dt, _ := dingtalk.New(msgbot.Config{WebhookURL: dingHook, Switch: gate})

if os.Getenv("APP_ENV") == "local" {
    gate.Disable()
}

_ = fs.SendText(ctx, "这条在静音期间不会发出") // 返回 nil

gate.Enable() // 恢复发送
```

开关的作用域就是「共享该实例的那些 provider」——本库没有全局开关。`Config.Switch` 为 nil 时始终启用，行为与不配置开关完全一致。

`feishu.Webhook.SendImageFromFile` 会在上传之前就检查开关，因此静音期间不会调用飞书上传 API。

飞书应用消息也受同一个开关控制，但要用 `feishu.NewAppWithConfig` 构造（`NewApp` / `NewAppWithTokenSource` 没有开关字段，创建出的 App 始终启用）：

```go
app, err := feishu.NewAppWithConfig(feishu.AppConfig{
    Source: func(ctx context.Context) (*feishu.AccessToken, error) {
        return feishu.GetAccessTokenCached(ctx, "cli_xxx", "secret_xxx", tokenCache)
    },
    Switch: gate, // 与上面三个 webhook provider 共用一个开关
})
```

静音期间 App 的 `Send*` 在参数校验通过后直接返回 nil：不解析 token、不上传图片、不发请求。**参数校验优先于静音**——静音是运维开关，不该把写错的调用一起吞掉，否则要等恢复发送才暴露。

因为静音时返回的是 `nil`，它是运维开关而不是错误注入手段：需要在静音期间感知「消息被丢弃」时，看 debug 日志或 `Stats`（两个计数都不增长）。

## 通用 webhook

`webhook` 包把任意 HTTP(S) 端点接成一个 `msgbot.Provider`，请求体由调用方决定，发送路径复用 `Config.Send`，因此重试、结构化错误、日志脱敏、`Stats` 与发送开关的行为与三个平台完全一致。请求固定为 `POST` + `application/json; charset=utf-8`。

四种消息类型被归一化成一个 `webhook.Message`，按 `Kind` 判别读哪些字段，因此只需实现一个构造函数：

```go
import "github.com/gtkit/msgbot/webhook"

hook, err := webhook.New(
    msgbot.Config{WebhookURL: "https://hooks.example.com/alert", Retry: msgbot.RetryPolicy{MaxRetries: 2}},
    func(m *webhook.Message) (any, error) {
        switch m.Kind {
        case webhook.KindText:
            return map[string]any{"text": m.Text, "at_all": m.Options.AtAll}, nil
        case webhook.KindMarkdown:
            return map[string]any{"title": m.Title, "body": m.Content}, nil
        default:
            return nil, fmt.Errorf("端点不支持 %s 消息", m.Kind)
        }
    },
)
if err != nil {
    return err
}

err = hook.SendText(ctx, "服务不可用", msgbot.WithAtAll())
```

`Kind` 与发送方法一一对应：`KindText` 读 `Text`，`KindMarkdown` 读 `Title`/`Content`，`KindRichText` 读 `RichText`，`KindImage` 读 `Image`；`Options` 始终非 nil。构造函数返回错误会使本次发送立即失败且不重试（归类为 `KindValidation`），所以对不支持的类型直接返回错误即可。

`Platform()` 返回 `msgbot.PlatformWebhook`，该平台不参与业务错误码的重试判定（通用端点没有公认的限流码语义）；HTTP 层的 408/425/429/5xx 与 `Retry-After` 照常生效。需要在一个 `Manager` 里放多个端点时，用 `NewNamedManager` 按名字注册。

## @ 提醒语义

各平台 @ 能力不同，本库按各自协议尽力实现，并对不支持的情况明确降级：

| 场景 | 飞书 | 企业微信 | 钉钉 |
|------|------|----------|------|
| 文本 `@所有人` / `@指定人` | 支持（`<at>` 标签，指定人 ID 会转义） | 支持（`mentioned_list`，`@all` 与指定人合并） | 支持（正文注入 `@手机号` + `atMobiles`） |
| Markdown `@所有人` | 支持（互动卡片 `<at id=all>`） | `@所有人` 生效、无法 @ 指定人 | 支持（正文注入 + `isAtAll`） |
| Markdown `@指定人` | 支持（卡片 `<at id=...>`） | **不支持**：静默忽略并记 debug 日志，消息照发 | 支持（正文注入 `@手机号`） |

企业微信 Markdown 无法 @ 指定人，传入该选项不会报错（保证 `Multi` 广播尽力而为），会记一条 debug 日志。需要 @ 指定人时用 `SendText`。钉钉 Markdown 的 `title` 为必填，空标题会在本地返回校验错误。

## 错误处理与重试

发送失败返回结构化 `*msgbot.Error`，可判类型、判是否可重试：

```go
err := bot.SendText(ctx, "hello")

var e *msgbot.Error
if errors.As(err, &e) {
    switch e.Kind {
    case msgbot.KindValidation: // 参数错误，不该重试
    case msgbot.KindHTTP:       // 看 e.HTTPStatus
    case msgbot.KindPlatform:   // 平台业务码 e.Code
    case msgbot.KindTransport:  // 网络错误
    }
    _ = e.Retryable // 是否值得重试
}
// 底层原因保留：errors.Is(err, context.Canceled) 等仍可穿透判断。
```

结构化错误覆盖：三平台 webhook 发送、access token 获取、飞书图片上传/下载与飞书 App 消息发送均返回 `*msgbot.Error`；少数纯本地错误（如打开文件失败）仍是普通 error。

重试默认关闭。开启后只重试瞬时错误（网络抖动、HTTP 408/425/429/5xx、平台限流码如钉钉 130101、企微 45009/45033、飞书 11232），采用指数退避 + jitter，尊重 `Retry-After` 与 `ctx` 截止时间：

```go
bot, _ := feishu.New(msgbot.Config{
    WebhookURL: "...",
    Retry:      msgbot.RetryPolicy{MaxRetries: 2, Jitter: true},
})
```

`Retry-After` 处理：服务端指定的等待时长完整生效、不被 `MaxDelay` 截断，但受 `MaxRetryAfter` 安全上限约束（默认 30s），以防调用方使用无 deadline 的 `context.Background()` 时，一个异常巨大的 `Retry-After` 造成近乎永久的阻塞；设 `MaxRetryAfter` 为负值表示完全信任服务端、仅由 `ctx` 兜底。生产环境仍**建议给 `ctx` 设置整体 deadline**。

> ⚠ **开启重试后投递语义变为 at-least-once**：首次请求可能已在平台侧成功、客户端却没收到响应，重试会产生重复消息。仅在能接受重复或下游可去重时开启。此外三平台 webhook 均有频率限制（如钉钉 20 条/分钟，超限封禁 10 分钟），高频场景请自行限流。

## 日志

`msgbot` 不依赖任何日志库，`Config.Logger` 是一个只有两个方法的最小接口。接入 `github.com/gtkit/logger` 只需几行适配：

```go
import "github.com/gtkit/logger"

type gtkitLogger struct{}

func (gtkitLogger) DebugContext(ctx context.Context, msg string, args ...any) {
    logger.DebugwCtx(ctx, msg, args...)
}
func (gtkitLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
    logger.ErrorwCtx(ctx, msg, args...)
}

bot, _ := feishu.New(msgbot.Config{WebhookURL: "...", Logger: gtkitLogger{}})
```

日志中的 webhook 路径、query、签名等敏感信息已自动脱敏，只记录 `scheme://host`。

## Web 框架集成

`msgbot` 不依赖任何 Web 框架。Provider（或 `Manager`/`Multi`）可作为普通依赖注入任意 HTTP handler——用闭包捕获或放进应用的依赖容器均可，在 handler 内调用 `Get(platform).SendText(...)` 等方法即可，无需额外适配。

## License

MIT
