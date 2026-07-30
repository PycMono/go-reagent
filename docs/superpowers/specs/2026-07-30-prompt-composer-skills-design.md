# Prompt Composer 与 Skill 解析器设计

日期：2026-07-30

## 目标

为 go-reagent 增加工作区感知的 System Prompt 组装能力。每次 Agent Run 开始时，系统动态组合：

1. 与当前真实工具能力一致的最小核心身份和行为纪律；
2. 工作区根目录中的 `AGENTS.md`；
3. `.claw/skills/**/SKILL.md` 中定义的标准化 Agent Skills。

同时补齐终端 Reporter 的生命周期输出，便于在本地观察 Thinking、工具调用、工具结果和最终消息。

## YAML 解析方案

直接使用 `gopkg.in/yaml.v3` 解析 `SKILL.md` 的 YAML Frontmatter。该库已经是
`github.com/jinzhu/configor v1.2.2` 的传递依赖，本功能将其提升为 `go.mod` 中的直接依赖。

不直接使用 `configor.Load` 解析 Skill：Configor 面向完整配置文件，会附带环境变量覆盖、环境文件叠加和
example 文件回退等行为，这些语义不属于 Markdown Frontmatter。也不使用手写 `name:`/`description:`
字符串切割，以便正确支持 YAML 引号、注释和多行标量。

## Skill 数据模型与解析

`internal/context/skill.go` 定义：

```go
type Skill struct {
    Name        string
    Description string
    Body        string
}
```

解析规则如下：

- 默认名称为 `Unknown Skill`，默认描述为 `No description provided.`，默认正文为完整文件内容；
- 仅当文件第一行是独占一行的 `---` 时才尝试解析 Frontmatter；
- 结束标记也必须是独占一行的 `---`，正文中的 Markdown 分隔线不会被误切；
- 同时接受 LF 和 CRLF 换行；
- 使用 `yaml.v3` 解码 `name` 和 `description`，解码后去除字段首尾空白；
- 没有 Frontmatter 时保留默认元数据与完整正文；
- Frontmatter 开始但没有结束标记，或 YAML 无法解码时，将该文件视为无效 Skill，不注入 Prompt；
- 空的 `name` 或 `description` 使用对应默认值，正文允许为空。

解析函数保持包内可见，并返回错误，使扫描器能够区分普通无 Frontmatter 文档和损坏的 Frontmatter。

## Skill 扫描与渲染

`SkillLoader` 以工作区为边界，扫描 `<workDir>/.claw/skills` 下所有名为 `SKILL.md` 的文件。

- 目录不存在或无法访问时静默返回空字符串，保持 Agent 可用；
- 单个文件读取失败或解析失败时跳过该文件，继续加载其余 Skill；
- 使用 `filepath.WalkDir` 的词法顺序产生稳定输出；
- 只有至少一个有效 Skill 时才输出 `### 可用专业技能 (Agent Skills)` 区块；
- 不使用输出长度阈值判断是否加载成功；
- 每个 Skill 输出名称、触发条件和执行指南，Skill 之间使用 Markdown 分隔线分隔。

`LoadAll() string` 保持轻量、无错误返回值的调用契约。无效 Skill 的容错仅影响该文件，不阻止 Agent Run。

## Prompt Composer

`internal/context/composer.go` 定义 `PromptComposer`，持有工作区路径和 `SkillLoader`。
`Build()` 返回一条 `schema.Message`，Role 固定为 `schema.RoleSystem`。

System Prompt 按以下顺序组装：

1. 最小核心身份与纪律；
2. 根目录 `AGENTS.md`；
3. SkillLoader 渲染的技能区块。

核心 Prompt 使用真实项目名 `go-reagent`，并与当前 Registry 能力保持一致：

- 仅调用当前请求实际提供定义的工作区工具；
- 没有工具定义时处于 Thinking 阶段，只能规划，不得模拟工具调用、虚构文件内容或声称工具已执行；
- 修改文件前先读取并理解现有内容；
- 工具失败时根据真实错误调整后重试；
- 获得工具结果后，以真实 Observation 为依据完成回答；
- 始终使用中文回复。

核心 Prompt 不要求调用当前不存在的 `bash`、`write_file`、`ls` 或 `test -f` 工具。

