# 加固版 edit_file 工具设计

日期：2026-07-30

## 目标

在不改变现有 Engine、Provider、Schema、配置加载和默认只读演示的前提下，为
`go-reagent` 增加模型可调用的 `edit_file` 工具。工具对工作区内已有文本文件执行一次
局部替换，并通过分级容错处理模型给出的换行或缩进偏差。

本次只增加 `edit_file`，不补充仓库当前不存在的 `write_file` 或 `bash`，也不在仓库根目录
加入供运行时自修改的 `server.go` 示例。真实修改场景由临时目录中的集成测试覆盖。

## 公共接口与组装

`EditFileTool` 实现现有 `tools.BaseTool`：

```go
type EditFileTool struct {
    root *os.Root
}

func NewEditFileTool(workDir string) (*EditFileTool, error)
func (t *EditFileTool) Name() string
func (t *EditFileTool) Definition() schema.ToolDefinition
func (t *EditFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error)
func (t *EditFileTool) Close() error
```

工具名为 `edit_file`，输入包含必填字符串字段 `path`、`old_text` 和 `new_text`，Schema 设置
`additionalProperties: false`。实际解析使用 `json.Decoder.DisallowUnknownFields`，并拒绝尾部
多余 JSON。`old_text` 不能为空；`new_text` 可以为空，以支持删除命中的文本。

`edit_file` 不设置 `ParallelSafe`，沿用零值 `false`。调度器因此把每次写操作作为独占屏障，
避免与其他工具调用并行修改工作区。

`cmd/reagent.registryForWorkDir` 创建并注册 `read_file` 与 `edit_file`。两个工具由一个组合
`io.Closer` 统一释放；任一构造或注册失败时，函数关闭此前已创建的资源并返回带上下文的错误。
默认 Prompt、`AGENT_PROMPT` 覆盖、Provider 配置和 Thinking 设置保持现状。

## 工作区与文件安全

构造函数将 WorkDir 规范化为绝对路径，并使用 Go 1.26 `os.OpenRoot` 建立能力边界。
`path` 去除首尾空白后必须是相对路径；绝对路径、卷路径、`..` 逃逸和指向 Root 外部的符号
链接由参数校验与 `os.Root` 共同拒绝。指向工作区内部普通文件的符号链接允许编辑其目标文件，
与现有 `read_file` 语义一致。

执行时通过 Root 以读写方式打开一次文件，并在同一个文件句柄上完成检查、读取和写回，避免
路径在读取与写入之间被替换。只允许普通文件。输入必须是合法 UTF-8 且不含 NUL 字节；不设置
额外文件大小上限，因为局部编辑需要看到完整文件才能证明匹配唯一，内存或读取错误直接返回。

成功匹配后先将新内容完整写入原文件，再截断遗留尾部。文件本身不重建，因此权限位保持不变；
内部符号链接也不会被替换。写入前后检查 Context。标准库普通文件写入不可在中途强制取消，
因此取消只保证在开始写回前或写回完成后被观察到。

## 四级唯一匹配

匹配器返回原始文件中的字节区间，而不是返回一个整体归一化后的文件。所有替换都只拼接
`original[:start] + replacement + original[end:]`，从而保留未命中区域的原始字节和换行符。

匹配按以下顺序执行：

1. **L1 精确匹配**：按原始字节查找 `old_text`。
2. **L2 换行等价匹配**：只把 CRLF 和 LF 视为等价，匹配结果映射回原始字节区间。
3. **L3 片段首尾空白容错**：对换行等价后的 `old_text` 执行 `TrimSpace`，再查找唯一的连续
   文本；文件中片段之外的空白不属于替换区间。
4. **L4 逐行缩进容错**：对 `old_text` 去除片段首尾空白后按行切分，在原文件中滑动窗口，
   比较每行 `TrimSpace` 后的内容，并把唯一命中的整组原始行内容作为替换区间。匹配区间不吞掉
   末行之后的换行符。

每一级遵循相同规则：零处命中才降到下一级；一处命中立即使用；多处命中立即返回歧义错误，
要求模型读取文件并提供更多上下文。空的 `old_text` 在匹配前拒绝，避免空字符串产生无意义的
多处命中。

替换文本中的换行符会转换为命中片段采用的换行风格；若命中片段不含换行，则使用文件的主要
换行风格，无法判断时保留 `new_text` 原样。这样 L2 到 L4 都不会顺带把整个 CRLF 文件改成 LF。

## 错误协议

错误信息保持面向模型、可自行纠正：

- 参数错误指出非法 JSON、未知字段、空字段或路径约束；
- 打开、检查、读取和写回错误带操作上下文但不泄露 WorkDir 绝对路径；
- 未命中建议先调用 `read_file` 确认内容；
- 歧义错误包含匹配数量并要求增加上下文；
- nil、未初始化或已关闭工具以及 nil/已取消 Context 明确返回错误。

成功结果固定包含相对路径，例如 `成功修改文件: cmd/reagent/main.go`。

## 测试策略

单元测试直接覆盖：

- Tool Definition、严格参数解析、空 `old_text` 和允许空 `new_text`；
- L1 精确、L2 CRLF/LF、L3 首尾空白、L4 忽略缩进；
- 每一级的唯一命中、重复命中和完全未命中；
- CRLF 保留、无换行片段采用文件主要风格、文件权限保持；
- 根目录与嵌套路径、内部符号链接、外部符号链接、`../`、绝对路径、目录和缺失文件；
- 非 UTF-8、NUL、nil/取消 Context、关闭后执行及无效 WorkDir。

CLI 集成测试通过 `registryForWorkDir(t.TempDir())` 验证发现结果按名称包含 `edit_file` 和
`read_file`，再经 Registry 对临时 Go 文件执行一次真实替换并读取磁盘结果。测试不调用真实模型，
也不修改仓库源码。

## 文件调整

```text
cmd/reagent/
├── main.go                  # 注册 read_file 与 edit_file，组合关闭资源
└── main_test.go             # Registry 发现与临时文件修改集成测试

internal/tools/
├── edit_file.go             # 工作区受限的四级唯一匹配编辑工具
├── edit_file_test.go        # 匹配、路径、文本、权限和取消测试
├── read_file.go             # 保持行为不变
└── registry.go              # 保持行为不变

README.md                    # 增加 edit_file 能力、安全边界和项目树说明
```

## 验收标准

- CLI 可发现并执行 `read_file` 和 `edit_file`，默认演示仍为只读任务；
- 四级匹配按顺序降级，任何层级都不接受多处命中；
- 替换只改变唯一命中的原始字节区间，未命中内容和文件权限保持不变；
- CRLF/LF 容错不会规范化整个文件；
- 工具不能编辑 WorkDir 外文件、非普通文件或二进制/非 UTF-8 文件；
- 不增加第三方依赖，不改变 Engine、Provider、Schema 和 Registry 接口；
- `go vet ./...`、`go test -race -count=1 ./...` 通过；
- `gofmt -l cmd internal` 无输出。
