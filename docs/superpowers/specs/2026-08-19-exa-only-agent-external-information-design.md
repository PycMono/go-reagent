# Agent 外部信息统一通过 Exa MCP 设计

日期：2026-08-19

## 背景

go-reagent 已经在 `pi/mcp` 中实现通用 HTTP MCP 客户端，并在 Web 组合根支持通过 Exa Hosted MCP 发现 `web_search_exa` 与 `web_fetch_exa`。但当前天气和地点时区解析仍由编译期工具直接调用 Open-Meteo HTTP API，因此 Agent 获取公网信息存在两条路径：

```text
天气和地点解析 -> Open-Meteo HTTP API
网页搜索与抓取 -> Exa MCP
```

本期统一 Agent 获取外部信息的出口。天气、新闻、政策、实时价格和网页资料等为了回答用户而发起的公网信息查询，只允许通过 Exa MCP。模型 Provider、Exa MCP 自身的连接、MySQL、Redis、企业微信和用户访问 Web 服务等业务基础设施连接不属于该约束。

## 目标

- Agent 的公网信息搜索和网页抓取统一使用 Exa MCP。
- 删除 `get_weather`、Open-Meteo Driver 及其天气和地点领域契约。
- 天气问题直接使用 `web_search_exa`，必要时使用 `web_fetch_exa` 获取原始页面。
- 保留 `get_current_time`，但改为使用调用方提供的 IANA 时区和本地时钟计算，不再联网解析地点。
- Exa 配置为 Web 应用的必需能力；配置、认证或工具发现失败时阻止应用启动。
- 运行期间 Exa 调用失败时，不回退到模型记忆、Open-Meteo 或其他公网数据源。
- 通过架构测试防止 Agent 工具重新引入直接公网 HTTP 查询。

## 非目标

本期不实现：

- Open-Meteo MCP Server；
- 天气专用 MCP Tool 或 Exa 天气结果的固定 JSON 结构转换；
- 通用网络代理、HTTP CONNECT 或对全部网络流量的拦截；
- 模型 Provider、数据库、缓存、Webhook 等基础设施经 Exa 转发；
- Exa 不可用时的其他搜索 Provider fallback；
- 对网页内容正确性的自动事实核验或多来源自动裁决；
- 每个 Skill 的代码级动态工具白名单。

## 方案选择

采用“直接暴露 Exa MCP 工具”的方案。领域 Skill 负责组织搜索意图，模型直接调用 `web_search_exa` 和 `web_fetch_exa`，不增加本地天气包装工具。

未选择以下方案：

- **保留 `get_weather` 并在内部调用 Exa**：需要把非结构化网页结果强制转换为原天气结构，增加脆弱的解析层，并隐藏真实数据来源。
- **新增统一 ExternalInformationGateway**：约束更强，但会在现有通用 MCP Extension 之外再增加一层代理，一期没有足够的多 Provider 或领域包装需求支撑该抽象。
- **开发 Open-Meteo MCP Server**：仍会保留第二个公网信息 Provider，不符合本期统一到 Exa 的目标。

## 边界定义

“所有联网查询统一通过 Exa MCP”的准确含义是：

> Agent 为回答用户而获取公网实时信息、搜索结果或网页正文时，只能调用 Exa MCP 工具。

以下行为受该约束：

- 天气和自然灾害相关公开信息；
- 新闻、政策、价格和公开事件；
- 网站、文档和公开资料搜索；
- 读取搜索结果或用户指定的公网网页。

以下连接不受该约束：

- OpenAI、DeepSeek、智谱等模型 API；
- go-reagent 到 Exa MCP Server 的 HTTP 连接；
- MySQL、Redis 和其他业务存储；
- 企业微信等业务集成；
- 用户浏览器与 go-reagent Web 服务之间的连接。

## 调用架构

统一后的外部信息调用链：

```text
用户问题
  -> Workspace AGENTS.md 与领域 Skill
    -> 模型选择 web_search_exa / web_fetch_exa
      -> Pi ToolRuntime
        -> pi/mcp Tool Adapter
          -> Exa Hosted MCP
            -> 公网搜索结果或网页正文
```

