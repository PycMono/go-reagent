# 多模态支持设计（一期：用户 URL 图片输入）

本文档定义一期多模态方案：**只做"用户发图片 URL → 模型看图 → 历史可回放"的端到端闭环**。工具结果带图、base64、MIME、图片下载代理均不在本期范围（见 §2）。

## 1. 目标与非目标

### 1.1 目标

1. `ai.ContentBlock` 支持 URL 图像块，保持块序列语义（`[]ContentBlock` 保序）。
2. 仅 `RoleUser` 消息可携带图像；Anthropic / OpenAI 适配器均能映射。
3. 打通真实入口：Web DTO → chat service → conversation runner → `pi.Message` → 模型。
4. 非视觉模型自动降级：图像块替换为不含查询参数的占位文本，不报错、不中断 Run。
5. 持久化与 Web 展示用户图片 URL；历史回放还原图像块。
6. 图像按固定 token 常量参与计量。

### 1.2 非目标（一期明确不做）

- **工具结果带图**：仓库目前没有任何图片工具输出链路；MCP proxy 遇到非 text 内容本就直接报错（`pi/mcp/tool.go:53`），维持现状。由此不引入 base64、OpenAI tool 消息合成 user 消息、8MB 限制、工具输出大小/裁剪/循环检测的图像适配（`pi/toolexec/runtime.go`、`pi/middleware/tracing.go`、`pi/loopdetect/fingerprint.go` 均只看 Text，工具结果保持纯文本后无需改动）。
- **base64 图像**：入口与持久化只接受/存储 URL，避免 `agent_messages.payload` 膨胀。
- **MIME 类型**：URL 图像在两套协议中都不要求（Anthropic `URLImageSourceParam` 只有 URL，OpenAI `image_url` 同理），一期 `ImageContent` 不保存 MIME。
- **图像下载/转存服务**。
- **assistant 图像输出**：模型响应侧维持纯文本契约，`TextContent()` 语义不变（遇非文本块报错），`ValidateThinking` / `ValidateAction` 不动。
- 音频走 ASR 旁路转写（应用层，pi 零改动）；PDF / 视频 / 工具图片二期再议。

### 1.3 前置依赖

调用方必须提供**推理服务商可访问、生命周期足够长**的图片 URL。URL 过期、需要登录或服务商无权拉取时，该图对模型不可见（视觉平台会收到拉取失败，非视觉平台只看到占位文本）。仓库不提供文件存储服务，这是使用方的责任。

## 2. 数据模型（pi/ai/content.go）

```go
const (
    ContentTypeText  ContentType = "text"
    ContentTypeImage ContentType = "image"
)

// ContentBlock 表示消息中的一个内容块。
type ContentBlock struct {
    Type  ContentType   `json:"type"`
    Text  string        `json:"text,omitempty"`
    Image *ImageContent `json:"image,omitempty"`
}

// ImageContent 表示一个 URL 图像内容。
type ImageContent struct {
    URL string `json:"url"`
}
```

- 新增构造器 `ImageBlock(url string) ContentBlock` 与统一的 `ContentBlock.Validate()` 校验函数，**同时约束联合类型的取值**（防止手写 `ai.Message` 产生歧义数据或 nil 解引用）：
  - `Type == ContentTypeText`：必须 `Image == nil`；
  - `Type == ContentTypeImage`：必须 `Text == ""`、`Image != nil` 且 `Image.URL` 为 http/https URL；
  - 其他 `Type`：报错。
- 校验的调用边界（复用同一函数，不各写各的）：`Message2AI`（含历史回放）、conversation runner 输入校验、Web DTO 校验。`normalizeMessages` 边界再做一次防御性校验（报错，不修复）。
- `TextContent()` 保持现有语义不变：其调用点全部在 assistant 侧与文本提取侧，遇图像块继续报错。
- `pi/harness/prune.go` 的 `isPrunedMarker`、`contentBytesOf` 不需要改动：一期工具结果仍是纯文本。

## 3. 归一化层（pi/ai/providers/message.go）

`normalizedMessage.text string` → `blocks []ai.ContentBlock`，归一化层保序透传，协议映射下沉到适配器。

