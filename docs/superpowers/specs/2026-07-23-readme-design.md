# README 设计说明

## 目标

为 `go-reagent` 创建一份与当前代码状态一致的中文 README，并补齐用户指定的最小 Agent 核心包，让第一次接触仓库的开发者能够理解项目定位、架构边界、目录职责、运行方法和后续建设顺序。

## 目标目录

```text
go-reagent/
├── cmd/reagent/main.go
├── internal/
│   ├── engine/loop.go
│   ├── provider/interface.go
│   ├── schema/message.go
│   └── tools/registry.go
├── go.mod
└── README.md
```

`internal/context`、`internal/feishu` 和 `internal/memory` 不属于本阶段目录；它们当前为空，可以移除。

## 内容结构

README 按以下顺序组织：

1. 项目名称、定位和当前状态。
2. 核心架构及一次 Agent 调用的主要数据流。
3. 当前项目目录树及各包的职责。
4. 环境要求、运行和测试命令。
5. 按依赖关系排序的开发路线图。

## 内容约束

- 使用中文编写，命令、包名和接口名保留英文。
- 明确项目仍处于最小核心搭建阶段。
- `schema` 提供消息和工具调用公共类型。
- `provider` 提供模型调用接口。
- `tools` 提供工具及工具注册表接口。
- `engine` 提供可测试的 Main Loop：请求模型、执行工具调用，并把工具结果回传模型，直到得到最终回复。
- 不把尚未实现的模型、工具或渠道适配描述成已经完成的能力。
- 不虚构配置项、环境变量、模型接口或部署方式。
- 使用仓库当前模块名 `go-reagent`，不猜测未知的 GitHub 账户或远程模块地址。
- 运行示例仅使用当前可执行的 `go run ./cmd/reagent` 和 `go test ./...`。

## 验收标准

- 仓库根目录存在 `README.md`。
- README 覆盖项目介绍、架构、目录布局、快速开始和路线图。
- README 中的路径与当前仓库结构一致。
- 文档中的 Go 命令可以在仓库根目录执行。
- 四个新增 Go 文件均可编译，并通过自动化测试验证 Main Loop 的直接回复和工具调用流程。
