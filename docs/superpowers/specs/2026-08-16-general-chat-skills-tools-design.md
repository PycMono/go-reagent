# 通用聊天 Skills 与工具设计方案

## 目标

在现有单浏览器聊天 Agent 基础上增加一组克制、可验证的通用聊天能力。第一期提供真实天气、当地时间和安全数值计算，同时用四个条件性 Skill 稳定天气建议、写作辅助、方案决策和学习讲解的行为。

本设计不改变单 Agent Workspace 架构。浏览器 Agent 继续从 `workspaces/chat` 加载身份和 Skills，通过 `pi` 的现有 Tool Group 调用业务明确注册的工具，并沿用 Cookie、Conversation、SSE、MySQL 持久化和 Direct Loop。

## 已确认决策

- 第一期采用均衡方案：4 个 Skill 和 3 个 Tool。
- 第一期不包含网页搜索。
- 天气使用 Open-Meteo 地理编码与预报 API，不需要 API Key。
- 普通问候、闲聊、翻译和用户提供文本的简单总结不创建 Skill。
- 当前时间和数值计算属于原子 Tool，不创建对应 Skill。
- 天气建议使用 `weather-assistance` Skill 和真实天气 Tool。
- 写作、决策和学习讲解使用纯 Skill，不增加外部权限。
- 城市不存在或有歧义是正常结构化结果，不作为 Tool Error。
- Web 继续关闭独立 Thinking 阶段。
- Web 不注册 `write`、`edit`、`apply_patch`、`exec` 或 `process`。
- 不新增数据库迁移、HTTP 页面接口、登录、在线训练、Agent 版本或多 Agent。
- Skill 实施前后允许使用独立子代理做只读对照评测；评测不调用真实外部 API，也不写业务数据库。

## 不在第一期范围内

本次不实现：

- 网页搜索、网页抓取和引用来源聚合；
- 地图、路线、航班、酒店或完整旅行规划；
- 每日简报；
- 日历、提醒和后台 Scheduler；
- 长期用户偏好和个人记忆；
- 汇率、股票、新闻或其他实时数据源；
- 文件上传、OCR 或用户文档解析；
- 通用单位换算系统；
- 天气告警订阅和主动推送；
- Open-Meteo 之外的可切换天气 Provider；
- Skill 管理页面或在线编辑流程。

## 能力分层

通用聊天能力分成三层：

```text
AGENTS.md
  -> 全局身份、语气、安全边界和普通聊天规则

Skills
  -> 条件性、多步骤、容易执行不一致的处理流程

Tools
  -> 当前时间、计算、天气等真实或确定性能力
```

不得为了让目录看起来丰富而给每种普通聊天意图创建 Skill。Skill 元数据会常驻模型上下文，过宽或重叠的描述会增加误触发和读取成本。

### 不创建 Skill 的能力

- 普通问候和闲聊；
- 简单翻译；
- 对用户已经提供的文本做直接总结；
- 简单改写请求中不需要稳定流程的部分；
- 当前时间；
- 数值计算。

当前时间和数值计算依靠 Tool Definition 让模型发现能力。其他基础聊天行为继续由 `workspaces/chat/AGENTS.md` 和模型原生能力承担。

## 目录结构

```text
domain/
└── service/
    └── weather.go

application/
└── tool/
    └── chat/
        ├── register.go
        ├── weather.go
        ├── weather_test.go
        ├── current_time.go
        ├── current_time_test.go
        ├── calculate.go
        └── calculate_test.go

infrastructure/
└── driver/
    └── openmeteo/
        ├── register.go
        ├── client.go
        └── client_test.go

workspaces/
└── chat/
    ├── AGENTS.md
    └── skills/
        ├── weather-assistance/
        │   └── SKILL.md
        ├── writing-assistance/
        │   └── SKILL.md
        ├── decision-support/
        │   └── SKILL.md
        └── learning-explanation/
            └── SKILL.md
```

