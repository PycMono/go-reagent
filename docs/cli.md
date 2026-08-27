# go-reagent CLI 多轮对话入口（最小版）

> 目标：一个开发者自用的终端 REPL，复用 `pi` 无状态 Runner 与公开 Registers，
> 验证 provider 配置、工具装配与多轮对话。不做持久化、不做 TUI、不改 `pi/`。

## 1. 范围

做：

- 终端多轮对话（REPL），历史进程内持有，跨轮累积；
- `-prompt "..."` 单轮模式，与 REPL 共用同一 runTurn；
- `-dir` 指定工作区（支持 `-dir .`）；
- 工具授权开关：默认只读，`--allow-write` / `--allow-exec` / `--yolo` 按需叠加；
- `-subagent` 可选挂载子代理；MCP 随配置文件模式自动挂载；
- stdout 只出模型正文，日志与进度走 stderr；
- Ctrl-C 取消当轮 / 退出进程。

不做（明确砍掉）：

- 会话落盘与断点续传（原 M3 全部：原子写、锁文件、损坏恢复——开发者工具不需要）；
- `/history` `/mode`、裁剪告警、80% 提示等装饰功能；
- 进程级可观测性（不装配，SDK 全局 Noop 空转）；
- DB、SSE、审批通知（服务端职责）。

## 2. 装配

```text
cmd/cli ──► resolveRuntime()  # §3：配置文件 → config.Load；无 → 环境变量默认
              │  产出 RuntimeConfig（统一 fx.Supply）；配置模式另 fx.Supply(cfg) 并
              │  fx.Provide(config.NewLoopDetectionConfig, config.NewExtraToolHandlers)
              ├─ pi.CoreRegister
              ├─ 工具档（见下）
              ├─ mcpdriver.Register（配置模式默认挂载，无启用项时为空扩展）
              └─ -subagent 时追加 pi.SubagentRegister
                 （仅配置模式；内置子代理固定依赖 MCP 的 web_search_exa/web_fetch_exa，
                 配置未启用对应 MCP 即启动 fail-fast）
```

工具档按两个布尔能力装配，**每个构造器只注册一次**（叠加即 fx 重复 Provider）：

```text
write = --allow-write || --yolo
exec  = --allow-exec  || --yolo
```

- 基础：`pi.ReadOnlyToolsRegister`
- write：追加 `tools.NewEditTool/NewWriteTool/NewApplyPatchTool`
  （仿 `pi.CodingToolsRegister` 的 `fx.Annotate(..., group:"agent_tools")` 写法）
- exec：追加 `tools.NewProcessSupervisor/NewExecTool/NewProcessTool`
- write && exec 与 `--yolo` 等价（两个 allow 开关同传也到达该状态）

授权横幅一行打到 stderr，说明当前档位；配置模式另打一行当前启用的 MCP 服务器；
exec 档附一句宿主机权限警告。

## 3. 配置：有配置文件用配置，没有就走环境变量默认

**选择规则**：显式设置了 `CONFIG_PATH` 但加载失败（不存在/损坏/校验不过）→
**报错退出**，不得悄悄回退；只有未设置 `CONFIG_PATH` 且默认 `config.json`
不存在时，才进入环境变量模式。

CLI 自己解析出统一的 `RuntimeConfig（providers.Options + WorkDir +
CompactionConfig + Limits）`，`fx.Supply` 进图，两种来源对 fx 完全同构：

**A. 配置文件模式**：复用 `config.Load`（**仍校验 Redis/MySQL/MCP 等完整服务
配置**，本模式要求本机配置齐全），取 `CurrentPlatformOptions()` /
`NewCompactionConfig` / `Agent.WorkspaceDir` / `Agent.Limits`
（`-max-turns` 显式传入正数时覆盖配置中的 MaxTurns）。

- `-dir` 注入：加载前 `os.Setenv("CONFIGOR_AGENT_WORKSPACEDIR", dir)`，让 config
  校验的对象就是最终工作区（确切 env 名用 config 包测试钉死；测试用 `t.Setenv`
  自动恢复，不污染全局环境）；
- 加载前 `os.Unsetenv("CONFIGOR_DEBUG_MODE"/"CONFIGOR_VERBOSE_MODE")`，
  防止 configor 向 stdout 打印；Load 完成后恢复原值；
- **唯一跨包改动**：`config.Load` 新增选项 `WithAllowProcessCWD()`，跳过
  `resolveAgentWorkspaceDir` 的 cwd 拒绝（该保护针对服务端部署目录，对 CLI
  不成立）；`cmd/server` 不传，行为不变；
