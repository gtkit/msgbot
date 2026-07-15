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

| API | 说明 |
|-----|------|
| `msgbot.Provider` | 三平台通用发送接口 |
| `msgbot.NewManager` | 管理多个平台 Provider |
| `msgbot.NewMulti` | 并发广播到多个 Provider |
| `feishu.New` | 创建飞书 Webhook Provider |
| `feishu.GetAccessToken` | 获取飞书自建应用 token |
| `*.GetAccessTokenCached` | 带缓存获取 token（三平台同形态） |
| `*.NewMemoryTokenCache` | 进程内默认 token 缓存（三平台同形态） |
| `feishu.NewApp` | 创建飞书自建应用消息客户端（静态 token） |
| `feishu.NewAppWithTokenSource` | 创建可自动刷新 token 的应用消息客户端 |
| `wecom.New` | 创建企业微信 Webhook Provider |
| `dingtalk.New` | 创建钉钉 Webhook Provider |
| `msgbot.Error` / `msgbot.ErrorKind` | 结构化错误，可 `errors.As` 判类型 |
| `msgbot.RetryPolicy` | 可选重试策略（`Config.Retry`，默认关闭） |

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

`msgbot` 不依赖任何 Web 框架。在 Gin 等框架中使用时，通过闭包注入 Manager 即可：

```go
func alertHandler(mgr *msgbot.Manager) gin.HandlerFunc {
    return func(c *gin.Context) {
        _ = mgr.Default().SendText(c.Request.Context(), "告警")
        c.JSON(200, gin.H{"status": "ok"})
    }
}

mgr := msgbot.NewManager(fs, wc, dt)

r := gin.Default()
r.POST("/alert", alertHandler(mgr))
```

## License

MIT