最终实现可以把相邻的小测试合并到一个测试文件，但不得把 Open-Meteo HTTP 协议、Agent Tool 行为和 Skill Discovery 测试混在同一个测试单元中。

## 领域契约

`domain/service/weather.go` 定义 Application 与外部天气 Provider 之间的稳定接口，不依赖 Open-Meteo JSON。

建议类型：

```go
type LocationQuery struct {
    Name        string
    CountryCode string
    Admin1      string
    Limit       int
}

type Location struct {
    Name        string
    Country     string
    CountryCode string
    Admin1      string
    Latitude    float64
    Longitude   float64
    Timezone    string
}

type ForecastRequest struct {
    Location  Location
    StartDate time.Time
    Days      int
}

type DailyForecast struct {
    Date                    string
    WeatherCode             int
    Condition               string
    TemperatureMinC         float64
    TemperatureMaxC         float64
    PrecipitationProbability int
    WindSpeedMaxKPH         float64
}

type Forecast struct {
    Location    Location
    GeneratedAt time.Time
    Days        []DailyForecast
}

type LocationResolver interface {
    ResolveLocations(context.Context, LocationQuery) ([]Location, error)
}

type WeatherProvider interface {
    Forecast(context.Context, ForecastRequest) (Forecast, error)
}
```

字段命名在实现时可以按 Go 格式微调，但 Tool JSON 输出必须稳定使用下文定义的 snake_case 字段。

## Open-Meteo Driver

### 固定端点

生产 Client 只访问：

```text
https://geocoding-api.open-meteo.com/v1/search
https://api.open-meteo.com/v1/forecast
```

Tool 参数不得包含 URL、Host、经纬度或任意请求参数透传。测试通过构造函数注入 `httptest.Server` 地址和 `http.Client`，生产 Fx Provider 使用固定端点。

### HTTP 规则

- 使用请求 Context，取消后立即终止请求；
- 默认总超时 10 秒；
- 设置明确的 `User-Agent`；
- 只接受 2xx；
- 响应体上限 1 MiB；
- JSON Decoder 只映射所需字段，允许上游增加无关字段，并在解码后严格校验必需字段和数组；
- 不把上游原始响应正文放入错误、日志或 Tool 输出；
- 第一期不做内部自动重试；
- 不记录完整查询 URL，避免把用户位置写入结构化日志。

### 地理编码

请求最多获取 5 个候选，并映射为领域 `Location`。解析规则由 Application Tool 完成：

1. 去除地点、国家代码和一级行政区首尾空白；
2. 去除空白后地点为空属于 `tool_invalid_arguments`，不发送上游请求；
3. 国家代码规范化为大写两位代码；
4. 按可选 `country_code`、`admin1` 做忽略大小写的完整值过滤，不做包含匹配；
5. 没有候选返回 `not_found`；
6. 只有一个候选继续执行；
7. 多个候选返回 `ambiguous` 和最多 5 个候选，不擅自选择第一项。

地点候选只返回名称、国家、国家代码、一级行政区和时区，不向模型暴露经纬度。

### 天气预报

Driver 请求以下每日字段：

- `weather_code`；
- `temperature_2m_min`；
- `temperature_2m_max`；
- `precipitation_probability_max`；
- `wind_speed_10m_max`。

请求显式传入 `start_date`、`end_date` 和地点时区。返回日期必须与请求区间逐日完全一致，日期数组与每个指标数组长度也必须一致，否则响应非法。WMO Weather Code 映射为稳定英文枚举，例如 `clear`、`partly_cloudy`、`fog`、`drizzle`、`rain`、`snow`、`thunderstorm`；未知代码保留原始整数并映射为 `unknown`，不得猜测。

## Tool 设计

三个 Tool 都注册到 `group:"agent_tools"`，并设置 `ParallelSafe=true`。

### `get_weather`

用途：按地点获取今天、明天或未来 1 到 7 天的每日天气预报。

输入 Schema：

