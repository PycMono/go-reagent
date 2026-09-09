# Agent整体开发记录

设计：`../specs/2026-09-04-agent-self-training-design.md`

工作分支：`feat/multi-agent-platform`；代码目录：`/Users/allen/projects/work/github/go-reagent`。
执行基线：`5943a8c`。2026-09-09 用户明确授权“按照这个思路开发吧”，恢复实施。
以 multi-agent-platform-design.md 与技术主规格为准，分阶段实现和验证。

## 已确认产品契约

- 登录身份由可信宿主接入；未接入时仅允许显式配置的固定组织匿名普通用户，不能获得管理权限。
- 客户先浏览本组织Agent目录，选择后进入聊天，第一条消息才创建会话。
- 历史会话绑定原Agent；切换Agent不带走消息、附件和临时文件。
- 每个 Agent有独立不可变版本；公共 AGENTS/Profiles 只作为初始化模板。
- 管理员训练、校验、人工发布及快捷换模型均保留版本、CAS 和恢复证据。

## 阶段状态

| 阶段 | 状态 | 证据/剩余工作 |
| --- | --- | --- |
| 方案完善 | 待用户审核 | 新增面向产品流程的整体方案；技术设计和 B/C/D 计划均是待审材料 |
| Phase 1A | 已暂停 | 提前开始的底层改动保留在隔离分支；存在未提交的 SDK/配置改动，不视为阶段完成 |
| Phase 1B | 计划已编写 | 身份、Agent目录、版本持久化、模板、会话 Runtime、聊天接入 |
| Phase 1C | 计划已编写 | 训练会话、持久输入、checkpoint、禁网作者、发布与恢复 |
| Phase 1D | 计划已编写 | 管理工作台、快捷换模型、迁移/备份演练和全链路验收 |

## 验证与限制

- Phase 1A 共享策略/文件子根/只读检查的定向及竞态测试通过。
- 原生 macOS Seatbelt 与 Linux Bubblewrap 的文件攻击测试通过：读写边界、删除/改名、符号链接、hardlink 和直接 argv。
- Linux 测试在 Docker 内运行，通过复制源码而非挂载宿主目录；为允许嵌套命名空间，专用测试容器使用 privileged。生产 Runner 仍实施自身的隔离规则。
- 基线全仓测试存在既有 `TestRegisteredConversationGraphStartsDisabledWithoutMySQL` 失败：旧 fixture 引入 HTTP 控制器却未提供 Chat Service。该问题与本次权限改动无关，后续装配验收必须明确处理，不把失败隐藏为通过。
- 真实宿主认证协议、生产历史租户映射及生产迁移/部署未执行；这些外部接入不影响独立功能开发和隔离测试。
- 后续阶段尚未实现，当前不能宣称Agent平台整体完成或已可部署。

## 本次恢复

- 已同步经审核的 13 个 Go / 4 个 JS / 4 个 SQL 草案；不会直接把审阅包复制成生产模块。
- Phase 1A baseline：pi 全部包通过；config/server/CLI 因先前未完成配置测试编译失败，正在补齐。
- 登录采用可信宿主 Authenticator 接口和显式匿名普通用户模式；无认证适配器时拒绝管理，不虚构客户登录。

- 按用户纠正，全部 61 个已变更文件与任务报告已迁回服务仓库，逐文件比对通过；旧临时工作区停止写入。迁移前服务目录内容已备份，未丢弃已有文档。
