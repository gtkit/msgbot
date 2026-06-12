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
| `feishu.NewApp` | 创建飞书自建应用消息客户端 |
| `wecom.New` | 创建企业微信 Webhook Provider |
| `dingtalk.New` | 创建钉钉 Webhook Provider |
| `ginews.Middleware` | 将 Manager 注入 Gin context |

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

调用方应按 `token.Expire` 缓存 access token。

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

## 多平台广播

```go
multi, _ := msgbot.NewMulti(fs, wc, dt)
err := multi.SendText(ctx, "全平台通知", msgbot.WithAtAll())
```

## Gin 集成

```go
mgr := msgbot.NewManager(fs, wc, dt)

r := gin.Default()
r.Use(ginews.Middleware(mgr))

r.POST("/alert", func(c *gin.Context) {
    mgr := ginews.MustFrom(c)
    _ = mgr.Default().SendText(c.Request.Context(), "告警")
    c.JSON(200, gin.H{"status": "ok"})
})
```

## 从 `github.com/gtkit/news/v2` 迁移

| news/v2 | msgbot |
|---------|--------|
| `github.com/gtkit/news/v2` | `github.com/gtkit/msgbot` |
| `github.com/gtkit/news/v2/feishu` | `github.com/gtkit/msgbot/feishu` |
| `github.com/gtkit/news/v2/wecom` | `github.com/gtkit/msgbot/wecom` |
| `github.com/gtkit/news/v2/dingtalk` | `github.com/gtkit/msgbot/dingtalk` |
| `github.com/gtkit/news/v2/ginews` | `github.com/gtkit/msgbot/ginews` |

`msgbot` 作为新模块 v1 发布，module path 不带 `/v2`。

## License

MIT