```json
{
  "type": "object",
  "properties": {
    "location": {"type": "string", "minLength": 1, "maxLength": 120},
    "country_code": {"type": "string", "pattern": "^[A-Za-z]{2}$"},
    "admin1": {"type": "string", "minLength": 1, "maxLength": 120},
    "date": {"type": "string", "minLength": 1, "maxLength": 10},
    "days": {"type": "integer", "minimum": 1, "maximum": 7}
  },
  "required": ["location"],
  "additionalProperties": false
}
```

语义：

- `date` 缺失时默认 `today`；
- `date` 允许 `today`、`tomorrow` 或严格 `YYYY-MM-DD`；
- `days` 缺失时默认 1；
- 返回区间必须完整落在地点当地今天起连续 7 个自然日内，即 `[local_today, local_today+6]`；
- `date` 决定返回区间首日，`days` 决定连续天数；例如 `date=tomorrow` 时最多返回 6 天；
- 日期计算基于解析地点的 IANA 时区，不基于服务器时区或模型猜测。

成功结果：

```json
{
  "status": "ok",
  "location": {
    "name": "Beijing",
    "country": "China",
    "country_code": "CN",
    "admin1": "Beijing",
    "timezone": "Asia/Shanghai"
  },
  "generated_at": "2026-08-16T09:30:00+08:00",
  "days": [
    {
      "date": "2026-08-17",
      "weather_code": 61,
      "condition": "rain",
      "temperature_min_c": 23.1,
      "temperature_max_c": 31.2,
      "precipitation_probability": 70,
      "wind_speed_max_kph": 18.4
    }
  ]
}
```

无结果：

```json
{"status":"not_found","query":"不存在的地点"}
```

歧义结果：

```json
{
  "status": "ambiguous",
  "query": "朝阳",
  "candidates": [
    {"name":"Chaoyang","country":"China","country_code":"CN","admin1":"Beijing","timezone":"Asia/Shanghai"},
    {"name":"Chaoyang","country":"China","country_code":"CN","admin1":"Liaoning","timezone":"Asia/Shanghai"}
  ]
}
```

`not_found` 和 `ambiguous` 返回正常 `ToolOutput`，不设置 `IsError`。非法参数和上游故障返回 error，由现有 ToolRuntime 分类并持久化真实 Tool Result。

### `get_current_time`

用途：按地点返回准确当地时间。

输入字段为 `location`、可选 `country_code` 和 `admin1`，约束与 `get_weather` 相同。地点无结果和歧义使用相同状态结构。

成功结果：

```json
{
  "status": "ok",
  "location": {
    "name": "Tokyo",
    "country": "Japan",
    "country_code": "JP",
    "admin1": "Tokyo",
    "timezone": "Asia/Tokyo"
  },
  "local_time": "2026-08-16T10:30:00+09:00",
  "date": "2026-08-16",
  "weekday": "Sunday"
}
```

实现导入 `time/tzdata`，保证最小部署环境仍可加载 IANA 时区。`get_current_time.local_time` 与 `get_weather.generated_at` 使用同一个可注入 Clock：生产使用 `time.Now`，测试使用固定时间；两个时间都转换到解析地点的 IANA 时区。`weekday` 固定输出英文 `Monday` 到 `Sunday`。

### `calculate`

用途：计算确定性的数值表达式。

输入 Schema：

```json
{
  "type": "object",
  "properties": {
    "expression": {"type": "string", "minLength": 1, "maxLength": 256}
  },
  "required": ["expression"],
  "additionalProperties": false
}
```

实现使用 `github.com/expr-lang/expr`，不给表达式注入环境、对象或函数，并在执行前遍历 AST。AST 只允许数值字面量、括号以及 `+`、`-`、`*`、`/`、`%`、幂和一元正负号；拒绝标识符、函数调用、成员访问、条件表达式、集合和字符串等其他节点。只接受最终结果为整数或浮点数；拒绝整数溢出、布尔值、字符串、集合、NaN 和无穷值。