若根目录 `AGENTS.md` 可读，Composer 将其放入独立的“项目专属指南”区块；缺失或不可读时静默略过。
Composer 不缓存文件内容，保证不同 Run 之间对 `AGENTS.md` 和 Skills 的修改即时生效。

## Engine 集成

`AgentEngine` 新增私有 `composer *context.PromptComposer` 字段。`NewAgentEngine` 使用现有 `workDir`
初始化 Composer。`Run` 中用 `e.composer.Build()` 替换当前硬编码的 System Prompt，并继续将用户输入作为第二条消息。

Thinking/Action 两阶段、历史消息追加、工具调度、并发控制、结构化日志和 Reporter 回调均保持不变。

## Terminal Reporter

保留 `NewTerminalReporter()` 作为现有调用入口，补齐四类终端输出：

- Thinking：显示模型正在推理；
- Tool Call：显示工具名和经过转义、截断的参数；
- Tool Result：显示成功或失败，失败时附带错误文本；
- Message：显示非空 Agent 回复。

参数中的换行和回车分别显示为 `\n` 和 `\r`。超长参数按 rune 截断，避免破坏中文 UTF-8。
Reporter 使用互斥锁将单个事件作为完整输出写入，避免并发工具回调互相穿插。Reporter 不改变 Engine
的错误传播规则。

## 文件调整

```text
internal/context/skill.go                  # Skill Frontmatter 解析、扫描与渲染
internal/context/skill_test.go             # Skill 解析、容错、顺序与格式测试
internal/context/composer.go               # System Prompt 动态组装
internal/context/composer_test.go          # 核心、AGENTS.md、Skills 组合测试
internal/engine/loop.go                     # 注入并使用 PromptComposer
internal/engine/loop_test.go                # 验证 Engine 首条请求使用动态 Prompt
internal/engine/terminal_reporter.go        # 补齐终端生命周期输出
internal/engine/terminal_reporter_test.go   # 输出、错误和 UTF-8 截断测试
go.mod                                      # yaml.v3 提升为直接依赖
go.sum                                      # go mod tidy 后的依赖校验和
```

## 测试策略

采用测试驱动开发：每项行为先写失败测试并确认失败原因，再实现最小代码使其通过。

Skill 测试覆盖：

- 普通 YAML Frontmatter；
- 引号、注释和多行 description；
- CRLF；
- 无 Frontmatter；
- 正文中的 `---`；
- 未闭合或无效 YAML；
- 多目录扫描的稳定顺序；
- 不存在的 Skill 目录；
- 有效与无效 Skill 混合时继续加载有效项。

Composer 和 Engine 测试覆盖：

- 基础 Prompt 的身份、真实工具纪律和 Thinking 约束；
- `AGENTS.md` 存在与缺失；
- Skills 存在与缺失；
- Engine 发给 Provider 的第一条消息来自 Composer，第二条为用户输入；
- 修改工作区文件后，新一次 Run 能读取最新内容。

Terminal Reporter 测试覆盖四类事件、空消息、参数换行转义、UTF-8 安全截断和失败结果展示。

## 验收标准

- `.claw/skills/**/SKILL.md` 能通过标准 YAML Frontmatter 加载并稳定注入 System Prompt；
- 单个损坏 Skill 不阻止其余 Skill 加载，也不阻止 Agent Run；
- 根目录 `AGENTS.md` 在存在时注入，在缺失时不产生占位内容；
- Engine 不再维护硬编码 System Prompt，且现有主循环行为不回归；
- 核心 Prompt 不引用未注册的工具；
- Terminal Reporter 能完整展示四类生命周期事件，并发输出不穿插；
- `gofmt -l cmd internal` 无输出；
- `go vet ./...` 通过；
- `go test -race -count=1 ./...` 通过。

## 不在本期范围

- Skill 热监听或单次 Run 中途重新加载；
- 递归加载 Skill 正文引用的其他文件；
- Skill 优先级、启停配置、冲突消解或按需选择性注入；
- 将 Skill 解析错误升级为 Agent 启动错误；
- 新增 `bash`、`write_file` 或其他工作区工具；
- 修改 Provider、消息 Schema、工具调度或企业微信 Reporter。
