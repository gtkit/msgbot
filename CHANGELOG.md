# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 和 Semantic Versioning。

## [Unreleased]

## [1.3.0] - 2026-09-08

本版本对标 `github.com/nikoksr/notify` 补齐分发编排层的四项能力，全部为新增，无破坏性变更，`go.mod` 仍无任何第三方依赖。

### Added

- 新增运行期发送开关 `msgbot.Switch` 与 `Config.Switch`（为 nil 时始终启用）：关闭期间 `Send*` 直接返回 `nil`，不发出请求、不计入 `Stats`，并记一条 debug 日志。多个 provider 共享同一实例即可被一次 `Disable` 同时静音；`feishu.Webhook.SendImageFromFile` 在上传之前就检查开关，静音期间不会调用飞书上传 API。
- 新增 `msgbot.NewNamedManager` 与 `msgbot.NamedProvider` / `Named`：按名字注册 provider，使同一平台可以有多个目标（P0 群 / 值班群 / 归档群）。配套 `Manager.GetNamed` 与 `Manager.Names`；空参数、空名字、重名与 nil provider 均返回错误。具名 provider 同时进入平台索引，`Get` / `Default` 等照常可用。
- 新增 `webhook` 包与 `msgbot.PlatformWebhook`：面向任意绝对 HTTP(S) 端点的通用 `Provider`，请求体由调用方通过 `PayloadBuilder` 决定，四种消息类型归一化为带 `Kind` 判别字段的 `webhook.Message`。发送路径复用 `Config.Send`，重试、结构化错误、日志脱敏、`Stats` 与发送开关的行为与三平台一致。
- 新增 `feishu.AppConfig` 与 `feishu.NewAppWithConfig`：以结构体字面量配置飞书应用消息客户端，是唯一能配 `Switch` 发送开关的构造函数，使「一键停发」覆盖到飞书应用消息。`Token` 与 `Source` 二选一且必须恰好提供一个（都空或都给都返回错误）。静音时 App 的 `Send*` 在参数校验通过后返回 nil，不解析 token、不上传图片、不发请求；参数校验优先于静音。`NewApp` / `NewAppWithTokenSource` 的签名与行为不变，创建的 App 无开关、始终启用。
- 新增 `feishu.ReceiveIDType` 与 `App.SendTextMessageTo` / `App.SendImageMessageTo`：飞书应用消息支持 `open_id`、`user_id`、`union_id`、`email`、`chat_id` 五种收件人类型，白名单之外的取值在本地返回校验错误且不解析 token。

### Changed

- `Manager.All()` 改为按注册顺序返回副本（此前遍历 map，顺序随机），`Multi()` 因此也有了确定的发送顺序。`NewManager` 下被同平台后者覆盖掉的 provider 仍不会出现在结果中。
- `App.SendTextMessage` / `App.SendImageMessage` 改为委托到对应的 `*To` 方法，签名与语义不变；空收件人的校验文案由 `open_id is required` 改为 `receive_id is required`（错误分类仍为 `KindValidation`）。

### Fixed

- `Config.Send` 在 `BuildRequest` 返回非 `*msgbot.Error` 错误时，此前同时填充 `Message` 与 `Err`，导致 `Error()` 把同一句原因打印两遍（如 `validation: generate sign: xxx: generate sign: xxx`）。现在只保留被包裹的原因。
- `internal.PostJSON` 在 client 为 nil 时不再回退到无超时的 `http.DefaultClient`，改为带 10s 超时的共享 client。此前该分支与库内三处「绝不落到无超时 client」的声明相矛盾；当前所有调用方都传入非 nil client，故无行为变化。

## [1.2.0] - 2026-07-15

### Changed

- 内部 JSON 序列化/反序列化改用标准库 `encoding/json`，保持既有线上格式（不转义 HTML、无结尾换行，飞书 `<at>` 等特殊字符行为不变），公开 API 与消息 wire 格式均无变化。
- 移除第三方 JSON 库依赖及其全部传递依赖，`go.mod` 不再引入任何外部依赖。

## [1.1.0] - 2026-07-15

> **本版本包含明知的破坏性变更**（见下方 BREAKING）；因本库仍处早期、v1.0.0 发布未久且外部使用者极少，团队决定以 minor 号发布并在此显著标注，而非升 `/v2`。重试默认关闭，保持既有发送语义。

### BREAKING

