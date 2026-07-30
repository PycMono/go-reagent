# 真实模型并发读文件集成测试设计

日期：2026-07-30

## 目标

新增一个按需运行的 Go 集成测试，使用项目现有配置创建真实大模型 Provider，让模型在同一个
Action 响应中一次性调用三次 `read_file`，分别读取 `a.txt`、`b.txt` 和 `c.txt`。测试必须以
确定性的同步信号证明三个读取在 Engine 中并发执行，并确认模型在收到全部工具结果后给出综合
说明。同时保留 `go-logger-sdk`、JSON 日志和当前模型，通过调整日志事件文案与阶段事件，使运行
输出在语义、顺序和关键文案上达到参考案例约 90% 的一致性。

## 非目标

- 不新增可执行 Demo，也不修改 `cmd/reagent/main.go` 的 Provider、模型或日志配置；
- 不 Mock Provider 或厂商 HTTP API；
- 不依赖仓库根目录当前未跟踪的 `a.txt`、`b.txt`、`c.txt`；
- 不把需要网络和 Token 的测试加入普通测试的强制执行路径；
- 不以执行时间缩短作为并发成立的证据；
- 不把 JSON 外壳、时间戳、Tool Call ID、Goroutine 启动顺序或模型自然语言逐字一致纳入 90%
  验收范围；
- 不恢复标准库 `log`，不切换为参考案例使用的智谱模型；
- 不新增仓库中不存在的 `write_file` 或 `bash` 工具来伪造挂载日志。

## 输出对齐契约

“90% 一致”按可控的日志事件契约衡量，不按整段输出字符相似度衡量。使用现有 JSON Logger，
保留 `component`、`turn`、`phase`、`tool_index`、`tool_call_id` 等结构化字段，同时让 `msg` 与
参考案例的阶段语义和核心文案对齐。

一次成功的三文件并发任务必须按顺序出现以下事件：

1. `[Registry] 成功挂载工具`，字段中包含 `tool=read_file`；
2. `[Engine] 引擎启动，锁定工作区`，包含 `work_dir`；
3. `[Engine] 慢思考模式 (Thinking Phase)`，包含 `thinking_enabled=true`；
4. `========== [Turn N] 开始 ==========`；
5. `[Engine][Phase 1] 剥夺工具访问权，强制进入慢思考与规划阶段`；
6. `🧠 [内部思考 Trace]` 用户可见输出；
7. `[Engine][Phase 2] 恢复工具挂载，等待模型采取行动`；
8. `🤖 [对外回复]`，仅在模型返回文本时出现；
9. `[Engine] 模型请求并发调用工具`，包含 `tool_count=3`；
10. 三条 `-> [Go-N] 🛠️ 触发并行执行`；
11. 三条 `-> [Go-N] ✅ 工具执行成功`，包含返回字节数；
12. `[Engine] 所有并发工具执行完毕，开始聚合观察结果 (Observation)`；
13. 下一轮重复 Thinking/Action 阶段；
14. `[Engine] 模型未请求调用工具，任务宣告完成`。

JSON 编码、字段顺序和日志 SDK 自带 Caller 不要求与纯文本参考输出一致。`Go-N` 使用模型原始
Tool Call 索引，Goroutine 实际开始和完成顺序允许变化。

独占工具不能被错误标成并发。Engine 的内部执行函数必须接收执行模式：并发波次使用“触发并行
执行”，独占调用使用“触发串行执行”。聚合日志在全部 Tool Call 完成、Observation 写回历史前
发出；三文件案例只有一个并发波次，因此与参考流程一致。

## 测试入口与启用方式

测试放在 `cmd/reagent/concurrency_integration_test.go`，使用 `package main`，从而复用生产入口的
Provider 组装逻辑，但所有测试辅助类型只存在于测试文件中。

测试默认调用 `t.Skip`，只有设置 `RUN_LLM_INTEGRATION=1` 时才访问真实模型：

```bash
RUN_LLM_INTEGRATION=1 go test ./cmd/reagent \
  -run TestRealModelReadsThreeFilesConcurrently -v
```

配置路径优先读取 `CONFIG_PATH`；未设置时根据测试源文件位置定位仓库根目录的 `config.json`。
测试使用配置中的当前平台、协议、Base URL、API Key 和模型，不复制 Provider 创建规则，也不在
失败日志中打印 API Key 或完整配置。由于 Go 会隐藏成功测试的标准输出，必须使用 `-v` 才能
查看完整流程；环境变量门禁继续保留，避免普通 `go test ./...` 消耗 Token。

集成测试在启用后显式安装 `newApplicationLogger()`，确保日志继续使用 `go-logger-sdk`、JSON
格式和 `module=go-reagent`，不会因为 `main()` 未执行而落到 SDK 默认的 `module=demo`。

## 测试文件夹具

测试使用 `t.TempDir()` 创建隔离工作区，并写入三个普通 UTF-8 文件：

- `a.txt`：前端报错日志；
- `b.txt`：后端接口响应时间；
- `c.txt`：今天是周五。

文件由测试结束时自动清理。仓库工作区现有的同名文件不会被读取、修改、暂存或提交。

## 组件设计

### Recording Provider

`recordingProvider` 实现现有 `provider.LLMProvider` 接口，并包装真实 Provider。每次 `Generate`
调用均原样转发，请求完成后仅保存响应的必要副本，最后原样返回结果。

记录用于在测试结束时检查：

- Thinking 阶段没有工具定义；
- 首个携带工具定义的 Action 响应包含三个 Tool Call；
- 三个调用均为 `read_file`，参数路径恰好覆盖三个目标文件且无重复；
- 工具 Observation 返回后，Provider 至少再被调用一次，并返回非空最终说明。