结果：

```json
{"expression":"(12.5 + 7.5) * 3","result":60}
```

第一期不承诺自然语言单位换算。模型必须先把可确定的简单问题转换成数值表达式；需要实时汇率或业务单位表时应说明没有相应工具。

## Skill 设计

Skill 全部放在 `workspaces/chat/skills/<name>/SKILL.md`。只使用当前 Runtime 支持的 `name` 和 `description` Frontmatter，不生成 Codex 专用 `agents/openai.yaml`，也不创建脚本、README 或重复参考文件。

每个 SKILL.md 保持简短，正文使用命令式步骤。描述负责触发条件，正文不重复大段通用聊天规则。

### `weather-assistance`

触发条件覆盖：天气、气温、降雨、降雪、带伞、穿衣、户外活动和未来天气预报。

规则：

1. 缺少地点时只询问地点；
2. 使用 `get_weather`，不根据模型知识猜测天气；
3. `ambiguous` 时展示简短候选并让用户确认；
4. `not_found` 时请用户换用城市、国家或行政区描述；
5. 回答明确解析地点和预报日期；
6. 活动建议必须以温度、降水概率和风力等结果为依据；
7. Tool 失败时说明暂时无法获取，不编造替代数据。

### `writing-assistance`

触发条件覆盖：邮件、通知、文案、报告、改写、润色、缩写和语气调整。

规则：

1. 信息充分时直接给成稿，不先叙述写作计划；
2. 只在缺少受众、目的等关键条件时询问一个问题；
3. 匹配用户要求的语言、语气、长度和格式；
4. 不虚构姓名、日期、价格和业务事实，必要时使用明确占位符；
5. 修改已有文本时保留用户未要求改变的内容。

### `decision-support`

触发条件覆盖：比较、选择、权衡、排序和建议。

规则：

1. 提取目标、硬约束和偏好；
2. 使用明确评价维度比较；
3. 区分已知事实、用户偏好和推测；
4. 给出带条件的推荐和主要代价；
5. 缺少实时数据 Tool 时明确限制，不把旧知识当作当前事实。

### `learning-explanation`

触发条件覆盖：概念讲解、学习辅导、分步骤解释、举例和练习。

规则：

1. 从用户表达推断水平，确实无法判断时才询问；
2. 先给核心答案，再分步骤说明；
3. 至少提供一个贴合问题的例子；
4. 简单问题保持简短，不强制测验或追问；
5. 用户表示没理解时更换解释角度，不重复原文。

### 触发重叠

- 天气和户外活动建议优先使用 `weather-assistance`；
- 普通产品或方案比较使用 `decision-support`；
- “帮我写一封比较 A/B 的邮件”可以同时匹配写作和决策，先形成可靠比较，再按写作格式输出；
- “解释明天天气为什么这样”仍必须先获取真实天气，再做讲解；
- 模型只读取明显匹配的 Skill，不读取全部 Skill。

## Fx 装配

`application/tool/chat.Register` 提供三个 Tool，并把它们注册到现有 `group:"agent_tools"`。

`infrastructure/driver/openmeteo.Register` 提供一个共享 Client，并将其暴露为 `LocationResolver` 和 `WeatherProvider`。`get_weather` 消费两个接口，`get_current_time` 只消费 `LocationResolver`，`calculate` 不消费外部服务。

`application/web.Register` 组合：

```text
pi.CoreRegister
pi.ReadOnlyToolsRegister
chattools.Register
openmeteo.Register
pi.ThinkingEnabled(false)
```

最终 Web Tool Definition 名称按稳定排序为：

```text
calculate
get_current_time
get_weather
read
```

以下名称不得进入 Web 图：

```text
apply_patch
edit
exec
process
write
```

`pi.Register` 和 `pi.CodingToolsRegister` 保持不变，不把通用聊天业务工具放入 SDK 默认完整 Coding 图。

## 错误处理

