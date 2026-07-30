# go-reagent

`go-reagent` 是一个用 Go 编写的企业级 Agent Harness 内核，用于统一承载大模型推理、工具调用与 Agent 生命周期。

项目目前处于最小核心搭建阶段：公共协议、可测试的 ReAct Main Loop、配置驱动的 OpenAI/Anthropic 兼容适配器、真实 Tool Registry，以及受限的 `read_file` 和 `edit_file` 工具已经就位。

## 核心流程

```text
用户消息
   │
   ▼
Engine Main Loop
   │
   ├── 从 Registry 获取 Tool Definition
   │
   ├── Thinking 开启时：隐藏工具，生成规划 Trace
   │
   ├── Action 阶段：恢复工具，调用 LLMProvider.Generate
   │
   ├── 没有 Tool Call ──────────────► 输出最终响应并结束运行
   │
   └── 存在 Tool Call
          │
          ▼
       按 ParallelSafe 分波
          │
          ├── 安全工具：最多 4 个并发执行
          └── 独占/未知工具：作为前后波次屏障
          │
          ▼
       按原 Tool Call 顺序聚合结果
          │
          └─────────────────────────► 再次调用 Provider
```

## 项目布局

```text
go-reagent/
├── config.example.json          # 不含真实密钥的平台配置模板
├── config.json                  # 本地平台配置（已被 Git 忽略）
├── cmd/
│   └── reagent/
│       ├── main.go              # 组装日志、Provider、Registry 和文件工具
│       └── main_test.go         # 启动配置、日志与真实工具组装测试
├── internal/
│   ├── config/              # 启动配置层
│   │   ├── config.go        # 严格加载、校验与选择当前平台
│   │   └── config_test.go   # 配置解析与错误处理测试
│   ├── engine/              # 核心引擎层
│   │   ├── loop.go          # AgentEngine、ReAct Main Loop 与有界工具调度
│   │   └── loop_test.go     # 生命周期、并发上限、屏障与取消测试
│   ├── logtest/             # 日志测试支持
│   │   └── recorder.go      # 并发安全的 logsdk.Logger 记录器
│   ├── provider/            # 模型适配层
│   │   ├── interface.go     # LLM Provider 接口
│   │   ├── factory.go       # 根据协议创建 Provider
│   │   ├── openai.go        # OpenAI Chat Completions 兼容适配器
│   │   └── claude.go        # Anthropic Messages 兼容适配器
│   ├── schema/              # 公共数据结构
│   │   └── message.go       # 消息、角色与工具调用类型
│   └── tools/               # 工具与执行层
│       ├── registry.go      # 线程安全的工具注册、发现与执行
│       ├── registry_test.go # Registry 生命周期与错误隔离测试
│       ├── read_file.go      # WorkDir 内受限文本文件读取工具
│       ├── read_file_test.go # 路径与输出预算安全测试
│       ├── edit_file.go      # WorkDir 内受限的多级唯一匹配编辑工具
│       └── edit_file_test.go # 匹配、路径、文本与权限安全测试
├── go.mod
└── README.md
```

各模块之间保持单向依赖：

```text
engine ──► provider ──► schema
   │
   └─────► tools ─────► schema
```

- `schema` 不依赖其他业务包，统一各层交换的数据结构。
- `config` 通过 Configor 加载 JSON、YAML 或 TOML 平台配置，并返回 `currentPlatform` 选中的完整配置。
- `provider` 通过 `LLMProvider.Generate` 隔离协议调用细节，通过工厂选择 OpenAI 或 Anthropic 适配器。
- `tools` 通过 `BaseTool`、`MutableRegistry` 和 `Registry` 分离工具注册、发现与执行权限。
- `engine` 持有工作区路径并负责编排上下文、模型推理与工具执行，不关心具体厂商和工具实现。
- `cmd/reagent` 是依赖组装入口，不承载核心业务逻辑。

## 环境要求

- Go 1.26 或更高版本

项目使用 OpenAI Go v3、Anthropic Go SDK 和 `go-logger-sdk` v1.0.5。

## 快速开始

