# 平台配置文件设计

日期：2026-07-27

## 背景与目标

当前启动入口通过 `LLM_PROVIDER`、`LLM_MODEL`、`DEEPSEEK_API_KEY` 和
`ZHIPU_API_KEY` 等环境变量选择模型。配置分散，切换平台时需要同时修改多项变量，
也无法直观看出某个 Token 对应的协议、地址和模型。

本次改造将每个平台定义为一条可直接运行的完整配置，并通过
`currentPlatform` 选择当前配置。切换平台时只修改一个字段；Token、模型、协议和
API 地址始终属于同一平台配置。

## 配置格式

项目根目录的 `config.json` 使用以下结构：

```json
{
  "currentPlatform": "deepseek",
  "platforms": [
    {
      "id": "deepseek",
      "protocol": "openai",
      "baseURL": "https://api.deepseek.com/v1/",
      "apiKey": "sk-your-deepseek-key",
      "model": "deepseek-chat"
    },
    {
      "id": "zhipu-openai",
      "protocol": "openai",
      "baseURL": "https://open.bigmodel.cn/api/paas/v4/",
      "apiKey": "your-zhipu-key",
      "model": "glm-4.5-air"
    },
    {
      "id": "zhipu-claude",
      "protocol": "anthropic",
      "baseURL": "https://open.bigmodel.cn/api/anthropic/",
      "apiKey": "your-zhipu-key",
      "model": "glm-4.5-air"
    }
  ]
}
```

字段语义：

- `currentPlatform`：当前启用的平台 ID，必须引用 `platforms` 中的一项。
- `id`：平台配置的唯一标识。它是用户定义的实例名，不是厂商枚举。
- `protocol`：协议适配器，第一阶段支持 `openai` 和 `anthropic`。
- `baseURL`：兼容 API 的基础地址。
- `apiKey`：该平台实例使用的认证密钥。
- `model`：该平台实例使用的模型 ID。

同一个厂商需要多个模型时，配置为多个平台实例。例如 `deepseek-chat` 和
`deepseek-reasoner` 可以拥有不同的 `id`，仍然只通过 `currentPlatform` 切换。

## 配置来源

- 默认读取当前工作目录下的 `config.json`。
- `CONFIG_PATH` 可以指定其他配置文件。
- `AGENT_PROMPT` 继续用于临时覆盖演示任务。
- 不再使用 `LLM_PROVIDER`、`LLM_MODEL`、`DEEPSEEK_API_KEY` 和
  `ZHIPU_API_KEY`，避免文件配置和环境覆盖产生不透明的优先级。
- 本阶段不支持配置热加载；配置只在进程启动时读取一次。

## 架构边界

### `internal/config`

新增独立配置包，负责：

- 严格读取和解析 JSON；
- 标准化字符串与 Base URL；
- 校验平台配置；
- 根据 `currentPlatform` 返回当前平台。

配置包不创建 Provider，也不依赖 `internal/provider`。

```go
type Config struct {
    CurrentPlatform string           `json:"currentPlatform"`
    Platforms       []Options `json:"platforms"`
}

type Options struct {
    ID       string `json:"id"`
    Protocol string `json:"protocol"`
    BaseURL  string `json:"baseURL"`
    APIKey   string `json:"apiKey"`
    Model    string `json:"model"`
}
```

公开行为：

```go
func Load(path string) (*Config, error)
func (c *Config) Current() (Options, error)
```

### `internal/provider`

新增协议工厂，不再由厂商专用构造器读取环境变量：

```go
type Options struct {
    Name     string
    Protocol string
    BaseURL  string
    APIKey   string
    Model    string
}

func New(options Options) (LLMProvider, error)
```

`provider.New` 只认识协议，不认识厂商：

- `openai` 创建 OpenAI Chat Completions 兼容 Provider；
- `anthropic` 创建 Anthropic Messages 兼容 Provider；
- 其他值返回错误。

现有消息翻译、工具定义翻译、Tool Call 历史恢复和 Thinking 阶段隐藏工具的行为保持
不变。

### `cmd/reagent`

入口只负责组装依赖：

1. 解析 `CONFIG_PATH`，默认使用 `config.json`；
2. 加载配置并取得当前平台；
3. 通过 `provider.New` 创建协议适配器；
4. 创建 Registry 和 AgentEngine；
5. 启动 Main Loop。

入口不再包含 DeepSeek、智谱等厂商分支。

