# go-reagent 完整重命名设计

## 目标

将遗留项目身份完整统一为 `go-reagent`，并采用与 Git 远端一致的 Go 模块路径
`github.com/PycMono/go-reagent`。重命名只改变项目身份、内部引用和
文档示例，不改变运行逻辑、配置结构、公共行为或第三方依赖版本。

## 变更范围

- 将 `go.mod` 的 module path 改为 `github.com/PycMono/go-reagent`。
- 将全部项目内部 Go import 改为 `github.com/PycMono/go-reagent/internal/...`。
- 将遗留命令入口目录迁移至 `cmd/reagent`，同步代码、测试、提示词和运行示例。
- 将系统提示词、README、示例文本、包注释中的旧项目身份改为 `go-reagent`。
- 将已有设计文档和实施计划中的旧项目名、旧入口路径与项目专用缓存路径同步改名。
- 保留 `OpenClaw` 等确实指向外部项目或外部理念的名称。
- 保持所有第三方模块名称与版本不变；“全部依赖改名”仅指项目自身模块路径及其内部引用。

## 实施方式

先迁移命令目录，再执行受控的全仓文本替换。替换范围只覆盖明确属于遗留项目身份、
入口、缓存目录和安全路径示例的内容；之后逐项检查 `OpenClaw` 内容，确认它们确实是
应保留的外部引用。

## 兼容性与风险

Go 模块路径和命令包路径会发生有意的破坏性变更：外部使用者需要把 import 更新为
`github.com/PycMono/go-reagent/...`，启动命令更新为 `go run ./cmd/reagent`。配置字段、
环境变量、工具协议和 Agent 行为均保持不变。

## 验收标准

- `go list ./...` 输出的所有本地包均位于 `github.com/PycMono/go-reagent/...`。
- 全仓项目身份统一为 `go-reagent`，命令入口统一为 `cmd/reagent`。
- 剩余 `OpenClaw` 字样均经人工确认属于外部名称；项目身份不再使用旧名。
- `go test ./...` 通过。
- `go vet ./...` 通过。
- `git diff --check` 通过。
