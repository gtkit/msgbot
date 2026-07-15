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
| `feishu.NewApp` | 创建飞书自建应用消息客户端 |
| `wecom.New` | 创建企业微信 Webhook Provider |
| `dingtalk.New` | 创建钉钉 Webhook Provider |

## 飞书

```go
fs, _ := feishu.New(msgbot.Config{WebhookURL: "...", Secret: "..."})

fs.SendText(ctx, "hello", msgbot.WithAtAll())
fs.SendMarkdown(ctx, "部署报告", "**状态**: 成功")
fs.SendRichText(ctx, feishu.BuildRichText(
    "告警",
    "服务响应超时",
    &msgbot.RichTextTag{Tag: "a", Text: "查看", Href: "https://example.com"},
    true,
))
fs.SendImage(ctx, &msgbot.ImageMessage{ImageKey: "img_xxx"})
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

## 企业微信

```go
wc, _ := wecom.New(msgbot.Config{WebhookURL: "..."})

wc.SendText(ctx, "hello", msgbot.WithAtAll())
wc.SendMarkdown(ctx, "告警", `新增反馈<font color="warning">132例</font>`)
wc.SendImageFromFile(ctx, "/path/to/alert.png")
```

## 钉钉

```go
dt, _ := dingtalk.New(msgbot.Config{WebhookURL: "...", Secret: "SEC-xxx"})

dt.SendText(ctx, "hello", msgbot.WithAtUsers("13800001111"))
dt.SendMarkdown(ctx, "杭州天气", "#### 杭州天气\n> 9度")
dt.SendLink(ctx, "标题", "正文", "https://example.com", "https://example.com/pic.png")
dt.SendActionCard(ctx, &dingtalk.ActionCard{
    Title: "CI 构建失败",
    Text:  "### 构建失败",
    Buttons: []dingtalk.Button{
        {Title: "查看日志", ActionURL: "https://ci.example.com/123"},
    },
})
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