- 移除 `msgbot.SlogLogger`（v1.0.0 曾导出）。`Config.Logger` 仍是与日志库无关的最小接口；接入方式见 README 的 `github.com/gtkit/logger` 适配示例。**迁移**：删除对 `SlogLogger` 的引用，自行实现 `Logger` 的两个方法（几行适配代码）。
- `Manager.SetDefault` 签名由 `func(Platform)` 改为 `func(Platform) error`，对未注册平台返回错误并保持原默认不变（此前静默设置会导致 `Default()` 返回 nil）。常见的 `mgr.SetDefault(p)` 语句调用仍可编译；仅方法值、接口匹配或函数类型赋值需调整。

### Added

- 统一三平台 `@` 提醒语义：飞书文本转义被 @ 的用户 ID、Markdown 卡片改用 `<at id=...>` 语法；企业微信文本合并 `@all` 与指定人；钉钉文本/Markdown 将 `@手机号` 注入正文并保留 `atMobiles`。
- 新增结构化错误 `msgbot.Error` 与 `msgbot.ErrorKind`（validation/transport/http/platform/decode），可 `errors.As` 判类型、`errors.Is` 穿透底层原因，并标注是否可重试。覆盖三平台 webhook 发送、token 获取、飞书图片上传/下载与飞书 App 消息发送。
- 新增可选重试 `msgbot.RetryPolicy`（`Config.Retry`，默认 `MaxRetries: 0` 关闭）：仅重试瞬时错误、HTTP 408/425/429/5xx 及平台限流码（钉钉 130101、企业微信 45009/45033、飞书 11232），指数退避 + jitter，尊重 `Retry-After` 与 `ctx`。开启后投递为 at-least-once，可能产生重复消息。
- `RetryPolicy` 新增 `MaxRetryAfter` 字段（默认 30s）：服务端 `Retry-After` 的安全上限，防止调用方使用无 deadline 的 context 时因异常巨大的 `Retry-After` 造成近乎永久阻塞；设为负值表示不设上限。
- 新增 `feishu.TokenSource` 与 `feishu.NewAppWithTokenSource`，应用消息客户端可自动刷新过期 token；单次 `SendImageMessage` 内上传与发送使用同一 token。
- 非 webhook API（token、应用消息、图片上传/下载）新增分级默认超时：token/消息 10s、图片 30s；调用方自带 client 或 ctx 截止时间优先。
- 新增面向自定义 Provider 的扩展 API：`Config.Send`、`BuildRequest` 及错误构造器 `WrapError`/`PlatformError`/`DecodeError`/`ValidationError`，含文档与 Example。

### Changed

- 钉钉签名 URL 改用 `net/url` 处理 query，无论原 URL 是否带 query 都能生成合法且签名正确的地址。
- 三平台 webhook 发送收敛到统一发送路径（`Config.Send`），集中处理重试、结构化错误、统计与日志脱敏。
- 飞书应用消息发送复用 `internal.ReadResponse`，去除重复的响应读取与状态码判断逻辑。
- 重试完整尊重服务端 `Retry-After`（不再被 `MaxDelay` 截断，总时长由 `ctx` 兜底）；退避等待期间 `ctx` 结束时返回 `errors.Join(最后错误, ctx.Err())`，可被 `errors.Is(context.Canceled/DeadlineExceeded)` 命中。
- `Config.Send` 容忍 nil `Stats`（不再 panic）；`Config.GetHTTPClient` 在未 `Freeze` 时兜底返回带超时的 client，杜绝无超时的 `http.DefaultClient` 路径。
- 图片发送增加平台大小上限：飞书上传经 `io.LimitReader` 限制 10MB，企业微信读文件前校验 2MB，超限本地拒绝。
- `internal.HTTPError.Error()` 截断响应体（上限 512 字节，按 UTF-8 rune 边界），避免超大或含无关内容的上游响应体被完整拼入错误串与日志。
- 三平台面向用户的参数/空值校验统一为结构化 `KindValidation` 错误（App 收发、图片上传/下载入口等）；纯内部运行时错误（marshal、建请求、文件 IO）仍为普通 error。
- 全部代码注释统一为简体中文；三平台包内 `news "github.com/gtkit/msgbot"` 历史别名统一改为自然的 `msgbot`。

## [1.0.1] - 未发布

### Fixed

- 加强访问令牌安全与错误处理（对应 `fix(auth)`，已在 main 分支、尚未打 tag）。

## [1.0.0] - 2026-06-12

### Added

- 新增飞书、企业微信、钉钉三平台机器人消息发送能力。
- 新增通用 `Provider`、`Manager` 和 `Multi` 广播能力。
- 新增飞书自建应用 access token、图片上传/下载、按 `open_id` 发送文本和图片消息能力。
- 新增三平台统一的 access token 缓存：`TokenCache` 接口、`GetAccessTokenCached`、`NewMemoryTokenCache`（缓存故障自动降级直连，同凭证并发去重防击穿）。