- fx 图除 RuntimeConfig 外**必须同时 `fx.Supply(cfg)`**：
  `mcpdriver.Register` 的 `NewExtensions` 直接依赖 `*config.Config`
  （`infrastructure/driver/mcp/mcp.go`），缺它配置模式启动即 fx 缺依赖失败；
- 配置中的治理策略**不得静默忽略**：装配图同时加入
  `config.NewLoopDetectionConfig` 与 `config.NewExtraToolHandlers`
  （permissions deny / retry / timeout 均由此进入中间件链——用户配置的 deny
  规则被跳过会造成安全预期偏差）。环境变量模式无 `*config.Config`，
  对应行为为 pi 默认（循环护栏默认启用、无扩展 Handler）。

**B. 环境变量模式（无配置文件）**：不碰 `config` 包，零配置可用：

| 环境变量 | 说明 |
|---|---|
| `ANTHROPIC_API_KEY` 或 `OPENAI_API_KEY` | 必填二选一，决定 protocol；同时存在时 anthropic 优先 |
| `REAGENT_MODEL` | 作用于选中的协议；缺省 anthropic=`claude-sonnet-4-6`、openai=`gpt-5` |
| `REAGENT_BASE_URL` | 缺省 anthropic=`https://api.anthropic.com`、openai=`https://api.openai.com/v1` |

其余内置默认：provider ID = 协议名；Pricing **零值对象（非 nil）**，成本显示 0；
ContextWindowTokens 0（主动压缩关闭）；Limits 直接给零值——pi 契约中零值会由
governor 自动回填 `DefaultLimits()`（MaxTurns=20 / MaxCostUSD=$1 / MaxTotalTokens=2M，
`pi/governor/types.go`），**Limits 不表达"不限制"，预算始终存在**；
`-max-turns N` 仅接受正数覆盖默认值，0 与未传等价（用 `flag.Visit` 区分仅是
解析需要，语义相同）；WorkDir = `-dir` 或进程 cwd（无 config 校验，`-dir .`
天然可用）；无 MCP/子代理（无 `*config.Config`，MCP 与 `-subagent` 在本模式均不可用）。

启动前做一次轻量 AGENTS.md 预检（存在、是普通文件、合法 UTF-8、非空），
避免首轮 Run 才报错；内容细节仍由 pi 把关。

## 4. 会话与历史（核心，全部在 session.go）

`pi.Runner` 无状态，CLI 持有 `[]pi.Message` 逐轮累积。每轮：

```go
result, err := runner.Run(ctx, pi.RunRequest{History, Input, Limits}, listener)
// 提交规则（保证历史永远是完整的 user+assistant 对）：
//   从 NewMessages 提取最终 assistant 纯文本
//   （Role==assistant、无 ToolCalls/ToolCallID/ToolName、非 IsError，
//    ai.TextContent 提取；与 conversation/mapper.go 同口径）。
//   提取到 → 追加 user + assistant；
//   提取不到（出错/取消/预算终止停在工具轮）→ 不提交历史，
//   stderr 提示"本轮未产生最终回答，工作区可能已被工具修改"。
// Termination.Totals 无条件累加（与历史提交无关），stderr 打一行终止摘要。
```

- 历史上限 `--history-limit N`（默认 100，<2 报错）：超出时从头部成对删除
  （user+assistant 一起删，不留孤儿消息；N 为奇数时实际保留向下取偶数条）；
- `/new` 清空历史与用量。

## 5. REPL 与信号

REPL：`bufio` 读行，空行忽略；单行超 256 KiB 拒绝并**丢弃该行剩余内容**
（不把尾部当作下一条输入）；`/exit`（Ctrl-D 同效）、`/new`、`/help`，其余视为输入。
`-prompt` 模式执行一次 runTurn 后退出；`-prompt ""`（用 `flag.Visit` 与未传区分）
报错退出。
失败与退出码：REPL 单轮失败（provider 错误等）打 stderr **继续会话**，不退出；
`-prompt` 单轮失败退出 1；`-prompt` 被用户取消退出 130；正常退出 0；
启动失败（含 fx 装配）1；强退 130。

信号：一个 `signal.Notify` goroutine + mutex 保护的当前 run cancel 函数：

