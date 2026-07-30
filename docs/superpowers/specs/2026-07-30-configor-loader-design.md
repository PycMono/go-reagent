# Configor 配置加载器设计

日期：2026-07-30

## 目标

使用 `github.com/jinzhu/configor` 最新发布版本全面替换 `internal/config` 中手写的文件打开和 JSON
Decoder 逻辑。经 Go Module Proxy 确认，最新版本为 `v1.2.2`。

## 加载行为

`Load` 直接调用 Configor 原生入口：

```go
var cfg Config
if err := configor.Load(&cfg, path); err != nil {
    return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
}
```

不配置 `ErrorOnUnmatchedKeys`、`ENVPrefix`、`Environment`、`Silent` 或其他兼容选项。项目接受
Configor v1.2.2 的原生行为：

- 根据扩展名加载 JSON、YAML 或 TOML；
- 自动加载当前 Environment 对应的叠加文件；
- 配置文件缺失时尝试同名 `example` 文件；
- 使用 `CONFIGOR_` 前缀的 Shell 环境变量覆盖字段；
- 未知字段默认忽略；
- JSON Decoder 只读取第一个 JSON 值，不额外拒绝尾部文档。

删除 `os.Open`、`json.NewDecoder`、`DisallowUnknownFields`、`requireJSONEnd` 及对应 import。

## 数据结构

`Config` 和 `PlatformConfig` 增加等价的 `yaml`、`toml` tag，同时保留现有 `json` tag：

```go
CurrentPlatform string `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
```

其他字段按相同规则标注。字段名称和对外 Config API 不变。

## 业务校验

Configor 只负责配置来源、解析、叠加和环境覆盖。以下现有业务行为继续由项目代码负责：

- 字符串去除首尾空白，Protocol 转小写；
- `currentPlatform` 和 `platforms` 非空；
- Platform ID 非空且唯一；
- Protocol 仅允许 `openai`、`anthropic`；
- BaseURL 必须是无用户信息、查询和片段的 HTTP/HTTPS URL；
- Model 非空；
- 当前平台必须存在并配置 API Key；
- 错误不得输出 API Key 内容。

## 测试调整

删除“未知字段必须报错”和“JSON 尾部内容必须报错”用例，因为它们与 Configor 默认行为冲突。

新增测试：

- JSON 仍能完成现有规范化和当前平台选择；
- YAML 文件可加载；
- TOML 文件可加载；
- `CONFIGOR_CURRENTPLATFORM` 可覆盖当前平台；
- `config.test.<ext>` 在测试进程中覆盖基础文件；
- 基础文件缺失时可使用 `config.example.<ext>`；
- 现有 URL、协议、重复 ID、缺失字段和凭据防泄漏用例继续通过。

测试环境变量必须使用 `t.Setenv` 隔离，环境叠加和 example 文件均放在独立的 `t.TempDir`。

## 文档与依赖

- `go.mod` 增加 `github.com/jinzhu/configor v1.2.2`；
- `go mod tidy` 记录 Configor 的 TOML/YAML 传递依赖；
- README 将配置格式从 JSON 扩展为 JSON/YAML/TOML，并说明 Environment、example 和
  `CONFIGOR_` 环境覆盖；
- `CONFIG_PATH` 仍只负责选择基础配置路径。

## 文件调整

```text
internal/config/config.go       # 使用 Configor 原生 Load
internal/config/config_test.go  # 删除旧解析器契约，增加 Configor 能力测试
README.md                       # 说明格式、叠加、回退和环境覆盖
go.mod                          # Configor v1.2.2
go.sum                          # 依赖校验和
```

`cmd/reagent`、Engine、Provider、Schema、Registry 和 Tools 行为不变。

## 验收标准

- 项目配置文件完全通过 Configor v1.2.2 加载；
- JSON、YAML、TOML 均通过真实文件测试；
- Configor 原生 Environment、example 和 Shell 环境覆盖行为有测试；
- 项目原有业务规范化和校验继续生效；
- `go vet ./...`、`go test -race -count=1 ./...` 通过；
- `gofmt -l cmd internal` 无输出。
