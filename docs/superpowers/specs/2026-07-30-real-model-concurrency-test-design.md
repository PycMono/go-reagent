# 真实模型并发读文件集成测试设计

日期：2026-07-30

## 目标

新增一个按需运行的 Go 集成测试，使用项目现有配置创建真实大模型 Provider，让模型在同一个
Action 响应中一次性调用三次 `read_file`，分别读取 `a.txt`、`b.txt` 和 `c.txt`。测试必须以
确定性的同步信号证明三个读取在 Engine 中并发执行，并确认模型在收到全部工具结果后给出综合
说明。

## 非目标

- 不新增可执行 Demo，也不修改 `cmd/reagent/main.go` 的生产行为；
- 不 Mock Provider 或厂商 HTTP API；
- 不依赖仓库根目录当前未跟踪的 `a.txt`、`b.txt`、`c.txt`；
- 不把需要网络和 Token 的测试加入普通测试的强制执行路径；
- 不以执行时间缩短作为并发成立的证据；
- 不验证某个厂商模型输出的精确自然语言措辞。

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
失败日志中打印 API Key 或完整配置。

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

### Probed Read File Tool

测试创建生产 `tools.ReadFileTool`，再使用 `probedReadFileTool` 包装它。包装器保持生产工具的
名称、输入 Schema 和 `ParallelSafe: true` 元数据，仅在 `Execute` 前增加同步探针：

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
3. 在 Goroutine 中调用 `Run`，Prompt 明确要求模型在同一个 Action 中一次性并行读取三个文件，
   然后综合说明各自记录的领域或信息；
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
集合和并发事实则必须严格满足，不做宽松匹配。

## 文件调整

```text
cmd/reagent/concurrency_integration_test.go  # 真实 Provider 并发集成测试及测试专用包装器
```

README 和生产 Provider、Engine、Registry、工具源码均保持不变，不新增第三方依赖。

## 验收标准

- 未设置 `RUN_LLM_INTEGRATION` 时普通 `go test ./...` 不访问网络且保持通过；
- 显式启用时测试读取项目现有 Provider 配置并调用真实模型；
- 测试使用临时文件，不依赖或修改仓库中的同名文件；
- 测试能确定性区分并发调度与串行调度；
- 模型不满足同轮三调用要求时给出可诊断失败，不静默跳过或自动重试；
- API Key 不进入测试输出、日志或提交内容；
- `go test ./...`、`go test -race ./...` 和 `go vet ./...` 通过。
