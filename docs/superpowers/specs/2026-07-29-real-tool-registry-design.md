# 真实 Tool Registry 与 read_file 设计

日期：2026-07-29

## 目标

将 `cmd/reagent` 中的 Mock Weather Registry 替换成可注册、可发现、可路由的真实工具注册表，
并提供受工作区能力边界保护的 `read_file` 工具。Engine、Provider 和 Schema 的现有协议与
行为保持不变。

## 架构

Engine 继续只依赖最小只读执行接口：

```go
type Registry interface {
    GetAvailableTools() []schema.ToolDefinition
    Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult
}
```

具体工具实现统一接口：

```go
type BaseTool interface {
    Name() string
    Definition() schema.ToolDefinition
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

启动组装阶段使用扩展接口：

```go
type MutableRegistry interface {
    Registry
    Register(tool BaseTool) error
}
```

`NewRegistry` 返回 `MutableRegistry`。这样 Engine 无权修改注册表，现有 Engine fake 也不必
实现无关的注册方法。

## Registry 行为

默认实现持有 `map[string]BaseTool` 和 `sync.RWMutex`。

注册规则：

- 拒绝 nil 和 typed-nil 工具；
- 工具名称去除首尾空白后不能为空；
- `Definition().Name` 必须与 `Name()` 一致；
- 同名工具重复注册返回错误，不覆盖已有工具；
- 注册成功只记录工具名称。

发现规则：

- 在读锁下获取工具快照；
- 在锁外调用 `Definition`；
- 按 Definition 名称排序，确保 Provider Prompt 和测试稳定。

执行规则：

- nil 或已取消 Context 返回错误 ToolResult；
- 在读锁下完成路由后立即释放锁，不持锁执行工具；
- 未知工具返回错误 ToolResult；
- 工具返回的 error 转成错误 ToolResult；
- 工具 panic 在 Registry 边界恢复，模型只收到通用错误；
- panic 值不输出，日志只记录工具名和不含局部变量的调用栈；
- 所有结果保留原始 `ToolCallID`。

## read_file 能力边界

项目要求 Go 1.26，因此使用标准库 `os.OpenRoot` 建立工作区能力边界：

```go
type ReadFileTool struct {
    root *os.Root
}

func NewReadFileTool(workDir string) (*ReadFileTool, error)
func (t *ReadFileTool) Close() error
```

`os.Root` 自动拒绝绝对路径、`..` 逃逸和指向 Root 外部的符号链接，同时允许指向 Root
内部的符号链接，并在主流 Unix/Windows 平台避免手工路径检查的 TOCTOU 窗口。

构造函数必须成功打开 WorkDir；失败时 CLI 在启动阶段退出。CLI 在进程退出前关闭 Root。

## read_file 参数协议

工具名为 `read_file`，参数定义为：

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "相对于工作区的文件路径，例如 cmd/reagent/main.go"
    }
  },
  "required": ["path"],
  "additionalProperties": false
}
```

实际解析使用 `json.Decoder.DisallowUnknownFields`，拒绝非法 JSON、未知字段和尾部多余
JSON。`path` 去除首尾空白后不能为空，并明确拒绝绝对路径；`os.Root.Open` 作为最终强制
边界。

## 文件类型与输出预算

打开文件后使用文件句柄的 `Stat`，只允许普通文件，拒绝目录、设备和管道。

固定最大输出预算：

```go
const maxReadFileBytes = 8000
```

最多从文件读取 `maxReadFileBytes + utf8.UTFMax` 字节：

- 不超过 8000 字节时完整返回；
- 超过时向前寻找合法 UTF-8 边界并追加截断提示；
- 待读取窗口内的非 UTF-8 内容返回错误；
- 待读取窗口内包含 NUL 字节的疑似二进制内容返回错误；
- 截断提示固定为 `...[文件内容超过限制，已截断至前 8000 字节]...`。

在参数解析后、打开前、读取前和读取后检查 Context。由于物理读取上限约 8KB，本阶段不
引入异步 Reader 中断机制。

## CLI 组装

`cmd/reagent/main.go` 继续通过现有 `config.json` 创建 Provider，然后：

1. 获取当前 WorkDir；
2. 创建真实 Registry；
3. 创建并注册 `ReadFileTool`；
4. defer 关闭 ReadFileTool；
5. 关闭 Thinking，启动 AgentEngine；
6. 默认 Prompt 要求读取已存在的 `README.md` 并总结；
7. `AGENT_PROMPT` 继续允许覆盖默认任务。

删除 `cmd/reagent/mock_registry.go` 和 Mock Weather 测试。Provider 配置不退回旧的 API Key
环境变量模式。

## 文件调整

```text
cmd/reagent/
├── main.go                 # 挂载真实 Registry 和 read_file
└── main_test.go            # 保留配置测试，删除 Mock Weather 测试

internal/tools/
├── registry.go             # BaseTool、接口和线程安全默认实现
├── registry_test.go        # 注册、发现、路由、错误与 panic 测试
├── read_file.go            # os.Root 受限文本读取工具
└── read_file_test.go       # 路径、符号链接、类型、预算与取消测试
```

## 测试矩阵

Registry：

- 成功注册、发现与执行；
- 稳定排序；
- nil、typed-nil、空名、定义名不一致和重复注册；
- 未知工具、工具 error、取消 Context 和 panic 隔离；
- race 模式下并发发现与执行。

read_file：

- 根目录和嵌套文件；
- 内部符号链接允许、外部符号链接拒绝；
- `../`、绝对路径、目录和不存在文件；
- 非法 JSON、未知字段、尾部 JSON 和空路径；
- 取消 Context；
- 8000 字节边界、超限截断和中文 UTF-8 边界；
- 读取窗口内的非 UTF-8、NUL 内容和 Root 关闭后的读取。

## 验收标准

- CLI 只挂载真实 `read_file`，不再使用 Mock Weather Registry；
- 模型能够发现并调用 `read_file`；
- 工具不能读取 WorkDir 外部路径；
- 内部符号链接可读，外部符号链接拒绝；
- 大文件和二进制文件不能冲击模型上下文；
- 无效注册、工具错误和 panic 不击穿 Main Loop；
- 不增加第三方依赖；
- `go vet ./...`、`go test -race -count=1 ./...` 通过；
- `gofmt -l cmd internal` 无输出。
