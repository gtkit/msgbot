# msgbot 架构文档

> 本文档为项目内部参考资料，**不纳入版本控制**（通过 `.git/info/exclude` 本地排除）。
> 描述 `github.com/gtkit/msgbot` 的目录结构、整体架构与关键流程时序。
> 面向维护者，随代码演进可自行更新。

`msgbot` 是多平台 IM 机器人消息推送 Go 库，支持飞书（Feishu）、企业微信（WeCom）、钉钉（DingTalk）。核心设计：一个平台无关的根包定义统一契约与共享能力，三个平台子包实现各自协议，`internal` 承载不对外暴露的实现细节。

---

## 一、目录结构与职责

```
github.com/gtkit/msgbot
├── provider.go            根包核心：Provider 接口、Platform、Config、SendOption、Response、Stats、Logger
├── manager.go             Multi（并发广播）与 Manager（多平台注册/默认选择）
├── dispatch.go            Config.Send —— 三平台 webhook 共用的统一发送路径（Provider 扩展 API）
├── retry.go               RetryPolicy —— 可选重试（默认关闭），指数退避 + jitter + Retry-After
├── errors.go              结构化错误 Error / ErrorKind，及分类器与限流码表
├── richtext.go            RichTextToMarkdown —— 富文本向 markdown 的降级转换
├── version.go             版本常量
│
├── feishu/                飞书子包
│   ├── webhook.go         Webhook Provider（text / markdown 卡片 / post 富文本 / image）+ @ 语义
│   ├── app.go             自建应用消息客户端 App（TokenSource 可刷新 token）
│   ├── auth.go            GetAccessToken、AccessToken、DownloadImage
│   ├── image.go           图片上传（含 10MB 上限）
│   └── tokencache.go      TokenCache 接口、MemoryTokenCache、GetAccessTokenCached
│
├── wecom/                 企业微信子包（webhook.go / auth.go / image.go / tokencache.go）
│   └── image.go           base64+md5 图片（含 2MB 上限）
│
├── dingtalk/              钉钉子包（webhook.go / auth.go / image.go / tokencache.go）
│   └── webhook.go         额外支持 Link / ActionCard / FeedCard；签名走 URL query
│
└── internal/             不对外暴露的实现细节
    ├── http.go            PostJSON、ReadResponse、结构化 HTTPError（body 截断）
    ├── client.go          分级默认 HTTP 客户端（消息 10s / 图片 30s）、PickClient
    ├── sign.go            FeishuSign、DingTalkSignedURL（net/url 处理 query）
    ├── url.go             URL 校验与日志脱敏（URLOriginForLog、SanitizeRequestError）
    └── tokencache/        token 缓存泛型核心
        ├── tokencache.go  Fetch —— 读穿透 + TTL 分段 + 故障降级
        ├── group.go       Group —— in-flight 去重（防冷启动击穿）
        └── memory.go      Memory —— 基于 atomic.Pointer 的零锁进程内缓存
```

### 分层职责

| 层 | 位置 | 职责 |
|---|---|---|
| 契约层 | `provider.go` | 定义 `Provider` 统一接口、`Config`、发送选项、结构化错误载体 |
| 编排层 | `manager.go`、`dispatch.go`、`retry.go` | 多平台注册与广播、统一发送路径、重试策略 |
| 平台层 | `feishu/`、`wecom/`、`dingtalk/` | 各平台协议实现、消息类型、@ 语义、token 与图片 |
| 基础层 | `internal/` | HTTP 收发、签名、URL 脱敏、token 缓存泛型核心 |

依赖方向自上而下单向：平台层依赖契约层与基础层；基础层不反向依赖任何上层；`internal/tokencache` 泛型核心不感知具体平台类型。

---

## 二、整体架构图

```mermaid
graph TD
    User["调用方业务代码"]

    subgraph Root["根包 msgbot（契约 + 编排）"]
        Provider["Provider 接口"]
        Manager["Manager / Multi"]
        Config["Config（含 Retry）"]
        Send["Config.Send 统一发送路径"]
        Retry["RetryPolicy 重试"]
        Errors["Error / ErrorKind 结构化错误"]
    end

    subgraph Platforms["平台子包"]
        Feishu["feishu.Webhook / App"]
        WeCom["wecom.Webhook"]
        DingTalk["dingtalk.Webhook"]
    end

    subgraph Internal["internal（基础设施）"]
        HTTP["http: PostJSON / ReadResponse / HTTPError"]
        Client["client: 分级默认客户端"]
        Sign["sign: 飞书/钉钉签名"]
        URLpkg["url: 校验 + 日志脱敏"]
        TokenCore["tokencache: Fetch / Group / Memory"]
    end

    IM["飞书 / 企业微信 / 钉钉 开放平台 API"]

    User -->|"SendText / SendMarkdown / ..."| Manager
    User --> Feishu
    User --> WeCom
    User --> DingTalk
    Manager --> Provider
    Feishu -.实现.-> Provider
    WeCom -.实现.-> Provider
    DingTalk -.实现.-> Provider

    Feishu --> Send
    WeCom --> Send
    DingTalk --> Send
    Send --> Retry
    Send --> HTTP
    Send --> Errors
    DingTalk --> Sign
    Feishu --> Sign

    Feishu --> TokenCore
    WeCom --> TokenCore
    DingTalk --> TokenCore

    HTTP --> Client
    HTTP --> URLpkg
    Send --> IM
    HTTP --> IM
```

要点：