- 运行中 SIGINT → cancel 当轮（按 §4 规则提交），回到提示符；
- 空闲 SIGINT → 正常退出流程；
- SIGTERM（任意态）→ 先 cancel 当前 Run（若在运行）再走退出流程；
- `-prompt` 模式任意信号 → cancel 当轮后直接走退出流程（无提示符可回）；
- 3 秒内第二次 SIGINT → `os.Exit(130)`（**唯一允许跳过 `app.Stop` 的强制例外**，
  后台进程可能来不及回收，stderr 明示）；
- Run 结束/取消时若 listener 的 `stdoutLineOpen` 仍为 true（§6），
  先向 stdout 补一个换行再回提示符。

退出必须走 `app.Stop`（5s 超时；触发 `ProcessSupervisor.OnStop` 回收后台进程），
Stop 失败以退出码 1 结束；除二次 SIGINT 外，禁止 Start 成功后
`log.Fatalf`/`os.Exit` 直接退出。

## 6. 输出

实现 `pi.EventListener`，一把 `sync.Mutex` 保护写入：

| 事件 | 去向 |
|---|---|
| `message_update` 文本 delta | stdout 流式 |
| `message_end` | stdout 换行 |
| `tool_start` / `tool_end` | stderr 一行 |
| 其余 | 忽略 |

`tool_end` 事件本身无时长字段：listener 在 `tool_start` 时以 `ToolCall.ID` 为键
记录开始时间（与终端写入同一把 mutex），`tool_end` 取差值并删除记录。

换行以 **`stdoutLineOpen` 布尔**判定（不能数 `message_end`：一轮 Run 可能有多个
assistant 消息，工具轮完成后下一个流式被取消时，已收到过 `message_end` 但最新
delta 仍未换行）：写出一个非空文本 delta → true；写出换行 → false；
Run 结束/取消时仍为 true → 补换行。

CLI 启动第一步 `logsdk.SetLogger` 安装 stderr logger。**注意 go-logger-sdk v1.0.6
的 `NewLogrus` 硬编码写 stdout 且级别固定 Trace，不可复用**；CLI 自实现
`logsdk.Logger`（6 个方法，约 30 行）写 stderr，默认 Warn，`-v` 为 Info。
保证 stdout 只有模型正文，管道可用。

## 7. 文件与工作量

```text
cmd/cli/
├── main.go      # flags、resolveRuntime（§3 两种配置来源）、fx 装配、shutdown 唯一出口
├── session.go   # runTurn、历史提交与裁剪、用量累计
├── repl.go      # REPL 循环、/命令、信号处理
├── listener.go  # EventListener 终端输出
└── logger.go    # stderr 版 logsdk.Logger（约 30 行）
```

约 500–600 行。`config` 包加 `WithAllowProcessCWD`（约 10 行 + 测试，仅配置模式需要）。

## 8. 测试

- 历史提交规则全分支（含"无最终文本不提交"）+ 成对裁剪（session 单测）；
- `fx.Populate(&executor)`（`toolexec.Executor`，`Definitions()` 为 freeze 后全集）
  断言四种开关组合的最终工具集合（write/exec 布尔矩阵）；
- 配置模式必须实际 `app.Start`/`Stop`（`fx.ValidateApp` 覆盖不了启动期 MCP
  注册与 Registry freeze），断言 Exa 工具在册；`-subagent` 追加后断言子代理
  绑定成功；
- resolveRuntime：无配置文件 + env 构造出合法 Options；anthropic 优先；缺 key 报错；
  显式 CONFIG_PATH 失败必须报错、不回退；Limits 零值经 governor 回填 DefaultLimits；
- flags：`-max-turns` 未传/0/正数/负数；`-prompt` 未传/空串/正常（`flag.Visit` 区分）；
- config：`WithAllowProcessCWD` 放行 cwd、默认仍拒绝（回归）；`-dir` env 名钉死；
- listener：`stdoutLineOpen` 覆盖"工具轮完成、下一轮流式中被取消"场景；
  自定义 logger 的级别过滤与 stdout 零污染；
- 信号路径 `go test -race`：运行态/空闲态 SIGINT、SIGTERM、二次 SIGINT 强退、
  -prompt 模式信号；退出码逐路径断言（0/1/130）；
- 手工验收：多轮记忆（第二轮引用第一轮内容）、默认档无写/执行工具、
  stdout 重定向纯净、`-dir .` 可启动、运行中 Ctrl-C 回提示符、
  无配置文件仅设 `ANTHROPIC_API_KEY` 可启动。