记录结构使用互斥锁保护，因为 Engine 运行在测试启动的 Goroutine 中，而测试控制逻辑会读取
记录用于超时诊断。

### Probing Registry

测试创建生产 `tools.ReadFileTool` 并挂载到生产 Registry，再使用 `probingRegistry` 包装该
Registry。包装器原样转发生产工具定义，因此保留 `read_file` 的名称、输入 Schema 和
`ParallelSafe: true` 元数据，仅在 `Execute` 前后增加同步探针：

1. 严格解析 `path` 参数，并将标准化后的文件名写入 `started` Channel；
2. 等待共享 `release` Channel 关闭，或在 Context 取消时立即返回；
3. 获准继续后调用真实 `ReadFileTool.Execute`，因此路径边界和文件读取行为仍使用生产实现；
4. 成功读取后将路径写入 `completed` Channel，供测试确认三个生产读取均已完成。

探针只允许 `a.txt`、`b.txt`、`c.txt`。重复路径、未知路径或无法解析的参数都会使测试失败，
并触发 Context 取消，防止留下阻塞的 Goroutine。

## 执行流程

测试使用带总超时的 Context，并按以下顺序运行：

1. 创建真实 Provider、临时文件、探针工具和只注册 `read_file` 的 Registry；
2. 构造 `AgentEngine`，保持 `EnableThinking=true`，并确认 `MaxParallelTools` 至少为 3；
3. 在 Goroutine 中调用 `Run`，Prompt 明确告诉模型 Thinking 阶段没有工具、不得假装读取或
   编造内容；进入 Action 后必须在同一响应中一次性并行读取三个文件；
4. 测试 Goroutine 等待 `started` 收到三个互不重复的目标路径；
5. 三个路径全部启动后关闭 `release`，允许生产读取逻辑继续；
6. 等待 Engine 完成模型的后续总结，并检查 Provider 记录、启动集合和最终响应。

如果 Engine 串行调度，第一个工具会停在 `release`，第二和第三个工具无法上报 `started`，测试
必然在并发启动阶段超时。因此“三个启动信号均在统一放行前到达”构成确定性的并发证据。

## 真实模型不确定性

真实模型可能没有遵守“一次性调用三个工具”的要求。这属于集成测试失败，而不是自动重试的
理由：重试会掩盖当前模型或 Prompt 无法稳定触发目标行为，并产生额外 Token 成本。

发生以下情况时测试直接失败并输出脱敏诊断：

- 模型没有请求工具；
- 模型只调用一个或两个文件；
- 模型分多个 Action 轮次读取文件；
- 模型调用重复路径、未知路径或非 `read_file` 工具；
- Provider、工具或 Engine 返回错误；
- 并发启动或完整运行超过超时。

诊断包含模型名、Tool Call 名称和参数、已观察到的启动路径以及阶段错误，但不包含 API Key。

## 超时与清理

测试具有两个层次的边界：较短的并发启动超时用于判断模型是否在同一轮发出三个正确调用，较长
的总 Context 超时覆盖 Thinking、Action、工具回传和最终总结。

任何失败路径都会取消 Context，并以幂等方式关闭 `release`，确保已经启动的工具调用能够退出。
真实 `ReadFileTool` 在测试结束时关闭，临时目录由测试框架清理。

## 断言范围

测试断言结构化行为，不断言完整自然语言：

- 真实 Provider 被成功创建并调用；
- 同一 Action 响应包含三个目标 `read_file` Tool Call；
- 三个调用在统一放行前均已进入工具执行层；
- 三个真实临时文件均被成功读取；
- Engine 将工具结果交回真实 Provider；
- 模型返回非空最终回复，并且流程正常结束。

文件含义的精确措辞不作为硬断言，避免不同模型或模型版本因同义表达导致无意义失败。Tool Call
集合和并发事实则必须严格满足，不做宽松匹配。最终回答额外要求同时出现 `a.txt`、`b.txt`、
`c.txt`，但不比较整段自然语言字符相似度。

## 文件调整

```text
cmd/reagent/concurrency_integration_test.go  # 真实 Provider 并发集成测试及测试专用包装器
internal/engine/loop.go                       # 对齐阶段、并发执行和聚合日志事件
internal/engine/loop_test.go                  # 验证结构化事件文案、字段与串并行模式
internal/tools/registry.go                    # 对齐工具挂载成功事件
internal/tools/registry_test.go               # 验证 Registry 日志契约
```

README、生产 Provider 和具体工具源码保持不变，不新增第三方依赖。

## 验收标准

- 未设置 `RUN_LLM_INTEGRATION` 时普通 `go test ./...` 不访问网络且保持通过；
- 显式启用时测试读取项目现有 Provider 配置并调用真实模型；
- 日志继续由 `go-logger-sdk` 以 JSON 输出，模型继续使用当前配置选项；
- 参考案例的 14 个关键事件按适用流程出现，关键 `msg` 和结构化字段有自动化测试；
- 并发调用显示 `Go-N` 和并行执行，独占调用不会被误标为并发；
- 集成测试输出使用 `module=go-reagent`，不再出现 SDK 默认 `module=demo`；
- 测试使用临时文件，不依赖或修改仓库中的同名文件；
- 测试能确定性区分并发调度与串行调度；
- 模型不满足同轮三调用要求时给出可诊断失败，不静默跳过或自动重试；
- 最终回答必须提及三个文件名，但不要求模型自然语言逐字一致；
- API Key 不进入测试输出、日志或提交内容；
- `go test ./...`、`go test -race ./...` 和 `go vet ./...` 通过。