- 三平台的 webhook 发送最终都汇聚到 `Config.Send`，由它统一处理**重试、结构化错误分类、统计、日志脱敏**，避免各平台重复实现。
- `Config.Send` 每次尝试通过 `BuildRequest` 回调重新构造请求，使时间敏感的签名（钉钉时间戳、飞书 sign）在每次重试时刷新。
- token 缓存的编排逻辑集中在 `internal/tokencache`，三平台仅提供各自的 `AccessToken` 类型与获取函数。

---

## 三、关键流程时序图

### 3.1 Webhook 发送（含重试与结构化错误）

```mermaid
sequenceDiagram
    participant C as 调用方
    participant W as 平台 Webhook
    participant S as Config.Send
    participant R as RetryPolicy
    participant H as internal.PostJSON
    participant API as 平台 API

    C->>W: SendText / SendMarkdown(ctx, ...)
    W->>W: 校验参数、解析 SendOption、拼 @ 语义
    W->>S: Send(ctx, stats, platform, op, build)
    loop 每次尝试（默认不重试）
        S->>W: build() 构造 URL + payload（重签名）
        S->>H: PostJSON(ctx, client, url, body)
        H->>API: HTTP POST
        API-->>H: 状态码 + 响应体
        H-->>S: data 或 *HTTPError
        alt 成功且业务码为 0
            S-->>W: nil
        else 可重试失败（瞬时/5xx/限流码）
            S->>R: 计算退避（指数或 Retry-After，受 MaxRetryAfter 约束）
            R-->>S: 等待后重试；ctx 到期则终止
        else 不可重试（校验/4xx/解码）
            S-->>W: *msgbot.Error（分类 + 保留 cause）
        end
    end
    W-->>C: nil 或 *msgbot.Error
```

### 3.2 带缓存获取 access token（读穿透 + 并发去重）

```mermaid
sequenceDiagram
    participant C1 as 调用方 A
    participant C2 as 调用方 B（同凭证并发）
    participant F as tokencache.Fetch
    participant Cache as TokenCache（内存/Redis）
    participant G as Group（in-flight 去重）
    participant API as 平台 gettoken API

    C1->>F: GetAccessTokenCached(ctx, id, secret, cache)
    F->>Cache: Get(ctx)
    alt 缓存命中且未过期
        Cache-->>F: token
        F-->>C1: token（不发上游）
    else 未命中或缓存故障（降级）
        F->>G: Do(key, fetchFn)
        Note over G: 同凭证并发只放行一个发起者
        C2->>F: 同凭证并发请求
        F->>G: Do(same key)
        G-->>C2: 等待发起者结果（共享）
        G->>API: 仅一次 HTTP 请求
        API-->>G: token + 有效期
        G->>Cache: Set(ctx, token, TTL)（提前量分段）
        G-->>F: token
        F-->>C1: token
        F-->>C2: 同一 token
    end
```

### 3.3 飞书自建应用发送图片消息（单操作单 token）

```mermaid
sequenceDiagram
    participant C as 调用方
    participant A as feishu.App
    participant TS as TokenSource
    participant U as UploadImageFromFile
    participant API as 飞书开放平台

    C->>A: SendImageMessage(ctx, openID, path)
    A->>A: 校验 openID / path
    A->>TS: 解析 token（整个操作仅一次）
    TS-->>A: AccessToken
    A->>U: 上传图片（用该 token，30s 客户端，10MB 上限）
    U->>API: POST /im/v1/images
    API-->>U: image_key
    U-->>A: image_key
    A->>API: POST /im/v1/messages（用同一 token）
    API-->>A: 结果
    A-->>C: nil 或 *msgbot.Error
    Note over A: 上传与发送共用同一 token，绝不中途刷新导致两次请求 token 不一致
```

### 3.4 多平台广播（Multi 尽力而为）

```mermaid
sequenceDiagram
    participant C as 调用方
    participant M as Multi
    participant P1 as feishu
    participant P2 as wecom
    participant P3 as dingtalk

    C->>M: SendText(ctx, text, opts...)
    par 并发分发
        M->>P1: SendText
        M->>P2: SendText
        M->>P3: SendText
    end
    P1-->>M: nil
    P2-->>M: *msgbot.Error（如失败）
    P3-->>M: nil
    M-->>C: errors.Join(各平台错误)
    Note over M: 单 provider 时走快路径，无 goroutine 开销；<br/>部分失败不影响其余平台，聚合返回
```

---

## 四、几个设计约束（阅读代码前须知）

- **重试默认关闭**：`RetryPolicy` 零值不重试，保持单次发送语义；开启后投递为 at-least-once，可能重复。
- **@ 语义按平台差异实现**：飞书文本 `<at user_id>`、卡片 `<at id>`；企微 `mentioned_list`（markdown 无法 @ 指定人，静默降级）；钉钉正文注入 `@手机号` + `atMobiles`。
- **结构化错误覆盖**：webhook 发送、token 获取、飞书图片上传/下载与 App 消息返回 `*msgbot.Error`；纯本地文件/系统错误仍为普通 error。
- **日志脱敏**：webhook 路径/query/签名不入日志，仅记 `scheme://host`；`HTTPError` 响应体在错误串中截断。
- **并发安全**：Provider 构造后不可变；`Manager` 默认平台经 atomic 可变；`TokenSource` 要求实现方并发安全。
- **依赖**：JSON 用 `github.com/gtkit/json/v2`；日志不绑定具体库（`Config.Logger` 为最小接口）。