## 数据流

```text
config.json
    │
    ▼
config.Load
    │ 严格解析、标准化、校验
    ▼
Config.Current
    │ 选中 currentPlatform
    ▼
provider.New
    ├── protocol=openai    → OpenAIProvider
    └── protocol=anthropic → ClaudeProvider
    │
    ▼
AgentEngine
```

## 校验与错误处理

配置加载采用严格 JSON 解析，未知字段和尾部多余内容均视为错误，防止字段拼写错误被
静默忽略。

所有平台都必须满足：

- `id` 非空且全局唯一；
- `protocol` 为 `openai` 或 `anthropic`；
- `baseURL` 是带 Host 的 HTTP 或 HTTPS 地址；
- `model` 非空。

只有当前平台强制要求 `apiKey` 非空。非当前平台允许暂时不配置密钥，切换过去时会
得到明确错误。

`baseURL` 去除首尾空白并规范化为以 `/` 结尾。`id`、`protocol`、`apiKey` 和
`model` 去除首尾空白，`protocol` 统一转为小写。

典型错误应包含配置路径和字段上下文，但不得包含 API Key：

```text
加载配置 config.json 失败: platforms[1].baseURL 不是合法的 HTTP/HTTPS 地址
当前平台 "zhipu" 不存在，可用平台: deepseek, zhipu-openai, zhipu-claude
当前平台 "deepseek" 未配置 apiKey
```

## 安全策略

- `config.json` 加入 `.gitignore`，不得提交真实密钥。
- 仓库提交不含真实密钥的 `config.example.json`。
- 启动日志只打印平台 ID、协议和模型，不打印 API Key 或 Authorization Header。
- README 提醒本地用户将 `config.json` 权限设置为 `0600`。
- 测试只使用假密钥和本地 `httptest`，不访问真实模型 API。

## 文件调整

```text
go-reagent/
├── .gitignore                         # 新增：忽略 config.json
├── config.example.json                # 新增：安全配置模板
├── config.json                        # 新增：本地配置，不进入版本控制
├── cmd/reagent/
│   ├── main.go                        # 改为从配置组装 Provider
│   └── main_test.go                   # 更新启动配置测试
├── internal/
│   ├── config/
│   │   ├── config.go                  # 新增：加载、校验、选择平台
│   │   └── config_test.go             # 新增：配置行为测试
│   └── provider/
│       ├── factory.go                 # 新增：按协议创建 Provider
│       ├── factory_test.go            # 新增：协议工厂测试
│       ├── openai.go                  # 保留协议翻译，移除厂商构造器
│       ├── claude.go                  # 保留协议翻译，移除厂商构造器
│       ├── deepseek.go                # 删除：端点改由配置提供
│       └── environment.go             # 删除：Provider 不再读取环境变量
└── README.md                          # 更新配置和启动说明
```

## 测试策略

配置包测试覆盖：

- 加载并选择有效平台；
- 自定义配置路径；
- 重复平台 ID；
- 当前平台不存在；
- 当前平台缺少 API Key；
- 空 Model；
- 不支持的 Protocol；
- 非法 Base URL；
- 未知 JSON 字段；
- JSON 尾部多余内容；
- Base URL 自动补 `/`。

Provider 工厂测试覆盖：

- `openai` 返回 OpenAIProvider；
- `anthropic` 返回 ClaudeProvider；
- 不支持的协议返回错误；
- 必需字段为空时返回错误；
- 错误信息不泄露 API Key。

保留现有 Provider HTTP 协议测试、AgentEngine 双阶段测试和 Mock Weather Registry
测试。

完成验证命令：

```bash
go vet ./...
go test -race -count=1 ./...
gofmt -l cmd internal
```

## 非目标

本次不实现：

- 配置热加载；
- 密钥加密或远程 Secret Manager；
- 一个实例内动态选择多个模型；
- Thinking、预算、Prompt 等其他运行参数配置化；
- Web 配置界面。

这些能力可以在当前平台实例结构上继续扩展，不影响本次边界。

## 验收标准

- 用户只修改 `currentPlatform` 即可在已配置的平台实例间切换；
- 新增 OpenAI/Anthropic 兼容厂商只需要增加 JSON 配置，无需增加厂商 Go 代码；
- 配置错误在网络请求前失败，并给出不泄露密钥的明确错误；
- 真实 `config.json` 不进入版本控制；
- 所有现有功能和测试继续通过。
