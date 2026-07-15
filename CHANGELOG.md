# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 和 Semantic Versioning。

## [Unreleased]

> 以下为面向下一个 **minor（v1.1.0）** 的行为增强与修正，均为新增能力或缺陷修复，重试默认关闭以保持既有发送语义。

### Added

- 统一三平台 `@` 提醒语义：飞书文本转义被 @ 的用户 ID、Markdown 卡片改用 `<at id=...>` 语法；企业微信文本合并 `@all` 与指定人；钉钉文本/Markdown 将 `@手机号` 注入正文并保留 `atMobiles`。
- 新增结构化错误 `msgbot.Error` 与 `msgbot.ErrorKind`（validation/transport/http/platform/decode），可 `errors.As` 判类型、`errors.Is` 穿透底层原因，并标注是否可重试。
- 新增可选重试 `msgbot.RetryPolicy`（`Config.Retry`，默认 `MaxRetries: 0` 关闭）：仅重试瞬时错误、HTTP 408/425/429/5xx 及平台限流码（钉钉 130101、企业微信 45009/45033、飞书 11232），指数退避 + jitter，尊重 `Retry-After` 与 `ctx`。开启后投递为 at-least-once，可能产生重复消息。
- 新增 `feishu.TokenSource` 与 `feishu.NewAppWithTokenSource`，应用消息客户端可自动刷新过期 token；单次 `SendImageMessage` 内上传与发送使用同一 token。
- 非 webhook API（token、应用消息、图片上传/下载）新增分级默认超时：token/消息 10s、图片 30s；调用方自带 client 或 ctx 截止时间优先。

### Changed

- **BREAKING（低影响）**：`Manager.SetDefault` 改为返回 `error`，对未注册平台返回错误并保持原默认不变（此前静默设置会导致 `Default()` 返回 nil）。常见的 `mgr.SetDefault(p)` 语句调用形式仍可编译。
- 钉钉签名 URL 改用 `net/url` 处理 query，无论原 URL 是否带 query 都能生成合法且签名正确的地址。
- 三平台 webhook 发送收敛到统一发送路径（`Config.Send`），集中处理重试、结构化错误、统计与日志脱敏。
- 飞书应用消息发送复用 `internal.ReadResponse`，去除重复的响应读取与状态码判断逻辑。

### Removed

- 移除 `msgbot.SlogLogger`：`Config.Logger` 仍是与日志库无关的最小接口，README 提供 `github.com/gtkit/logger` 适配示例。

## [1.0.1] - 未发布

### Fixed

- 加强访问令牌安全与错误处理（对应 `fix(auth)`，已在 main 分支、尚未打 tag）。

## [1.0.0] - 2026-06-12

### Added

- 新增飞书、企业微信、钉钉三平台机器人消息发送能力。
- 新增通用 `Provider`、`Manager` 和 `Multi` 广播能力。
- 新增飞书自建应用 access token、图片上传/下载、按 `open_id` 发送文本和图片消息能力。
- 新增三平台统一的 access token 缓存：`TokenCache` 接口、`GetAccessTokenCached`、`NewMemoryTokenCache`（缓存故障自动降级直连，同凭证并发去重防击穿）。
