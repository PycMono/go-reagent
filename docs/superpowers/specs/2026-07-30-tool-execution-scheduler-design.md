# 工具执行分级与有界并发调度设计

日期：2026-07-30

## 目标

将 AgentEngine 同一 Action 轮次中的工具执行从完全串行升级为分级调度：明确声明并发安全的
连续工具调用在固定上限内并发执行；未声明或具有副作用的工具保持串行，并作为前后并发波次
之间的执行屏障。

调度器必须保持模型给出的 ToolCall 顺序来组织 Observation，同时限制并发量、响应 Context
取消，并保留现有的工具错误自愈流程。

## 非目标

- 不推断 ToolCall 参数之间的数据依赖；
- 不向 ToolCall 协议增加 `depends_on` 或实现 DAG 调度；
- 不把 `bash` 命令解析成只读或写入类别；
- 不修改 Provider 请求和响应格式；
- 不在本阶段新增 `write_file`、`edit_file`、`bash` 或网络工具。

若后一个调用需要前一个调用的输出才能确定参数，模型仍必须把它们拆成不同 Action 轮次。

## 并发安全元数据

在 `schema.ToolDefinition` 增加 Harness 内部调度元数据：

```go
type ToolDefinition struct {
    Name         string      `json:"name"`
    Description  string      `json:"description"`
    InputSchema  interface{} `json:"input_schema"`
    ParallelSafe bool        `json:"parallel_safe,omitempty"`
}
```

规则如下：

- `ParallelSafe: true` 表示同一波次的其他安全工具可以同时执行该工具；
- 默认值 `false` 表示独占执行；
- 未知工具在调度阶段没有 Definition，按 `false` 处理，再由 Registry 返回未知工具错误；
- Provider 的工具转换只发送名称、描述和输入 Schema，`ParallelSafe` 不进入厂商 API；
- `read_file` 使用并发安全的 `os.Root`，Definition 显式设置 `ParallelSafe: true`；
- 未来的写入、编辑、Shell 等副作用工具默认不设置该字段。

默认串行是刻意的保守策略：新增工具即使忘记声明，也不会被错误地并行执行。

## Engine 配置

`AgentEngine` 增加公开配置：

```go
// MaxParallelTools 限制单个并发安全波次中同时执行的工具数量。
// 小于等于 0 时退化为串行执行。
MaxParallelTools int
```

`NewAgentEngine` 不增加参数，保持现有调用方兼容，并将默认值设置为 `4`。调用方可在构造后按
资源预算调整该字段。

## 分波调度算法

Action Response 的所有 ToolCall 仍先通过现有 `validateToolCalls` 整批校验。只有整个调用列表
合法时才进入执行阶段。

调度器根据本轮 `availableTools` 建立 `工具名 -> ParallelSafe` 映射，并按模型原始顺序扫描：

1. 连续的 `ParallelSafe` 调用组成一个并发波次；
2. 遇到非安全或未知工具，先等待当前并发波次全部完成；
3. 非安全工具单独执行，完成后才继续扫描；
4. 后续连续安全工具形成新的并发波次；
5. Context 取消后停止启动新调用和后续波次。

示例：

```text
read A ─┐
read B ─┴─ wave 1，最多 MaxParallelTools 个同时执行
write C ─── barrier，独占执行
read D ─┐
read E ─┴─ wave 2
bash F  ─── barrier，独占执行
```

当前仓库只有 `read_file` 被标记为安全，因此多个读取可以并发。即使未来挂载的副作用工具没有
及时声明元数据，也会安全地落入 barrier 分支。

## 并发波次实现

每轮预分配与 ToolCall 数量相同的 `[]schema.Message`。每个调用只写自己对应的索引，因此不需
要为 Observation 切片加 Mutex；WaitGroup 完成构成主循环读取结果前的同步边界。

安全波次使用 `sync.WaitGroup` 和容量为有效并发上限的 semaphore channel：

- 每个 ToolCall 启动一个短生命周期 Goroutine；
- Goroutine 在进入 Registry 前获取 semaphore；
- 获取前后都检查 Context，取消后不再调用 Registry；
- `defer` 释放 semaphore 并调用 `wg.Done`；
- `wg.Wait` 后检查 Context，再决定是否进入下一波；
- 单个工具返回 `IsError` 只生成错误 Observation，不取消兄弟调用。