`pi/mcp` 保持通用协议与传输层，不导入天气、新闻等业务概念。领域行为只存在于 Workspace Skill 中。

## 目标目录与删除范围

保留通用 MCP 实现：

```text
pi/
└── mcp/
    ├── client.go
    ├── extension.go
    ├── protocol.go
    ├── transport_http.go
    ├── tool.go
    ├── errors.go
    └── 对应测试
```

调整本地工具：

```text
application/tool/chat/
├── register.go                 # 不再注册 get_weather
├── current_time.go             # 使用 IANA 时区进行纯本地计算
├── current_time_test.go
├── calculate.go
└── 其他本地工具
```

删除以下天气和地点解析实现：

```text
application/tool/chat/weather.go
application/tool/chat/weather_test.go
application/tool/chat/location.go
domain/service/weather.go
infrastructure/driver/openmeteo/
```

同时从 `application/web/register.go` 删除 `openmeteo.Register`。若删除后某个地点类型或测试帮助函数仍被其他包引用，应在使用方内收缩为局部类型，不保留已经失去业务所有者的天气领域接口。

## Exa MCP 配置

Web 应用使用以下 Exa 配置：

```json
{
  "mcp": {
    "servers": [
      {
        "name": "exa",
        "enabled": true,
        "required": true,
        "url": "https://mcp.exa.ai/mcp",
        "timeout": 60,
        "header_env": {
          "x-api-key": "EXA_API_KEY"
        },
        "allow_tools": [
          "web_search_exa",
          "web_fetch_exa"
        ],
        "tool_prefix": ""
      }
    ]
  }
}
```

`EXA_API_KEY` 只从进程环境读取，不写入配置文件、日志或测试输出。`tool_prefix` 固定为空，使 Workspace Skill 使用稳定的远端工具名。

Exa 是 Web 应用的必需外部信息能力。出现以下任一情况时 Fx 启动失败：

- Web 配置中不存在已启用且 `required: true` 的 `exa` Server；
- Exa Server 的 URL、允许工具列表或空前缀不符合本设计约束；
- 缺少或传入空的 `EXA_API_KEY`；
- Exa MCP initialize 或 initialized notification 失败；
- `tools/list` 未发现 `web_search_exa` 或 `web_fetch_exa`；
- 远端工具 Schema 无效或与本地工具重名；
- MCP 连接在配置的启动超时内未完成。

不改变未挂载 Web 组合根的 Pi Core 和测试构造方式；通用 `pi` 包仍然允许调用方在没有 MCP 的情况下独立使用。

## 天气查询行为

`weather-assistance` Skill 改为直接使用 Exa：

1. 从对话上下文解析地点和时间范围；缺少地点时先询问用户。
2. 结合会话当前日期，把“今天”“明天”等相对时间转换为明确日期。
3. 使用 `web_search_exa` 搜索地点、日期和用户关心的温度、降水、风力或天气现象。
4. 优先选择气象机构、政府部门或可信天气服务的结果。
5. 搜索摘要不足以支持结论时，对选中的结果调用 `web_fetch_exa`。
6. 多个可信来源冲突时说明差异，不把不同来源的字段拼成一份虚构预报。
7. 回答包含解析后的地点、明确日期、来源和查询时间。

Exa 返回的是搜索结果和网页内容，Agent 不得把它描述为 Open-Meteo 数据或保证固定字段完整。超出来源覆盖范围的信息必须明确无法确认。

## 当前时间工具

`get_current_time` 保留为本地确定性工具。输入从地点改为 IANA 时区：

```json
{
  "timezone": "Asia/Shanghai"
}
```

工具使用 Go `time.LoadLocation` 与注入的本地时钟返回：

- IANA 时区；
- RFC3339 本地时间；
- 当地日期；
- 星期。