```go
type normalizedMessage struct {
    role       ai.Role
    blocks     []ai.ContentBlock
    toolCalls  []normalizedToolCall
    toolCallID string
    isError    bool
}
```

能力归属 Provider，不随消息复制：两个 Provider 结构体各自持有 `vision bool`（构造时取自 `Options.Vision`），归一化以参数传入：

```go
// AnthropicImpl / OpenAIImpl
type AnthropicImpl struct {
    // ...
    vision bool // 来自 Options.Vision
}

func normalizeMessages(messages []ai.Message, vision bool) ([]normalizedMessage, error)
```

role 契约（fail-fast，不做静默降级）：

- `RoleUser`：允许 text + image 混排（Provider 层保序映射，不限制块顺序；业务链路的规范形态约束见 §5.3）。
- `RoleSystem` / `RoleAssistant` / `RoleTool`：出现 image 块直接报错（assistant/tool 的图像是一期不存在的路径，出现即上游契约破坏）。
- 全部块先过 `ContentBlock.Validate()` 防御性校验。

降级（唯一收敛点）：`vision=false` 时，user 消息中的 image 块替换为占位文本 `[图片: <host><path>]`。URL 只保留 scheme、host 与 path，**剥离查询参数与片段**，避免签名、临时 Token 泄漏到模型上下文。占位文本必须非空（path 为空时用 host）。

## 4. 协议映射

### 4.1 Anthropic（pi/ai/providers/anthropic.go）

user 消息 text 块照旧 `NewTextBlock`；image 块映射（SDK v1.61.0 实际类型名）：

```go
anthropicsdk.NewImageBlock(anthropicsdk.URLImageSourceParam{URL: block.Image.URL})
```

`finish()` 出向映射不变：assistant 响应只产出 text / tool_use 块。

### 4.2 OpenAI（pi/ai/providers/openai.go）

user 消息从 `UserMessage(text)` 改为统一 content parts 数组（text + image_url 有序混排，纯文本也是单 text part）：

```json
[
  {"type": "text", "text": "..."},
  {"type": "image_url", "image_url": {"url": "https://..."}}
]
```

工具结果保持纯文本 `ToolMessage(text, id)`，无合成消息、无投影技巧。

## 5. 能力开关与入口

### 5.1 平台配置

`providers.Options` 增加 `Vision bool`（默认零值 false = 降级模式）；config 平台字段增加 `"vision"`，**默认 false**（与默认平台 deepseek 不支持视觉一致）。校验只在 config 包 Load 时 fail-fast（布尔类型由 JSON 反序列化保证），不向 pi 运行时追加校验。

### 5.2 入口消息（pi/message.go）

`pi.Message` 增加 `ImageURLs []string` 字段（`json:"image_urls,omitempty"`）；`ContentType` 维持只有 `"text"`：

- `Message2AI`：`ContentType: "text"` + `ImageURLs` 时，产出 user 消息 `[TextBlock(content), ImageBlock(url)...]`。校验：每个 URL http/https；`ContentType != "text"` 仍 fail-fast。
- v1 规则：**正文必填，图片可选附加**，不支持纯图片消息（与 Web DTO `content` binding:"required" 一致）。
- 现有 `FileURL` 字段语义不变（仍不发送给模型），不复用。

### 5.3 Web 链路（一期闭环的关键，此前方案缺失）

