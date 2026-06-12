# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 和 Semantic Versioning。

## [Unreleased]

### Added

- 新增飞书、企业微信、钉钉三平台机器人消息发送能力。
- 新增通用 `Provider`、`Manager` 和 `Multi` 广播能力。
- 新增飞书自建应用 access token、图片上传/下载、按 `open_id` 发送文本和图片消息能力。
- 新增三平台统一的 access token 缓存：`TokenCache` 接口、`GetAccessTokenCached`、`NewMemoryTokenCache`（缓存故障自动降级直连，同凭证并发去重防击穿）。