复制本地配置模板并限制文件权限。默认示例使用 JSON，也可以通过 `CONFIG_PATH` 指定 YAML 或 TOML：

```bash
cp config.example.json config.json
chmod 600 config.json
```

在 `config.json` 中填写各平台的 `apiKey`。默认选择 DeepSeek：

```json
{
  "currentPlatform": "deepseek",
  "platforms": [
    {
      "id": "deepseek",
      "protocol": "openai",
      "baseURL": "https://api.deepseek.com/v1/",
      "apiKey": "填写你的 DeepSeek API Key",
      "model": "deepseek-chat"
    }
  ]
}
```

然后启动：

```bash
go run ./cmd/reagent
```

切换到其他已配置平台时只需修改 `currentPlatform`：

```json
"currentPlatform": "zhipu-claude"
```

平台字段：

| 字段 | 说明 |
| --- | --- |
| `id` | 平台实例的唯一标识，供 `currentPlatform` 引用 |
| `protocol` | `openai` 或 `anthropic` |
| `baseURL` | 兼容 API 的基础地址 |
| `apiKey` | 与该平台和模型配套的密钥 |
| `model` | 该平台实例使用的模型 ID |

配置文件由 Configor 加载，支持环境叠加、example 回退和 Shell 环境变量覆盖：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CONFIG_PATH` | `config.json` | 指定其他平台配置文件 |
| `CONFIGOR_ENV` | `development`（测试时为 `test`） | 加载环境叠加文件，如 `config.production.json` |
| `CONFIGOR_CURRENTPLATFORM` | 配置文件中的值 | 覆盖当前平台选择 |
| `AGENT_PROMPT` | 并行读取并总结三个项目文件 | 覆盖启动测试任务 |

例如使用另一份配置：

```bash
CONFIG_PATH=/secure/reagent/config.json go run ./cmd/reagent
```

例如加载 YAML 基础配置及其 `production` 叠加文件：

```bash
CONFIG_PATH=/secure/reagent/config.yaml CONFIGOR_ENV=production go run ./cmd/reagent
```

Configor 会先加载基础文件，再加载同目录下的环境文件，例如 `config.production.yaml`。如果基础文件和环境文件都不存在，则尝试同扩展名的 example 文件，例如 `config.example.yaml`。字段也可以通过 `CONFIGOR_` 前缀的环境变量覆盖；数组内字段按结构路径命名，例如 `CONFIGOR_PLATFORMS_0_APIKEY`。

`config.json` 已加入 `.gitignore`，不要把真实 API Key 写入 `config.example.json`。

当前入口开启慢思考模式，挂载真实 `read_file` 和 `edit_file` 工具，默认要求模型同时读取并总结
`README.md`、`go.mod` 和 `cmd/reagent/main.go`。

### 日志输出

运行日志通过 `go-logger-sdk` 以 JSON 写入 stdout，每条日志都包含
`module=go-reagent`，并通过 `component`、`turn`、`phase`、`tool` 和
`tool_call_id` 等结构化字段标识来源与执行上下文。平台启动日志只记录平台 ID、协议和模型，
不会记录 API Key、Authorization Header 或完整平台配置。

模型的内部思考 Trace 和最终回复仍以纯文本输出，因此直接运行命令时会看到 JSON 运行日志与
模型文本结果共存；接入日志平台时应按 JSON 行采集运行日志。

`go-logger-sdk` v1.0.5 的 `caller` 字段目前指向 SDK 内部方法，而不是实际业务调用位置。
排查日志时应以 `component`、`turn`、`phase`、`tool` 和 `tool_call_id` 等结构化字段为准。

### 工具并发调度

- `ToolDefinition.ParallelSafe` 默认是 `false`，未声明和未知工具按独占方式执行。
- 当前只有只读的 `read_file` 标记为并发安全；`edit_file` 保持独占执行。
- 连续的安全工具组成一个波次，默认最多同时执行 4 个；`MaxParallelTools <= 0` 时退化为串行。
- 独占工具会等待前一安全波完成，并阻止后一波提前启动。
- Observation 始终按模型原始 Tool Call 顺序回传，与工具实际完成顺序无关。
- 同一安全波中的调用必须语义独立；需要前序结果的操作仍应拆成多个模型轮次。

### read_file 安全边界

- 只接受相对于当前 WorkDir 的路径。
- 使用 Go 1.26 `os.Root` 阻止绝对路径、`..` 穿越和外部符号链接逃逸。
- 允许指向 WorkDir 内部文件的符号链接。
- 只读取普通 UTF-8 文本文件，拒绝目录、设备和疑似二进制内容。
- 最多返回前 8000 字节，并在合法 UTF-8 边界截断。

### edit_file 安全边界

- 只修改 WorkDir 内已经存在的普通 UTF-8 文本文件，拒绝 NUL 内容、路径穿越和外部符号链接。
- `old_text` 依次尝试精确匹配、CRLF/LF 等价、片段首尾空白容错和逐行忽略缩进匹配。
- 每一级都必须唯一命中；未命中会提示先调用 `read_file`，多处命中会要求提供更多上下文。
- 替换只改变命中的原始字节区间，并保留其他内容、原换行风格和文件权限。
- `new_text` 可以显式传入空字符串以删除片段，但字段不能缺失。
- 工具未标记为并发安全，调度器会把每次编辑作为独占屏障执行。

运行全部测试：

```bash
go test ./...
```

## 当前能力

- 统一的 `system`、`user` 和 `assistant` 消息角色；工具观察结果通过 `user` 消息和 `tool_call_id` 关联。
- Provider 无关的消息、Tool Call、Tool Result 和带调度元数据的 Tool Definition 数据结构。
- 使用 `json.RawMessage` 保留工具调用参数，并由具体工具延迟解析。
- 可替换的 `LLMProvider` 接口，每轮接收上下文和可用工具定义。
- Configor 驱动的 JSON/YAML/TOML 平台配置加载，以及项目级规范化与启动前校验。
- 配置驱动的 OpenAI Chat Completions 兼容 Provider。
- 配置驱动的 Anthropic Messages 兼容 Provider。
- 通过 `currentPlatform` 一键切换 DeepSeek、智谱或其他兼容服务。
- 基于 `go-logger-sdk` 的 JSON 运行日志，以及 Bootstrap、Engine 和 Registry 的结构化上下文字段。
- 线程安全、稳定排序、拒绝重复注册的真实 Tool Registry。
- 工具 error、Context 取消和 panic 的统一错误隔离。
- 受 WorkDir 能力边界保护的真实 `read_file` 和 `edit_file` 工具。
- 持有 `WorkDir` 的 `AgentEngine` 生命周期驱动。
- 可选的 Thinking Phase：暂时隐藏工具，将规划 Trace 注入 Action 上下文。
- 支持直接模型响应的 ReAct Main Loop。
- 支持将连续安全 Tool Call 有界并发执行，以独占工具为屏障，并稳定聚合结果。
- Provider 错误和空响应防护。
- 工具调用 ID 的整批前置校验。
- 取消信号的 Provider 与工具执行前置检查。
- 可直接运行的真实 Provider、Registry 与文件工具示例。

## 开发路线图

- [x] 定义公共消息与工具调用结构。
- [x] 定义 LLM Provider 接口。
- [x] 定义工具发现和执行 Registry 接口。
- [x] 实现并测试最小 AgentEngine ReAct Main Loop。
- [x] 增加可开关的慢思考与行动双阶段循环。
- [x] 实现内存版 Tool Registry 和基础 `read_file`、`edit_file` 工具。
- [x] 实现配置驱动的 OpenAI/Anthropic 兼容 Provider。
- [x] 在 `cmd/reagent` 中组装真实 Provider、Registry 和工具。
- [x] 增加按工具安全等级分波的有界并发调度器。
- [ ] 增加可配置的轮次、Token、时间与成本预算。
- [x] 增加支持多格式、环境叠加和字段覆盖的平台启动配置。
- [ ] 增加上下文管理和持久化记忆。
- [ ] 增加飞书等外部消息渠道适配。