| 层 | 改动 |
|---|---|
| DTO（`common/dto/chat.go`） | `StartRunDTO` 增加 `ImageURL string \`json:"image_url,omitempty"\`，binding 校验 `omitempty,http_url`（注意：validator 的 `url` 规则接受 FTP/file 等 scheme，必须用 `http_url`） |
| chat service（`run_manager.go`） | 校验非空 ImageURL 后构造 `ai.Message{Role: RoleUser, Content: [TextBlock(content), ImageBlock(url)]}` 传入 `conversation.RunRequest` |
| conversation runner（`runner.go`） | `validateRunRequest` 按规范形态校验输入（见下），runtime 输入改为 `pi.Message{ContentType: "text", Content: runtimeInputText, ImageURLs: [url]}` 透传图像 |
| 前端 | 发送消息时附图片 URL 输入（粘贴链接）；消息气泡渲染 `<img loading="lazy" referrerpolicy="no-referrer" alt="用户图片">`，加载失败显示占位框——`no-referrer` 避免把当前页面地址通过 Referer 发给图片服务 |

**规范形态契约**：conversation 业务链路的 user 消息只允许"一个非空 text 块 + 0..N 个 image 块（正文在前、图片在后）"。runner 对其他形态（无正文、交错混排、image 在前）fail-fast。Provider 归一化层保持保序映射不限制顺序，规范形态只是业务链路的入口约束，不阻碍未来扩展。历史回放（§7.2）在同一契约下无损。

## 6. 计量与压缩（pi/harness/meter.go）

### 6.1 统一计量口径 — 图像权重必须落在 VisibleMessagesBytes

现状 `VisibleMessagesBytes` 只拼接 text 块，而它的调用方有三处，口径必须一致：

1. `TokenMeter.Estimate`（`meter.go:46`）— 上下文压力估算；
2. `BuildCompactionPlan`（`harness/compaction.go:151,160`）— 压缩范围选择的单位字节；
3. 压缩收敛契约检查（`pi/compaction.go:321`）— "摘要必须小于被替换范围"。

若只在 `Estimate()` 中单独给图像加权重，而范围选择仍按几十字节的占位符计，会出现"计量判定图像导致压力过高、压缩规划却认为这些消息不值得压缩"的两套口径。

**统一方式**：`VisibleMessagesBytes` 遍历块时，每遇到一个 image 块额外累加 `DefaultImageTokens * bytesPerTokenHeuristic`（1024 × 4 = 4096 虚拟字节）。这样：

- `Estimate = total / 4` 公式不变，天然包含每图 1024 token；
- 压缩范围选择与收敛检查共用同一权重——含图消息在范围选择中显得"够大"，会被优先纳入压缩；
- `MarshalVisibleMessages` 仍只输出短占位文本 `[图片: <host><path>]`（追加到投影 Text），摘要输入"看得见"图像存在，但绝不输出虚拟字节。

`DefaultImageTokens = 1024` 为 harness 内部稳定默认常量，不暴露配置：无像素尺寸信息时按字节换算会严重高估，1024 为量级正确的近似；reactive overflow 兜底仍生效。

### 6.2 压缩后图片语义丢失 — 已知限制，明确接受

压缩摘要模型只能看到 `[图片: host/path]` 占位文本，看不到图像内容；原始消息被摘要替换后，后续模型**也失去该图**。这是主动选择的取舍：图像本体不进入摘要（无法廉价地在摘要输入中携带），旧图随摘要一起失效。文档口径：**占位符只是让摘要"知道曾有图"，不保留语义**。若未来需要，可做"含图消息不参与摘要"的策略，属二期。

## 7. 持久化与展示

### 7.1 领域模型（domain/entity/conversation/message_payload.go）

平行扩展，仅 URL：

```go
const ContentTypeImage ContentType = "image"