### 正常结构化状态

- `not_found`：地理编码无候选；
- `ambiguous`：过滤后仍有多个候选；
- `ok`：成功取得时间或天气。

这些状态不产生 Tool Error，避免把预期对话分支当作系统故障。

### Tool Error

以下情况返回 error：

- JSON 参数无法解析；
- 日期不是 `today`、`tomorrow` 或严格 `YYYY-MM-DD`；
- 日期超出未来 7 天；
- `days` 超出 1 到 7；
- IANA 时区无效；
- Open-Meteo 请求超时或取消；
- Open-Meteo 返回非 2xx、超大响应或非法 JSON；
- 天气数组长度不一致；
- 计算表达式非法、过长、结果不是数值或结果非有限数。

Schema 错误继续由现有 Tool Middleware 映射为 `tool_invalid_arguments`。Tool 内部参数语义错误显式使用相同错误代码包装。HTTP 和响应错误映射为 `tool_runtime_failed`，Context 取消保持现有取消语义。

任何错误不得包含 Open-Meteo 原始响应正文、完整请求 URL、隐藏 Prompt、模型凭证或数据库凭证。

## 数据与 API 兼容性

- 不新增或修改数据库 Migration；
- `agent_conversations`、`agent_messages` 和 `agent_model_invocations` 继续作为权威数据；
- Tool Call 和 Tool Result 继续由现有 Conversation Runner 持久化；
- 不修改会话列表、消息详情、发送、SSE 或取消接口；
- 不修改 Cookie 用户身份和所有权校验；
- 不修改前端页面布局；
- Tool Start、Tool End 和失败继续通过现有 SSE 契约展示；
- 正常天气结果只进入模型上下文和持久化 Tool Result，不增加独立天气表。

## 安全边界

- Open-Meteo Client 只访问固定 HTTPS Host；
- 用户不能传 URL、经纬度、Header 或 HTTP 方法；
- 不把秘密写入 Workspace；
- 计算工具不提供对象环境、自定义函数、反射、文件、网络或进程访问；
- 所有 Tool 输入经过 JSON Schema 和严格 JSON 解码；
- 所有 Tool 输出继续受现有 50 KiB Runtime 上限保护；
- 地点查询只用于当前 Tool 调用，不建立用户位置档案；
- 独立子代理评测只读仓库，不调用 Open-Meteo、模型 API 或数据库。

## 性能与并发边界

- 三个新 Tool 沿用现有 Scheduler 的单轮最多 4 个并行 Tool 调用限制；
- Open-Meteo Client 可安全并发复用同一个 `http.Client`，但不创建无界 goroutine；
- 第一期不增加天气或地理编码缓存，每次调用只处理当前请求；
- 第一期不增加应用层限流，继续依赖现有本地 Web 服务边界和 Scheduler 并发上限；
- 外部请求不做重试，避免单次对话产生不可控的放大流量。

## 测试策略

### Skill RED-GREEN 对照

在创建每个 Skill 前记录最小基线场景。基线只使用独立子代理和当前 `AGENTS.md`，不向子代理泄漏期望答案。至少覆盖：

1. 信息充分的写作请求是否仍先长篇规划或反复提问；
2. 比较请求是否混淆事实、偏好和推测；
3. 简单概念请求是否过度讲解或强制测验；
4. 天气请求在没有真实结果时是否编造。

创建 Skill 后用新独立子代理重复等价场景，验证：

- Skill 能由描述正确触发；
- 子代理先完整读取对应 SKILL.md；
- 输出遵守 Skill 的正向结构；
- 不相关请求不读取 Skill；
- 组合请求只读取必要 Skill。

评测产生的临时输出放在 `/tmp`，不得写入 Workspace 或提交仓库。

### Skill Discovery

增加测试确认：

- `workspaces/chat` 恰好发现 4 个新 Skill；
- 名称、Location、描述和稳定排序正确；
- System Prompt 只包含 Skill Catalog 元数据，不包含 Skill Body；
- 现有 `read` 能完整读取四个 SKILL.md；
- 无关的仓库 `repository-development` Skill 不进入 Chat Catalog。