该工具不发起网络请求。IANA 时区无效时返回 `tool_invalid_arguments`。模型可以直接使用明确且可信的 IANA 时区；对陌生或有歧义的地点，不允许调用其他地理 API，应先使用 Exa 查询时区，再调用 `get_current_time`。

## 运行期失败语义

`web_search_exa` 或 `web_fetch_exa` 调用失败时：

- ToolRuntime 按现有语义返回错误 ToolResult；
- Agent 可以修正参数后重试一次不同查询，但不能切换到其他公网 Provider；
- 持续失败时明确告知用户当前无法验证实时信息；
- 不使用模型记忆补写具体温度、价格、政策状态或其他时效性事实；
- 不泄露认证 Header、请求完整内容或远端响应中的潜在敏感数据。

搜索成功但信息不足时，Agent 应说明证据不足，不能把“没有找到”改写成确定的否定事实。

## 架构约束

本期通过以下约束守住统一出口：

- `application/tool/chat` 不允许直接依赖 `net/http` 或公网信息 Driver；
- Web Agent 组合根不注册 Open-Meteo 或其他公网信息 Provider；
- `pi/mcp` 是 Agent 公网信息工具唯一允许的 HTTP 传输边界；
- 模型 Provider 和业务基础设施网络包显式排除在该约束之外；
- 包边界测试和 Fx 工具清单测试共同验证约束，不依赖代码评审人员记忆。

未来新增股票、汇率、政策等 Agent 实时信息能力时，一期默认通过 Exa Skill 路由。如果以后确实需要结构化专用 Provider，应先修改本设计边界并明确新增 MCP Server，而不是在领域工具中直接加入 HTTP Client。

## 测试设计

### 本地工具测试

- 工具注册清单不再包含 `get_weather`；
- `get_current_time` 的 Schema 只接受 `timezone`；
- 有效 IANA 时区产生确定的本地日期、时间和星期；
- 无效时区、额外参数和取消 Context 返回现有分类错误；
- 删除依赖 LocationResolver 和 WeatherProvider 的测试夹具。

### Workspace 测试

- `weather-assistance` 明确要求 `web_search_exa`；
- 摘要不足时允许 `web_fetch_exa`；
- 禁止引用 `get_weather`、Open-Meteo 或无来源实时天气；
- 天气 Skill 的触发路由用例保持通过。

### MCP 与 Web 集成测试

- 模拟 MCP Server 暴露两个 Exa 工具，天气 Agent 调用最终到达 `tools/call` 的 `web_search_exa`；
- 缺少启用的 Exa Server 配置时 Web 组合根启动失败；
- 缺少 Exa Header 环境变量时 Web 组合根启动失败；
- 缺少任一允许工具时启动失败；
- 远端调用失败时不存在 Open-Meteo 或其他 HTTP fallback；
- 现有通用 MCP 客户端、Transport 和工具适配测试继续通过。

### 架构测试

- `application/tool/chat` 的生产代码不导入 `net/http`；
- Web Agent 组合根不依赖 `infrastructure/driver/openmeteo`；
- 仓库中不存在 Open-Meteo 生产包和默认 API URL；
- Agent 对外信息工具清单只包含来自 MCP Extension 的搜索和抓取工具，本地计算、文件读取和模型 Provider 不计入公网信息工具。

### 验证命令

```bash
go test ./... -count=1 -timeout=180s
go test -race ./... -count=1 -timeout=300s
go vet ./...
git diff --check
```

真实 Exa smoke test 保持 opt-in；只有设置 `EXA_API_KEY` 和显式测试开关时才访问 Hosted MCP，普通测试不得依赖公网。

## 迁移结果

迁移完成后的关键行为：

```text
天气、新闻、政策、价格、网页资料
  -> Exa MCP

当前时间
  -> 本地时钟 + 本地时区数据库

模型 API、MySQL、Redis、业务接口
  -> 各自现有基础设施连接
```

仓库不再包含 Open-Meteo 生产代码，Web 应用未成功连接 Exa 时不会开始接收请求。这样既保留 `pi/mcp` 的通用扩展能力，也建立了 Agent 外部信息检索的单一出口。