type ContentBlock struct {
    Type  ContentType   `json:"type"`
    Text  string        `json:"text,omitempty"`
    Image *ImageContent `json:"image,omitempty"` // ImageContent{URL string}
}
```

`Value()/Scan()` 走 JSON，旧 payload 无 `image` 字段反序列化零影响，**不需要数据迁移**。

### 7.2 映射与回放（conversation/mapper.go）

- `messagesToDomain`：补 `Image.URL` 复制。
- `messagesToHistory` / `historyTextContent`：删除对非 text 块的报错；text 块拼接为 `Content`，image 块收集为同一条 `pi.Message` 的 `ImageURLs`。**不拆分消息**。
- 回放顺序契约：持久化 payload 遵守 §5.3 规范形态（text 在前、image 在后），"拼接全部文本 + 收集全部图片"的还原方式在该契约下**无损**——还原结果 `Content + ImageURLs` 与原始块顺序一致。对违反规范形态的存量/手写数据（如 `text → image → text` 交错），回放产物会改变块顺序，因此 `messagesToHistory` 对非规范形态 fail-fast，由 runner 报错，不做静默重排。

### 7.3 Web VO（common/vo/chat.go + application/service/chat）

`ContentBlockVO` 增加 `Image *ImageContentVO`（仅 URL）；`service.go:229`、`listener.go:85,101` 三处映射补齐。SSE 无新增事件类型（assistant 流式输出仍只有 text delta）。

## 8. 观测与安全

- 占位文本与所有日志只记录 scheme://host/path，**剥离查询参数与片段**（防签名/Token 泄漏）。
- observability content 记录（`content.mode`）不新增图像元数据；图像块不出现在任何观测内容中。
- URL 生命周期风险见 §1.3。

## 9. 实施顺序

| 步骤 | 内容 | 涉及文件 |
|---|---|---|
| 1 | 数据模型：URL 图像块 + 校验 + 构造器 | `pi/ai/content.go` |
| 2 | 平台能力：`Options.Vision` + config `vision` 字段 + config.example.json | `pi/ai/providers/options.go`、`config/{config,platform}.go`、`config.example.json` |
| 3 | 归一化层：`text → blocks`、role 契约、降级占位符 | `pi/ai/providers/message.go` |
| 4 | Anthropic 映射 | `pi/ai/providers/anthropic.go` |
| 5 | OpenAI 映射 | `pi/ai/providers/openai.go` |
| 6 | 计量与压缩投影 | `pi/harness/meter.go` |
| 7 | pi 入口：`pi.Message.ImageURLs` | `pi/message.go` |
| 8 | Web 链路闭环：DTO → service → runner | `common/dto/chat.go`、`application/service/chat/run_manager.go`、`conversation/runner.go` |
| 9 | 持久化与回放 | `domain/entity/conversation/message_payload.go`、`conversation/mapper.go` |
| 10 | Web VO 与前端展示 | `common/vo/chat.go`、`application/service/chat/{service,listener}.go`、`frontend/` |

每步前缀的验证口径：

- 步骤 1–5 完成 → **Provider 映射验证**：出向请求快照测试覆盖视觉/降级两分支（此时业务入口尚未改造，无法人工发图）。
- 步骤 7 完成 → **SDK 链路验证**：可通过 `pi.Message`（含 `ImageURLs`）做 SDK 级端到端。
- 步骤 8 完成 → **Web 链路验证**：Web API 真实入口发图可走通（页面 UI 到步骤 10 才可操作，此前用 API 调用验证）。
- 步骤 9–10 → **Web 完整闭环**：回放与前端展示补齐。

## 10. 测试计划

- `pi/ai`：`ImageBlock` 构造与 `ContentBlock.Validate()` 联合约束（text 带图、image 带文本、未知 Type、URL scheme、空 URL）。
- `config` → `providers` 接线：config 声明 `vision=true/false` 后 `Options.Vision` 真正传到两个 Provider 实例并生效（防止字段加了但没接）。
- `pi/ai/providers`：归一化保序；vision=false 占位符（URL 脱敏：查询参数被剥离）；system/assistant/tool 图像报错；Anthropic / OpenAI 出向请求快照；扩展现有 `normalization_test.go`。
- `pi/harness`：`VisibleMessagesBytes` 对 image 块的 4096 虚拟字节（Estimate、压缩范围选择、收敛检查三处共用同一口径）；`MarshalVisibleMessages` 只输出短占位符；prune 行为不变（工具结果仍纯文本）。
- `conversation`：runner 规范形态校验（无正文、交错混排、image 在前均 fail-fast）；含图消息持久化往返；历史回放还原 `ImageURLs` 不报错；非规范形态存量数据回放报错。
- `application/service/chat`：DTO 校验（FTP/file scheme、非法 URL 拒绝）；run_manager 构造含图 `ai.Message`。
- 前端：发送携带图片 URL、气泡渲染 `<img>`（`no-referrer`）、加载失败回退占位框。
- 端到端：vision=true 平台 Web 发图 URL → 模型描述图像；vision=false 平台发图 → Run 正常完成且上下文含脱敏占位符；含图历史触发压缩 → 摘要后占位符保留、图像语义丢失（§6.2 既有行为）。