有效并发上限为 `min(MaxParallelTools, waveSize)`；`MaxParallelTools <= 0` 时使用 `1`。

Registry 自身已经允许并发发现和路由，并且 `read_file` 使用的 `os.Root` 可被多个 Goroutine
安全调用。测试 fake 必须增加锁或使用专用并发 Registry，避免测试代码自身产生数据竞争。

## Observation 顺序

工具的物理完成顺序不进入对话协议。每个结果始终写到原 ToolCall 的索引，全部完成后使用：

```go
contextHistory = append(contextHistory, observationMsgs...)
```

因此 Provider 下一轮看到的 ToolResult 顺序与模型上一轮生成的 ToolCall 顺序严格一致。这个
顺序保证只影响上下文确定性，不代表并发工具按该顺序完成。

## Context 与错误语义

- Action 开始前已取消：保持现有行为，不执行工具；
- 并发波次中取消：已经进入 Registry 的调用由各工具和 Registry 响应 Context，等待它们退出；
- 尚未获取执行许可的调用不进入 Registry；
- 当前波结束后 `Run` 返回包装后的 Context 错误，不启动后续 barrier 或波次；
- Registry/工具错误只作为对应 Observation 返回，不中断未取消的同批调用；
- 若具体工具忽略 Context，Engine 无法强制停止该函数，只能等待它返回。

## CLI 演示

`cmd/reagent/main.go` 保留配置驱动的 Provider、真实 Registry 和 `AGENT_PROMPT` 覆盖。默认情况下：

- 将 `EnableThinking` 从 `false` 改为 `true`，鼓励模型一次规划多个独立读取；
- 默认 Prompt 要求同时读取仓库中已有的 `README.md`、`go.mod` 和 `cmd/reagent/main.go`，再总结
  三者分别描述的内容；
- 不创建 `a.txt`、`b.txt`、`c.txt` 演示文件；
- 不注册仓库中尚不存在的工具。

## 文件调整

```text
cmd/reagent/main.go                 # 开启 Thinking，更新并行读取演示任务
internal/schema/message.go       # 增加 ParallelSafe 元数据
internal/schema/message_test.go  # 验证元数据 JSON 行为
internal/tools/read_file.go      # 标记 read_file 并发安全
internal/tools/read_file_test.go # 验证安全标记
internal/engine/loop.go          # 有界分波调度与稳定 Observation 聚合
internal/engine/loop_test.go     # 并发、上限、屏障、顺序、取消和 race 测试
README.md                        # 更新 Main Loop、能力列表和运行示例说明
```

Provider 与 Registry 源码保持不变，不增加第三方依赖。

## 测试策略

测试不以“执行耗时变短”作为并发证据，而使用 Channel 屏障观察真实执行状态：

- 三个安全调用必须在释放任一调用前全部进入 Registry，证明不是串行；
- 工具按逆序完成后，下一轮 Provider 仍收到原始顺序的 Observation；
- `MaxParallelTools = 2` 时，第三个安全调用必须等前两个之一释放后才能进入；
- 安全调用、独占调用、安全调用形成三个严格有序的执行阶段；
- 未知工具按独占调用处理并得到 Registry 错误；
- 一个安全调用失败不阻止同波其他调用；
- `MaxParallelTools <= 0` 退化为串行；
- Context 在第一阶段取消后，后续阶段不进入 Registry；
- 单工具、Thinking、多轮 Action 和 ToolCall ID 整批校验保持原行为；
- 并发测试 fake 的共享字段使用锁保护，并通过 race detector 验证。

所有死锁保护使用测试超时作为失败护栏；并发断言由 Channel 状态和屏障决定，不依赖比较运行
耗时。

## 验收标准

- 连续 `read_file` 调用在同一 Action 轮内最多 4 个并发执行；
- 独占和未知工具成为前后波次的完整执行屏障；
- Observation 顺序始终与 ToolCall 顺序一致；
- 并发上限和取消行为有确定性自动化测试；
- Provider、Registry 和工具调用协议保持兼容；
- `go vet ./...` 通过；
- `go test -race -count=1 ./...` 通过；
- `gofmt -l cmd internal` 无输出。