### Open-Meteo Client

使用 `httptest.Server` 覆盖：

- 地点查询参数编码和候选映射；
- 国家代码、一级行政区和时区映射；
- 天气请求字段和时区参数；
- 返回日期与请求区间不一致；
- WMO Code 映射；
- 非 2xx；
- 空结果；
- 畸形 JSON；
- 超过 1 MiB；
- 数组长度不一致；
- Context 取消；
- HTTP 超时。

测试不调用真实 Open-Meteo。

### Tool 单元测试

`get_weather`：

- 唯一地点；
- 无地点；
- 多地点歧义；
- 国家和行政区消歧；
- `today`、`tomorrow` 和显式日期；
- 时区跨日；
- 今天起 1 到 7 天，以及明天起最多 6 天；
- 起始日期合法但结束日期超出 7 天窗口；
- 超范围日期；
- 上游错误保持 Tool Error。

`get_current_time`：

- 固定 Clock 在不同时区的时间和日期；
- 无地点和歧义；
- 无效时区。

`calculate`：

- 加减乘除、括号、小数和幂；
- 空表达式和超过 256 字符；
- 语法错误；
- 非数值结果；
- NaN、无穷值和除零；
- 整数溢出；
- 标识符、函数、成员访问、集合和条件表达式均在执行前被拒绝。

### Fx 与 Loop

- Web Tool Runtime 恰好包含 `calculate`、`get_current_time`、`get_weather`、`read`；
- Web 不包含五个 Coding 工具；
- Open-Meteo Client 在 Fx 图中只创建一次；
- 模拟 Provider 调用天气 Tool、接收 Tool Result 并产生最终 Assistant 消息；
- `ambiguous` 结果可以进入下一次 Provider 调用；
- Direct Loop 不增加 Thinking Invocation。

### 全量验证

```bash
go test ./domain/... ./application/tool/... ./infrastructure/driver/openmeteo/... ./application/web/...
go test ./...
go test -race ./...
go build ./cmd/server
git diff --check
```

## 文档调整

更新 `README.md` 和 `docs/web-chat.md`：

- 列出 Web 默认的四个 Tool；
- 说明天气数据来自 Open-Meteo 且无需 API Key；
- 说明天气地点可能需要消歧；
- 说明没有网页搜索、提醒、长期记忆和在线训练；
- 说明 Skills 修改在下一次 Run 生效，Go Tool 修改需要重新构建和重启。

## 实施顺序

1. 运行四类 Skill 基线评测并记录失败行为；
2. 定义天气领域接口；
3. 用 `httptest.Server` 驱动实现 Open-Meteo Client；
4. 用单元测试驱动实现 `get_weather`；
5. 用固定 Clock 驱动实现 `get_current_time`；
6. 引入表达式库并用边界测试驱动实现 `calculate`；
7. 注册三个 Tool 并验证 Web Fx 能力集合；
8. 创建四个 Skill 并通过 Discovery 和读取测试；
9. 运行 Skill 回归评测；
10. 更新文档并执行全量、竞态和构建验证。

## 验收标准

- 用户可以询问今天、明天或未来 7 天的天气并得到真实工具结果；
- 用户可以按城市询问当地时间；
- 用户可以执行安全数值计算；
- 城市不存在或有歧义时 Agent 不擅自编造或选择；
- 写作、决策和学习请求能触发对应 Skill；
- 普通问候、翻译和简单总结不触发不必要 Skill；
- Chat Skill Catalog 不包含仓库开发 Skill；
- Web Tool Runtime 只有 `read` 和三个通用聊天 Tool；
- 不新增网页搜索、Coding 工具、数据库迁移或前端接口；
- Skill 前后对照、聚焦测试、全量测试、竞态测试、Server 构建和差异检查全部通过。
