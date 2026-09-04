# Agent 自训练、评测与微调设计

## 状态

本文件是待审核的架构设计，不是实现计划。设计基于：

- go-reagent `master` 分支 `9eb5e37`；
- Workify `dev` 分支 `e838db53`；
- 已确认采用方案 A：管理员与目标 Agent 一对一聊天，由同一个逻辑 Agent 修改自己的候选资产；
- 训练执行与生产执行按会话隔离，候选资产通过评测和管理员审批后才能发布；
- 目标能力同时覆盖 Workspace 资产训练和模型 Fine-tuning，但分阶段实现。

本文中的“Workify 路径”均相对于参考仓库
`/Users/allen/projects/work/dustess/gitlab/workify`。

## 1. 结论

go-reagent 应采用 Workify 已验证的自训练身份模型：

1. 训练者就是目标 Agent，不创建另一条 `TrainerAgent` 业务记录；
2. 管理员训练会话为该 Agent 创建独立 Runner、独立候选 Workspace 和临时训练权限；
3. Agent 在候选 Workspace 内修改 `AGENTS.md`、Skills、脚本和资料；
4. 运行时拦截越权写入，发布时再次检查真实文件树和 Git diff；
5. 训练 Agent 只负责产出候选内容，不负责最终判分和生产发布；
6. 独立 Evaluation Runner 使用生产权限测试候选版本；
7. 管理员审核 diff、评测报告和成本后发布；
8. 发布单元是完整的 `AgentRelease`，同时固定 Bundle、模型版本、工具策略和评测报告；
9. Fine-tuning 由应用层异步流水线完成，`pi` 只运行最终得到的模型 ID。

角色边界固定为：

```text
目标 Agent       = Author：修改 Candidate
Evaluation Runner = Judge：执行不可修改的测试集
Release Service   = Gatekeeper：验证、发布、回滚
管理员             = Approver：批准候选、样本和发布
FineTune Backend   = Trainer：调用外部平台训练模型权重
```

不得把“Agent 自训练”实现成 Agent 直接修改正在服务的共享目录，也不得让 Agent
同时修改候选、修改评测基准、给自己打分并切换生产版本。

## 2. 术语

| 术语 | 定义 |
| --- | --- |
| Agent | 稳定业务身份，拥有名称、说明、状态和当前发布版本 |
| Bundle | Agent 自有的行为资产集合：`AGENTS.md`、Skills、脚本、资料和清单 |
| Bundle Version | Bundle 的不可变 Git commit/tag 和内容摘要 |
| Model Revision | 基础模型或 Fine-tuning 产出的不可变模型引用 |
| Agent Release | 可运行、可回滚的完整行为快照，固定 Bundle、模型、工具策略和验证证据 |
| Training Session | 管理员与目标 Agent 的一对一训练会话 |
| Candidate Workspace | 从基础 Release 检出的训练候选目录，只属于一个 Training Session |
| Training Example | 管理员审核通过的输入、期望输出、负例或工具期望 |
| Dataset Version | 由已审核 Training Examples 组成的不可变数据集 |
| Evaluation Suite | 平台保存、训练 Agent 不可修改的测试用例集合 |
| Evaluation Run | 一个 Candidate Release 在一个 Evaluation Suite 上的执行结果 |
| FineTune Job | 外部模型平台上的异步微调任务 |

## 3. 当前 go-reagent 的约束

### 3.1 单一 Runner 与固定 Workspace

`cmd/server/app.go` 当前在服务启动时调用一次 `pi.New`，全站共享一个
`*pi.Agent`。`pi.Options` 在构造时固定 `WorkDir`、Provider、工具、MCP、沙箱和
中间件。该模型适合当前单 Workspace Web Chat，但不能直接表达多个动态 Agent、
多个 Release 和多个候选训练目录。

### 3.2 `pi.Agent` 的可复用边界

`pi.Agent.Run` 本身同步、无状态、并发安全；History、Context 和消息持久化由上层
业务管理。它适合作为“一个运行规格”的执行实例：

```text
一个 pi.Agent
= 一个 WorkDir
+ 一个模型配置
+ 一套 Tool/MCP 注册表
+ 一套沙箱与中间件策略
```

因此第一阶段不把 WorkDir 改成每次 Run 动态参数，而是在应用层按 Release 或
Training Session 创建并管理多个 `pi.Agent`。

### 3.3 Profile 不是动态 Agent

当前 `Agent Profile` 是随代码发布的只读 Catalog：会话只保存 `profile_code`，
运行时把 Profile AGENTS 和 Skill Catalog 作为 ContextBlock 注入同一个 Runner。
它没有独立 Workspace、模型、工具权限、版本或发布生命周期。

引入训练系统后：

- 现有 Profile 改名为 `AgentTemplate`，只负责创建 Agent 的初始默认值；
- 生产会话绑定 `agent_id` 和具体 `agent_release_id`；
- `profile_code` 只用于迁移现有会话和标记创建来源，不再是运行时身份。

### 3.4 当前执行沙箱允许 Workspace 写入

浏览器服务当前打开 `AllowWrite`、`AllowExec` 和内置 Subagent。`exec` 的 Seatbelt/
Bubblewrap 策略允许写整个 Workspace。即使移除 `write/edit/apply_patch` 工具，shell
仍可以修改 Bundle。

生产 Agent 要运行 Skill 脚本但不能修改自己的发布资产，因此 `pi` 必须把“允许执行
命令”和“允许修改 Bundle”拆开，不能继续用 `AllowExec` 隐含整个 Workspace 可写。

### 3.5 当前没有管理员身份边界

`infrastructure/middleware/visitor.go` 当前只给浏览器创建匿名 `TypeUser` Session，随后
只向 `bizctx` 写入 UserID。仓库没有管理员登录、Role Context 或 Tenant Resolver。依赖的
`go-gin-sdk/session` 虽然定义了 `TypeAdmin`，但“类型存在”不等于服务已经可靠认证了
管理员。

所以 Training API 上线前必须先有可信的 `Principal{TenantID, UserID, Role}`。本设计只
定义消费管理员身份的端口，不在训练子系统里发明账号密码系统：

- 普通 Chat 可以继续使用匿名 Visitor Principal；
- Training/Release/Evaluation/Fine-tuning 路由必须经过 `RequireAdmin`；
- Principal 必须由受信任的登录系统或 Admin Session 适配器创建，不能由请求 Header
  直接声称角色；
- 没有配置 Admin Auth Resolver 时，服务不注册管理页面和写路由并在启动日志明确提示；
- 第一阶段可以使用单租户 `default`，但 TenantID 仍进入 Principal、数据库查询和文件
  路径，避免后续多租户改造破坏安全边界。

## 4. Workify 参考实现的真实模型

### 4.1 同一个逻辑 Agent，按 SessionMember 创建运行实例

Workify 的 `AgentDefinition` 保存稳定 `agentId`、Bundle 路径、最新版本、实时配置和
Capabilities。每个会话中的 Agent 是一条 `SessionMember`：

- `SessionMember.agentId` 指向同一个逻辑 Agent；
- `SessionMember.agentVersion` 固定该成员当前采用的 Bundle 版本；
- `SessionMember.configSnapshot` 固定本会话成员的有效模型、能力和资源；
- `SessionMember.cwdPath` 是本会话成员独有的运行目录；
- Runtime Manager 按 `member.id` 创建运行进程，不按 `agentId` 全局复用一个进程。

证据位置：

- `packages/store-spec/src/types.ts`：`AgentDefinition`、`AgentVersion`、
  `SessionMember`、`SessionMemberResourceOverrides`；
- `packages/server/src/session-member-provisioning.ts`：
  `prepareAgentSessionMember`；
- `packages/server/src/agent-runtime/manager.ts`：
  `ensureMemberRuntime`、`spawnMemberRuntime`。

所以 Workify 的自训练不是“一个共享生产进程原地改自己”，而是：同一个 Agent 身份
在管理员训练会话里运行一个独立成员实例，该实例修改自己的会话 cwd。

### 4.2 训练能力是会话有效能力

Workify 把 Bundle 作者权限拆成：

- `agent.train_self`：修改并发布自己的 Bundle；
- `agent.build_agents`：创建、修改和发布其他 Agent。

二者互不隐含。官方推荐把 `agent.train_self` 排除在 Agent 默认能力之外，由管理员
只在训练 SessionMember 上通过 `resourceOverrides.capabilities` 临时开启。普通用户
只能缩小本会话能力，管理员可以在租户能力目录上限内扩大能力。

这带来三个重要性质：

1. 普通生产会话看不到训练 Skill 和训练工具；
2. 训练能力不会因为 Agent 默认配置而散落到所有会话；
3. SessionMember reload 后仍保留明确的会话覆盖。

证据位置：

- `packages/builtin-resources/skills/workify-guide/1.0.0/topics/concepts.md`；
- `packages/server/src/member-resource-override-edit.ts`；
- `packages/server/src/member-resource-overrides.ts`；
- `packages/server/src/slash-commands/training-skill.ts`。

### 4.3 训练入口是普通会话加 `/skill:train`

Workify 已删除旧的全局 training mode。管理员可以在普通会话中发送明确的长期行为
修改要求，或显式调用 `/skill:train <instruction>`。持有 `agent.train_self` 时，平台
强制挂载内置 `train` Skill；该 Skill 的 `requiredRole: admin` 会在 Slash Command
分发边界校验。

`train` Skill 负责教 Agent：

- 什么属于永久行为修改；
- 哪类需求写入哪个 Bundle 位置；
- 如何处理 Skills、脚本、Extensions、Hooks 和配置；
- 如何验证实际工具轨迹；
- 默认不自行发布，等待管理员点击 Complete training；
- 只有管理员明确要求自动发布时才允许 self-publish。

证据位置：

- `packages/builtin-resources/skills/train/1.0.0/SKILL.md`；
- `packages/server/src/slash-commands/catalog.ts`；
- `packages/server/src/slash-commands/dispatch.ts`。

### 4.4 平台提示词提供资产分流规则

Workify 不把全部训练协议长期塞进 System Prompt。System Prompt 只保留行为治理和
Bundle 作者分类，详细训练流程按需从 `train` Skill 加载。

资产分流规则是：

| 需求形态 | 写入位置 |
| --- | --- |
| 简单身份、语气、约定、单条规则 | `AGENTS.md` |
| 多段但不是完整流程的专题规则 | `docs/<topic>.md`，由 `AGENTS.md` 简短引用 |
| 多步骤、按条件触发、需要脚本的流程 | `.pi/skills/<id>/` |
| 多任务复用的确定性操作 | `.pi/extensions/<id>/` 注册 Tool |
| 每个 Turn 前必须执行的确定性行为 | `.pi/extensions/<id>/` 生命周期 Hook |

其原则是优先选择更轻的载体：规则优先于 Skill，Skill 优先于 Extension；不要把单行
规则包装成 Skill，也不要把简单 shell 命令包装成新工具。

证据位置：`packages/agent/src/runtime-adapters/system-append.ts` 中
`BUNDLE_AUTHORING_RULES`。

go-reagent 第一期只采用前三类：`AGENTS.md`、专题文档和带脚本的 Skill。当前 SDK
没有 Workify 的 Bundle Pi Extension/Hook 加载器，不在第一阶段复制该子系统。

### 4.5 每个 SessionMember 有独立候选 cwd

Workify 从 Agent 的已发布 Git tag 物化 SessionMember cwd：

```text
Agent Bundle Git repository
  └── immutable version tag
        ├── SessionMember A cwd
        ├── SessionMember B cwd
        └── Admin training SessionMember cwd
```

训练 Agent 修改的是训练 SessionMember cwd。发布前，生产 Bundle 和其他会话的 cwd
不变。训练 cwd 同时包含运行时生成目录，但 `.gitignore` 排除 Workspace 挂载、附件、
`.workify/` 和其他非 Bundle 状态，避免发布时误带入会话数据。

证据位置：

- `packages/server/src/session-member-provisioning.ts`：cwd 路径和
  `materializeBundleWorktree`；
- `packages/server/src/routes/agent-training.ts`：训练 cwd 提交与发布；
- `packages/server/src/bundle-fs.ts`：Bundle Gitignore 和仓库操作。

### 4.6 `service` Profile 不能直接训练 Bundle

Workify 的 `service` Runtime Profile 强制采用最严格的文件 confinement：无论用户
是否为管理员，都只能写 `scratch/` 和 `/tmp`。`agent.train_self` 不能绕过该规则。

因此训练一个默认 `service` Agent 时，训练 SessionMember 需要临时使用 `general`
Profile，并开启 `agent.train_self`；发布后的生产会话仍按 Agent 的 `service` 默认配置
运行。这个差异不能只靠一个 `train_self=true` 表达。

证据位置：

- `packages/agent/src/runtime-adapters/gate-a.ts`：`checkConfinedWrite`；
- `docs/security.md` 的 File-access confinement；
- `packages/builtin-resources/skills/workify-guide/1.0.0/topics/best-practices.md`。

go-reagent 不复制 `general/service` 命名，但必须区分 `chat`、`training`、`evaluation`
三种 Runtime Purpose，并由服务端决定各自的权限策略。

### 4.7 Gate A：运行时工具层拦截

Workify 在 Pi SDK `beforeToolCall` 上包装 `read/write/edit`，检查：

1. 路径是否逃逸 cwd；
2. 是否触碰平台维护的 System/MCP 文件；
3. 修改自身行为文件时，当前 acting user 是否为管理员；
4. 当前 SessionMember 是否持有 `agent.train_self`；
5. 修改其他 Agent Build 时是否持有 `agent.build_agents`；
6. Memory、Workspace 和 fileAccess 策略是否允许；
7. confined 模式的读写路径是否落在允许范围。

acting user 身份由服务端从当前 Turn 的真实触发者派生，再随 `run_turn` 发送给 Runtime；
Agent 不能通过消息正文自称管理员。

Gate A 的局限是它不解析 bash 中的 `cat >`、`sed -i` 等写入。因此它改善模型的即时
反馈，但不是最终安全边界。

证据位置：

- `packages/agent/src/runtime-adapters/gate-a.ts`；
- `packages/agent/src/runtime-adapters/pi.ts`：`attachGateAHook`；
- `docs/security.md` 的 Git Boundaries。

### 4.8 Gate B：发布边界检查真实 diff

Workify 在所有发布路径的汇聚点检查即将发布的真实 Git diff。无论 Agent 使用
`write`、`edit` 还是 bash 修改文件，最终产物都必须通过 Gate B。

Gate B 永久拒绝 Workify 自有文件。普通用户只能发布受限路径；管理员和持有对应
Bundle 作者能力的 Agent 可发布行为资产。之后还会进行：

- Bundle Skill/Prompt 扫描；
- Slash Command 冲突检查；
- Runtime Adapter 与资源兼容性检查；
- Bundle TypeScript Extensions 类型检查；
- 空变更拒绝；
- SemVer 和 Git tag 唯一性检查。

证据位置：

- `packages/server/src/bundle-commit.ts`；
- `packages/server/src/agent-version-snapshot.ts`；
- `packages/server/src/bundle-typecheck.ts`；
- `packages/server/src/publish-from-worktree.ts`。

### 4.9 发布事务

Workify 的 Complete training 路径：

1. 校验目标是有 `agentId`、版本和 cwd 的 Agent SessionMember；
2. 提交训练 cwd 的脏文件；
3. 获取以目标 Agent 为粒度的分布式 publish lock；
4. 在锁内读取最高版本并由服务端计算下一 SemVer；
5. 确认版本行和 Git tag 均不存在；
6. 比较最新发布版本与 Candidate ref；
7. 执行 Gate B；
8. squash merge Candidate 到 Bundle 主线；
9. 拒绝无内容变更；
10. 创建 Git commit/tag；
11. 执行资源清单和 Extension 校验；
12. 分布式模式先上传 Bundle，再创建可被运行时引用的 AgentVersion 记录；
13. 更新 Agent latestVersion；
14. 记录审计和事件；
15. 重新物化发布者自己的 cwd；
16. 标记其他 SessionMember 在下一 Turn 前 reload。

Git、对象存储和数据库不是一个 ACID 事务。Workify 通过顺序和补偿降低不一致：只在
Bundle 可用后创建版本行；版本行创建前失败时删除 tag、重置 Git，并追加补偿审计。

证据位置：

- `packages/server/src/routes/agent-training.ts`；
- `packages/server/src/publish-from-worktree.ts`；
- `packages/server/src/agent-version-next.ts`；
- `packages/server/src/coordination/`。

### 4.10 发布传播与上下文更新

Workify 不在发布请求中同步重建所有会话，而是给目标 Agent 的所有 SessionMember
写入无版本号的 reload pending 标记。成员下一次正常 Turn 前重新解析 Agent 当前最新
状态并物化 cwd。连续多次发布会自然合并成一次 reload-to-latest。

Reload 保留聊天记录和会话级覆盖。Bundle、Skills 或 Extensions 变化时，系统向 Agent
写入包含具体变化的 reload notice；可选 `rebuildContextOnReload` 会丢弃 Provider 原生
推理/工具上下文，再从可见聊天记录重建，以减少旧行为惯性。

证据位置：

- `packages/server/src/session-member-reload.ts`；
- `packages/server/src/session-member-provisioning.ts`；
- `docs/references/runtime-sessions.md`。

### 4.11 Workify 的验证方式

Workify `train` Skill 要求训练 Agent 创建测试会话，发送 human-proxy 消息并检查
`toolTrace`：

- `totalCalls: 0` 表示 Agent 根本没有执行预期工具；
- `status: error` 表示工具确实执行但失败；
- `status: ok` 仍需检查输出，不能把进程退出码 0 当作业务成功。

这种方式适合交互验证，但 Workify 当前没有第一等的 Evaluation Suite、不可修改的
评测门槛、Candidate 与基线对比、Dataset Version 或 FineTune Job。

### 4.12 Workify 不应直接照搬的部分

| Workify 当前设计 | go-reagent 的取舍 |
| --- | --- |
| AgentVersion 只固定 Bundle | `AgentRelease` 同时固定 Bundle、模型和工具策略 |
| Agent config/capabilities 是 live state | 所有影响行为的配置进入 Release 快照 |
| 公共资源采用最高已发布版本 | Release 固定精确资源版本，保证可复现 |
| 训练验证主要由 Skill 引导人工执行 | 增加不可由训练 Agent 修改的 Evaluation Suite |
| Agent 可在明确要求时 self-publish | 第一阶段不向 Agent 暴露 publish，只允许提交 Candidate |
| Gate A 不拦 bash 写入 | 保留 Gate B，并让生产 exec 在 OS 沙箱层只写 scratch/tmp |
| `service` Profile 通过会话切到 `general` 才能训练 | 使用明确的 Runtime Purpose 和服务端权限策略 |
| Bundle Extensions/Hook 是核心资产 | 第一期只支持 AGENTS、文档、Skills 和脚本 |

### 4.13 Workify 源码调用链与文件分工

Workify 没有一个名为“自主训练引擎”的单体模块；能力由会话物化、Runtime、Skill、两层
Gate、Git 发布和 reload 多个模块共同完成：

```text
创建/加载 Agent SessionMember
  → prepareAgentSessionMember
  → materializeBundleWorktree
  → AgentRuntimeManager.ensureMemberRuntime
  → 独立 worker + cwd

管理员发送 /skill:train
  → slash command dispatch
  → force mount train Skill
  → Pi runtime turn
  → attachGateAHook/checkGateA
  → write/edit/bash 修改 member cwd

管理员 Complete training
  → completeAgentTraining
  → commitBundleIfDirty
  → withPublishLock(agentId)
  → publishFromWorktree
  → checkBundleCommitDiff (Gate B)
  → buildAgentVersionResourceSnapshot
  → commit/tag/AgentVersion/latestVersion
  → mark reload pending
```

| Workify 文件 | 真实职责 | go-reagent 对应落位 |
| --- | --- | --- |
| `packages/store-spec/src/types.ts` | AgentDefinition/Version/SessionMember 数据契约 | `domain/entity/agent*` |
| `packages/server/src/session-member-provisioning.ts` | 从版本物化每成员 cwd 和资源快照 | `agentruntime/resolver.go` + `agentbundle/worktree.go` |
| `packages/server/src/agent-runtime/manager.ts` | 按 member 创建、复用、销毁 worker | `application/service/agentruntime/` |
| `packages/server/src/slash-commands/training-skill.ts` | 根据 capability 强制提供 train Skill | `application/tool/agenttraining/register.go` |
| `packages/builtin-resources/skills/train/1.0.0/SKILL.md` | 教 Agent 如何选择和修改 Bundle 资产 | 平台维护的 training policy/Skill |
| `packages/agent/src/runtime-adapters/system-append.ts` | 注入 Bundle authoring 分类和治理提示 | Training Runtime ContextBlock |
| `packages/agent/src/runtime-adapters/gate-a.ts` | 工具调用前检查身份、能力和路径 | Training Tool middleware + WorkspacePolicy |
| `packages/agent/src/runtime-adapters/pi.ts` | 把 Gate A hook 接入 Pi 生命周期 | RuntimeSpec 的 ExtraHandlers |
| `packages/server/src/routes/agent-training.ts` | Complete training 用例编排 | `agenttraining` + `agentrelease` Service |
| `packages/server/src/bundle-commit.ts` | 发布前检查真实 Git diff | CandidateValidator/Gate B |
| `packages/server/src/publish-from-worktree.ts` | lock、SemVer、commit/tag、补偿 | `agentrelease/publish.go` + `agentbundle/publish.go` |
| `packages/server/src/agent-version-snapshot.ts` | 固定发布时资源快照 | AgentRelease/RuntimeConfigSnapshot |
| `packages/server/src/session-member-reload.ts` | 发布后标记并懒 reload | Conversation pending_release_id |

最关键的源码事实是：`completeAgentTraining` 不是让正在运行的 worker 覆盖生产目录；它把
该 member cwd 作为候选交给服务端发布流程。go-reagent 方案 A 保留这条主线，同时把
Workify 分散在 live Agent config 中的行为配置进一步收敛到不可变 AgentRelease。

## 5. 目标和非目标

### 5.1 目标

- 管理员选择一个既有 Agent，创建一对一训练会话；
- 训练会话中的 AI 使用该 Agent 的身份和当前 Release，而不是通用 Trainer 身份；
- Agent 能通过聊天创建或修改 `AGENTS.md`、Skills、脚本和资料；
- 每轮修改有 Git diff、checkpoint、RunID 和操作者审计；
- Candidate 与生产 Release 完全隔离；
- 发布前执行结构校验、安全校验和自动评测；
- 自动修正循环有明确次数、Token 和成本上限；
- 管理员能查看 diff、评测结果、成本、失败原因并决定发布或放弃；
- 生产 Release 不可变并支持完整回滚；
- 审核通过的纠正可进入 Fine-tuning 数据集；
- Fine-tuning 模型必须通过相同 Evaluation Suite 才能组成新 Release；
- 多 Agent、多 Release 运行不改变 `pi.Runner` 的无状态消息契约。

### 5.2 非目标

- 第一阶段不支持 Agent 构建其他 Agent；
- 第一阶段不支持 Agent 自主切换生产版本；
- 第一阶段不支持多人共同编辑同一个 Candidate；
- 第一阶段不支持 Bundle 内自定义 Go Tool、Pi Extension 或生命周期 Hook；
- 第一阶段不允许 Skill 脚本运行时安装依赖；
- 不把训练数据库、Git、评测或 Fine-tuning API 放进 `pi`；
- 不把普通用户对话自动当作训练样本；
- 不允许 Agent 修改 Evaluation Suite 或发布阈值；
- 不承诺不同模型 Provider 之间迁移原生推理上下文。

## 6. 总体架构

```text
Admin Web
  │
  ├── Training API ───────────────┐
  │                               │
  │                         Training Service
  │                               │
  │                    ┌──────────┴──────────┐
  │                    │                     │
  │             Candidate Git           Training Runner
  │                    │                     │
  │                    └──── checkpoint ─────┘
  │                               │
  │                         Bundle Validator
  │                               │
  │                         Evaluation Service
  │                               │
  │                   isolated Evaluation Runner
  │                               │
  │                         Candidate Release
  │                               │
  └── Admin approval ─────── Release Service
                                  │
                         Active AgentRelease
                                  │
                           Runtime Manager
                                  │
                              pi.Runner

Approved Training Examples
  └── Dataset Version
        └── FineTune Backend
              └── Model Revision
                    └── Evaluation Service
                          └── Candidate Release
```

## 7. 领域模型

### 7.1 Agent

```go
type Agent struct {
    ID                      string
    TenantID                string
    Name                    string
    Description             string
    Status                  AgentStatus
    TemplateCode            string
    ActiveReleaseID         string
    ActiveTrainingSessionID string
    Version                 uint64
    CreatedBy               string
    CreatedAt               time.Time
    UpdatedAt               time.Time
}
```

约束：

- `ID` 是稳定身份，发布和回滚不改变；
- `ActiveReleaseID` 是生产流量唯一权威指针；
- `ActiveTrainingSessionID` 非空时拒绝第二个活跃训练会话；
- 删除 Agent 前必须先停用生产会话和训练会话；第一阶段只支持 archive。

### 7.2 BundleVersion

```go
type BundleVersion struct {
    ID                      string
    TenantID                string
    AgentID                 string
    Version                 string
    GitCommit               string
    GitTag                  string
    ContentDigest           string
    Manifest                BundleManifest
    SourceTrainingSessionID string
    CreatedBy               string
    CreatedAt               time.Time
}
```

`BundleVersion` 创建后不可更新或删除。Git tag 使用
`agent/<agentID>/bundle/v<semver>`；版本号在 Agent publish lock 内由服务端计算，模型
和浏览器不能自行猜测。

### 7.3 ModelRevision

```go
type ModelRevision struct {
    ID               string
    TenantID         string
    ProviderID       string
    ModelID          string
    Kind             ModelRevisionKind // base | fine_tuned
    BaseRevisionID   *string
    DatasetVersionID *string
    FineTuneJobID    *string
    Status           ModelRevisionStatus
    CreatedAt        time.Time
}
```

基础模型也必须注册成 ModelRevision，避免 Release 对配置文件中的浮动模型名产生隐式
依赖。

### 7.4 AgentRelease

```go
type AgentRelease struct {
    ID                 string
    TenantID           string
    AgentID            string
    Version            string
    BundleVersionID    string
    ModelRevisionID    string
    RuntimeConfig      RuntimeConfigSnapshot
    ToolPolicy         ToolPolicySnapshot
    ToolPolicyDigest   string
    ValidationRunID    string
    EvaluationRunID    *string
    ValidationMode     ReleaseValidationMode // manual_only | evaluated
    PublishedBy        string
    PublishedAt        time.Time
}
```

Release 必须固定：

- BundleVersion；
- ModelRevision；
- 精确 Skill/MCP/Tool 引用；
- 运行限制、Thinking、Compaction、Loop Detection 等行为配置；
- 权限策略摘要；
- 与发布 Candidate digest 完全一致的成功 CandidateValidationRun；
- Phase 2 后通过发布门槛的 EvaluationRun。Phase 1 的人工例外使用
  `ValidationMode=manual_only` 且 `EvaluationRunID=nil`，不能伪装成已评测发布。

`AgentRelease` 的上述内容创建后不可修改。是否承载流量只由
`Agent.ActiveReleaseID` 表达；历史 Release 不写入 `retired` 状态，因此可以直接回滚
激活，也不会因为状态字段变化破坏快照不可变性。评测前使用的是临时 `ReleaseSpec`，
只有管理员发布时才创建正式 `AgentRelease`。

API Key、Token 和密码不进入快照；快照只保存服务端 SecretRef。

### 7.5 TrainingSession

```go
type TrainingSession struct {
    ID                   string
    TenantID             string
    AgentID              string
    AdminUserID          string
    ConversationID       string
    BaseReleaseID        string
    CandidateWorkspaceID string
    CandidateBranch      string
    CandidateHead        string
    Status               TrainingStatus
    AutoRepairAttempts   int
    LastValidationRunID  *string
    LastEvaluationRunID  *string
    ActiveRunID          *string
    RunLeaseExpiresAt    *time.Time
    ExpiresAt            time.Time
    Version              uint64
    CreatedAt            time.Time
    UpdatedAt            time.Time
}
```

`CandidateWorkspaceID` 是存储适配器的逻辑定位符，不在业务表持久化某台机器的绝对
路径；第一阶段本地适配器把它解析到租户目录下的 `training/<id>/candidate`。

状态机：

```text
active
  ├── validating → active       校验失败，继续训练
  ├── validating → ready        Phase 1 校验通过，等待人工确认未自动评测
  ├── validating → evaluating   Phase 2 继续执行自动评测
  ├── evaluating → active       评测失败，管理员继续训练
  ├── evaluating → repairing    自动修正
  ├── evaluating → ready        达到发布门槛
  ├── ready → published
  ├── ready → active            继续修改后使旧评测失效
  ├── * → cancelled
  ├── * → stale                 基础 Release 已变化
  └── * → expired               Training Session 到达明确的过期时间
```

任何文件变更都会使已有 CandidateValidationRun 和 EvaluationRun 失效，并把 `ready`
恢复为 `active`。`active_training_session_id` 在 `active|validating|evaluating|repairing|ready`
期间始终被占用；`cancelled|stale|expired|published` 才释放。

### 7.6 CandidateValidationRun、EvaluationSuite 与 EvaluationRun

每次 Gate B dry-run 生成不可变的 `CandidateValidationRun`，固定可选 TrainingSessionID、
来源（training/migration/revalidation）、CandidateHead、Candidate content digest、
ToolPolicy digest、Validator 版本、状态、诊断、脚本 smoke 结果、耗时和创建时间。所有
Release 都必须引用同一 Candidate digest 的成功 ValidationRun；Phase 1 也不能只相信
页面上一次性的“校验通过”提示。

EvaluationCase 支持：

- 输入消息和可选历史；
- 固定 Context fixture；
- Tool fake 或受控真实 Tool；
- 必须调用/禁止调用的 Tool；
- 参数 JSON Schema 或字段断言；
- 精确文本、正则、结构化 Schema、关键事实断言；
- 禁止内容和安全规则；
- 可选独立 Judge Model 评分；
- hard/soft 严重级别。

EvaluationRun 固定 Candidate digest、ModelRevision、ToolPolicy digest、Suite version、
每个 Case 结果、总成本和耗时。Candidate digest 变化后旧报告不能用于发布。

### 7.7 TrainingExample、DatasetVersion 与 FineTuneJob

TrainingExample 只来自管理员明确审核：

```go
type TrainingExample struct {
    ID                    string
    TenantID              string
    AgentID               string
    Input                 []TrainingMessage
    ExpectedOutput        TrainingMessage
    RejectedOutput        *TrainingMessage
    ExpectedToolCalls     []ExpectedToolCall
    Tags                  []string
    SourceConversationID  string
    SourceRunID           string
    ApprovedBy            string
    ApprovedAt            time.Time
}
```

`TrainingMessage` 是 Fine-tuning 领域自己的规范消息结构，只包含 role、content blocks 和
规范 Tool call/result，不 import `pi/ai` 或任何 Provider SDK。应用层在运行评测时转换为
`pi.Message`，FineTune adapter 在提交外部任务时转换成 Provider 格式。

DatasetVersion 保存规范化后的样本 ID 集合、目标 Provider 格式、内容摘要、脱敏报告和
创建人。FineTuneJob 保存外部任务 ID、Provider、基础模型、超参数、状态、错误和 Usage。

## 8. 文件系统和 Git 布局

第一阶段使用本地文件系统和每 Agent 一个 Bundle Git repository：

```text
<data-dir>/tenants/<tenant-id>/agents/<agent-id>/
├── bundle.git/                     # 权威 bare repository
├── releases/<bundle-version-id>/   # 不可变运行物化目录
└── runtime-cache/                  # 可删除缓存

<data-dir>/tenants/<tenant-id>/training/<training-session-id>/
├── candidate/                      # 从 BaseRelease tag 检出的独立 worktree
├── scratch/                        # 不进入 Bundle
└── state/                          # 运行 checkpoint、临时报告，不进入 Bundle
```

Bundle 结构：

```text
AGENTS.md
skills/
  <skill-id>/
    SKILL.md
    scripts/
    references/
    assets/
documents/
assets/
agent.yaml
```

`agent.yaml` 只声明非秘密的 Bundle 元数据：格式版本、入口 AGENTS、Skill roots、脚本
运行要求和兼容的最小 Runtime 版本。模型、权限和密钥不写入该文件。

永久拒绝：

- 绝对路径和越界符号链接；
- socket、device、FIFO 等非普通文件；
- Git submodule；
- 嵌套 `.git`；
- `.runtime/`、`.system/`、Secret 文件和平台生成配置；
- 单文件超过 1 MiB；
- Bundle 总解压大小超过 32 MiB；
- 单个 `SKILL.md` 超过现有 256 KiB 限制；
- 路径数量超过 2,000。

大知识库不进入 Bundle，后续通过独立 Knowledge Base 引用。

## 9. Runtime Purpose 与权限模型

### 9.1 三种运行目的

| Purpose | Workspace | 写权限 | Tool 集 | 会话历史 |
| --- | --- | --- | --- | --- |
| `chat` | 不可变 Release | 仅 `scratch/`、`.tmp/` | Release 生产 ToolPolicy | 用户会话历史 |
| `training` | Candidate | Bundle 全树可写，平台路径除外 | 生产工具的安全替身 + 作者工具 | 管理员训练历史 |
| `evaluation` | Candidate 快照 | 只读；仅临时目录可写 | 生产 ToolPolicy 的只读实现或测试替身 | 每个 Case 独立 |

Runtime Purpose 由应用层选择，不由客户端或模型通过 `pi.RunRequest` 传入。

### 9.2 `train_self` 的业务语义

go-reagent 第一阶段不把 `train_self` 做成 Agent 的永久默认能力字段。它由
Training Service 根据以下事实派生：

- 请求已经通过管理员认证；
- TrainingSession 属于该管理员；
- TrainingSession 的目标 Agent 与 Candidate 相同；
- 会话状态为 `active|validating|evaluating|repairing|ready`；
- 当前 Run 持有服务端签发且未过期的独占 lease。

普通 Chat API 永远不能请求或获得 `train_self`。这比在通用会话上开放任意 Capability
覆盖更符合当前“管理员一对一训练”的单一产品目标。

### 9.3 工具矩阵

| Tool | Chat | Training | Evaluation |
| --- | ---: | ---: | ---: |
| `read` | 是 | 是 | 是 |
| `write/edit/apply_patch` | 否 | 是 | 否 |
| `exec/process` | 按 Release | 是 | 按 Release |
| 生产业务 Tool | 按 Release | 只读实现或安全替身 | 只读实现或安全替身 |
| `inspect_candidate` | 否 | 是 | 否 |
| `validate_candidate` | 否 | 是 | 否 |
| `run_candidate_evaluation` | 否 | 是 | 否 |
| `submit_candidate` | 否 | 是 | 否 |
| `publish_release` | 否 | 否 | 否 |
| 修改 Evaluation Suite | 否 | 否 | 否 |
| 提交 FineTune Job | 否 | 否 | 否 |

Agent 调用 `submit_candidate` 只触发服务端校验/评测并在通过后把状态变成 `ready`；
发布只能由管理员 HTTP API 进入 Release Service。

应用层 Tool Catalog 必须给内置 Tool 和 MCP Tool 声明 `read_only`、`reversible_write` 或
`external_side_effect`。Training/Evaluation 默认只能使用只读实现；写操作和邮件、支付、
工单提交等外部副作用必须使用 Fake、dry-run 或专用测试租户。不能为了验证候选行为而
修改真实生产数据。未声明 Effect 的 Tool 在 Training/Evaluation 中默认拒绝，而不是
按只读处理。

## 10. `pi` SDK 边界

### 10.1 保持不变

以下公共契约保持不变：

```go
type Runner interface {
    Run(context.Context, RunRequest, EventListener) (RunResult, error)
}
```

`pi` 不增加 AgentID、ReleaseID、TrainingSessionID、数据库、Git、评测和 Fine-tuning
概念。业务标识继续只存在于上层 Span、Repository 和 Runtime Manager。

### 10.2 新增 Workspace 写策略

`pi.Options` 增加通用文件策略，而不是增加 TrainingMode：

```go
type WorkspaceWriteMode string

const (
    WorkspaceWriteRestricted WorkspaceWriteMode = "restricted"
    WorkspaceWriteAll        WorkspaceWriteMode = "all"
)

type WorkspacePolicy struct {
    WriteMode        WorkspaceWriteMode
    WritablePrefixes []string
}

type Options struct {
    WorkDir          string
    WorkspacePolicy  WorkspacePolicy
    AllowWrite       bool
    AllowExec        bool
    // existing fields...
}
```

语义：

- `WorkspaceWriteRestricted`：文件修改工具仍由 `AllowWrite` 决定是否注册；一旦注册，
  文件工具与 exec 都只能写 `WritablePrefixes`。Chat/Evaluation 设置
  `AllowWrite=false`，exec 只写 `.tmp/`、`scratch/` 和平台明确提供的 Runtime 目录；
- `WorkspaceWriteAll`：文件修改工具按 `AllowWrite` 注册；exec 可写 WorkDir，但仍受路径
  边界和平台沙箱限制；
- `WritablePrefixes` 只允许规范化后的 WorkDir 相对目录；不能配置绝对路径、`..` 或
  符号链接逃逸；
- `AllowWrite` 或 `AllowExec` 为 true 时，`WriteMode` 不能为空，避免旧调用方无意获得
  全 Workspace 写权限；
- Host sandbox 无法执行限制写保证时，
  `AllowExec=true + WorkspaceWriteRestricted` 必须启动失败，不能静默降级为宿主可写。

Seatbelt 和 Bubblewrap 必须在 OS 层实现相同策略，不能只依赖 Tool middleware。

`pi.New` 的关键装配变化如下，策略只规范化一次并传给所有可产生子进程或写句柄的组件：

```go
func New(opts Options) (*Agent, error) {
    policy, err := NormalizeWorkspacePolicy(opts.WorkDir, opts.WorkspacePolicy)
    if err != nil {
        return nil, fmt.Errorf("pi: workspace policy: %w", err)
    }
    runner, err := sandbox.NewRunner(opts.WorkDir, policy.SandboxPolicy())
    if err != nil {
        return nil, fmt.Errorf("pi: select sandbox runner: %w", err)
    }
    workspace, err := tools.NewWorkspace(tools.Root(opts.WorkDir), policy.FilePolicy())
    if err != nil {
        return nil, err
    }
    extensions, err := mcp.New(opts.MCPServers, tools.Root(opts.WorkDir), runner, policy.MCPPolicy())
    // 后续 Registry/Provider/Loop/Agent 装配保持现有顺序。
}
```

这里不是要求暴露三个新的公共 Policy 类型；`SandboxPolicy/FilePolicy/MCPPolicy` 可以是
`pi` 内部适配结果。公共 SDK 只承诺 `WorkspacePolicy`，避免调用方分别配置后产生漂移。

### 10.3 Workspace 检查接口

`pi/harness` 暴露只读检查能力，供发布服务复用现有 AGENTS/Skill 解析契约：

```go
type WorkspaceReport struct {
    AgentInstructionsDigest string
    Skills                  []skills.Summary
    Diagnostics             []skills.Diagnostic
}

func InspectWorkspace(ctx context.Context, workDir string) (WorkspaceReport, error)
```

它不做 Git、Release、权限或业务审批，只验证 Runtime 实际能否加载该 Workspace。

### 10.4 应用层 Runtime Manager

新增应用端口：

```go
type RuntimeSpec struct {
    Key             string
    Purpose         RuntimePurpose
    WorkDir         string
    WorkspacePolicy pi.WorkspacePolicy
    Platform        providers.Options
    Tools           []ResolvedTool
    MCPServers      []ResolvedMCPServer
    ConfigDigest    string
}

type RuntimeLease interface {
    Runner() pi.Runner
    Release()
}

type RuntimeManager interface {
    Acquire(context.Context, RuntimeSpec) (RuntimeLease, error)
    Invalidate(context.Context, string) error
    Close(context.Context) error
}

type ChatRuntimeProvider interface {
    AcquireChat(context.Context, ChatIdentity) (RuntimeLease, error)
}
```

`ReleaseResolver` 实现 `ChatRuntimeProvider`：读取 Release、解析 Bundle/Model/ToolPolicy，
构造 RuntimeSpec 后再调用 RuntimeManager。Conversation 只依赖 ChatRuntimeProvider，
不能自行拼装 RuntimeSpec。

缓存 Key：

- Chat：`release:<releaseID>:chat:<configDigest>`；
- Evaluation：
  `evaluation:<snapshotID>:<modelRevisionID>:<toolPolicyDigest>:<configDigest>`；
- Training：`training:<trainingSessionID>:<configDigest>`。

Runtime Manager 负责 `pi.New`、`Agent.Start`、引用计数、空闲 LRU、`Agent.Stop` 和并发
单飞。第一阶段限制最大 32 个热 Runtime，空闲 15 分钟回收；活跃 Run 不回收。

`ResolvedTool`/`ResolvedMCPServer` 是应用层结构，包含精确 revision、Effect 和针对当前
Purpose 选择的生产/Fake/dry-run 实现。Runtime Manager 先校验 ToolPolicy 和 Effect，
再提取 `ai.Tool`/`mcp.ServerOptions` 传给 `pi.New`；`pi` 不感知 RuntimePurpose 或业务
副作用分类。

## 11. 训练会话完整流程

### 11.1 创建

1. 管理员调用 `POST /agents/{id}/training-sessions`；
2. 服务端读取 Agent 当前 ActiveRelease；
3. 用 Agent 行锁/乐观版本设置 `active_training_session_id`；
4. 创建 TrainingSession 和类型为 `training` 的 Conversation；
5. 从 ActiveRelease.BundleVersion tag 创建 Candidate worktree；
6. 创建 Candidate `scratch/`、`state/` 并写入 Gitignore，同时创建持久的
   `refs/training/<trainingID>/head`，防止删除 worktree 后 checkpoint 被 Git GC；
7. 组装 Training RuntimeSpec：相同 Agent 身份、相同基础模型、Candidate WorkDir、
   写权限和训练工具；
8. 向每轮 `pi.RunRequest.Context` 注入平台维护的 `agent-training-policy`；
9. 返回 TrainingSession、Agent 信息、基础 Release 和欢迎语。

`agent-training-policy` 明确：

- 这是管理员授权的永久行为训练；
- 修改只发生在 Candidate；
- 如何在 AGENTS、专题文档和 Skill 之间分流；
- 必须先检查现有内容再修改；
- 必须运行与变更风险相称的验证；
- 不能修改评测用例、发布规则、平台文件或密钥；
- 不得宣称发布成功，除非收到 Release Service 的真实结果。

### 11.2 每轮对话

1. 校验管理员、TrainingSession 所有权和状态，以 CAS 获取或接管本轮独占 Run lease；
2. 获取 Training Runtime；
3. 加载训练 Conversation History；
4. 执行目标 Agent；
5. 持久化消息、Tool 轨迹、Invocation 和成本；
6. 计算 Candidate diff；
7. 有变更时创建 checkpoint commit，commit message 包含 RunID，不包含消息正文；
8. 更新 `CandidateHead`，使之前的 CandidateValidationRun 和 EvaluationRun 失效；
9. SSE 返回回复、文件变更摘要和验证结果；
10. 释放本轮独占 Run lease；异常退出后 lease 到期即可安全重试。

即使 Run 失败，只要产生文件变更也创建标记为 `partial` 的 checkpoint，避免重启后
丢失中间产物；它不能成为发布 Candidate，必须由后续成功 Run 修复并重新校验。

### 11.3 Candidate 内部回退

管理员可以查看 checkpoint 列表并把 Candidate 重置到本 TrainingSession 的某个历史
checkpoint。不能通过该接口选择生产 Bundle 的任意 Git ref，也不能影响其他 Session。

### 11.4 关闭

- `published`：保留 Candidate ref 和审计，工作目录可异步清理；
- `cancelled`：删除工作目录，保留会话消息、checkpoint commit 引用和审计；
- Run lease 过期：不改变 TrainingSession 状态，只允许新请求接管并从 CandidateHead
  重新物化缺失工作目录；
- Session `ExpiresAt` 到期：状态变为 `expired`，释放 Agent 活跃训练指针，保留
  Candidate ref 和审计供管理员只读查看或显式克隆成新 TrainingSession。

## 12. Candidate 发布前验证

### 12.1 Gate A

训练文件工具在执行时：

- 所有路径必须是 Candidate 相对路径；
- 拒绝绝对路径、`..`、Volume path 和越界符号链接；
- 拒绝平台保留目录；
- 写入大小和文件总数受限；
- Tool 错误结构化返回给 Agent，使其可以修正。

`exec` 无法靠正则完整识别 shell 写入，因此 Gate A 不是发布授权依据。

### 12.2 Gate B

Gate B 在服务端、publish lock 内对 Base Bundle 与 CandidateHead 的真实树差异执行：

1. 枚举新增、修改、删除和类型变化；
2. 校验所有路径和文件类型；
3. 拒绝平台保留文件、Secret 模式、二进制可执行文件和越界链接；
4. 校验 Bundle 配额；
5. 调用 `harness.InspectWorkspace`；
6. 要求非空、UTF-8、普通文件 `AGENTS.md`；
7. 要求 Skill diagnostics 为空；
8. 校验 Skill 名称唯一、位置稳定、脚本引用存在；
9. 校验 `agent.yaml` KnownFields 和 Runtime 版本；
10. 对脚本执行静态检查和可执行性探针；
11. 拒绝空 diff；
12. 生成 ContentDigest 和 ToolPolicyDigest；
13. 持久化不可变 CandidateValidationRun。

Gate B 失败不修改 Bundle 主线、Release 或生产会话。

### 12.3 脚本策略

第一阶段支持 Bundle 内 `.sh` 和 `.py` 文本脚本：

- 必须位于 `skills/<id>/scripts/`；
- 必须被对应 Skill 明确引用；
- 不允许 setuid、原生二进制、动态库和嵌套包管理器；
- 运行时环境变量使用最小白名单，密钥通过 Tool/MCP 获取，不直接注入脚本；
- 默认无网络；需要外部访问时调用注册业务 Tool/MCP；
- Python 仅使用部署镜像已批准依赖；第一阶段不接受 `pip install`；
- 发布探针在与生产一致的沙箱中执行 `--help` 或 Skill 声明的 smoke command；
- 执行输出和超时进入 EvaluationRun，不进入 Bundle。

脚本 smoke 属于 CandidateValidationRun；只有在 Evaluation Case 内触发的脚本输出才
属于 EvaluationRun。

## 13. 自动评测与自主修正

### 13.1 评测隔离

Evaluation Runner：

- 从 CandidateHead 创建只读快照；
- 使用 Candidate Release 拟采用的 ModelRevision；
- 使用生产 ToolPolicy，而不是训练 ToolPolicy；
- 每个 Case 使用全新 History；
- 不加载管理员训练聊天；
- 不能写 Candidate；
- 不能读取其他 Case 的结果；
- Evaluation Suite 位于数据库/平台资源目录，不挂载进 Candidate。

如果业务 Tool 会写数据或产生外部副作用，Evaluation Runner 必须注入 Fake/dry-run 或
专用测试租户实现；无法安全替换的 Tool 对应 Case 直接判为“环境不满足”，不能调用生产
端点碰运气。

### 13.2 判分

判分顺序：

1. 确定性结构检查；
2. Tool trace 与参数断言；
3. 禁止行为和安全规则；
4. 文本/Schema/事实断言；
5. 可选独立 Judge Model。

Hard Case 必须全部通过。Soft Case 的阈值由 Evaluation Suite 固定；训练 Agent 无权
查看隐藏期望的原文，只获得适合修正的失败摘要。

### 13.3 自主修正循环

管理员可以在一次训练指令中选择“自动验证并修正”。流程：

```text
Candidate change
→ Gate B dry-run
→ Evaluation Run
→ 失败摘要
→ 训练 Agent 内部修正 Turn
→ 新 checkpoint
→ 重新评测
```

固定上限：

- 最多 3 次自动修正；
- 每次修正使用独立 RunLimits；
- 整个循环还有总 Token、成本和墙钟时间上限；
- 连续两次 Candidate digest 不变则停止；
- 连续两次失败集合不变则停止并交给管理员；
- 触发安全失败时不自动放宽规则；
- Judge Model 不能与 Author 共用训练聊天上下文。

达到门槛后 TrainingSession 进入 `ready`，但不会自动发布。

## 14. Fine-tuning 流水线

### 14.1 样本来源

以下内容不能自动进入训练集：

- 普通用户所有聊天；
- 管理员尚未确认的自由讨论；
- Tool Result 中的 Secret、完整业务记录或附件原文；
- Agent 自己生成并自行标记为正确的回答；
- 评测隐藏用例。

管理员可从训练会话选择：

- 正确示范；
- 对错误回答的最终修正；
- preferred/rejected 回答对；
- 期望 Tool 调用和参数；
- 分类或结构化输出样本。

创建 TrainingExample 前执行脱敏、角色合法性、Tool schema 和消息顺序校验。

### 14.2 Dataset Version

Dataset Service：

1. 读取已审核 TrainingExample；
2. 去重并拒绝训练/验证集合泄漏；
3. 按标签做稳定分层切分；
4. 转换成 Provider 无关规范格式；
5. 生成数据摘要、统计和脱敏报告；
6. 冻结 DatasetVersion；
7. 在 FineTune adapter 边界转换成具体 Provider 格式。

### 14.3 FineTune Backend

应用层端口：

```go
type FineTuneBackend interface {
    Submit(context.Context, FineTuneSpec) (ExternalFineTuneJob, error)
    Get(context.Context, string) (FineTuneStatus, error)
    Cancel(context.Context, string) error
}
```

基础设施实现负责接入支持 Fine-tuning 的云模型平台或私有训练服务。Webhook 与有界
轮询都只更新 FineTuneJob；不会直接修改 Agent ActiveRelease。

### 14.4 微调模型发布

外部任务成功后：

1. 创建 `ModelRevision(kind=fine_tuned)`；
2. 用目标 BundleVersion + 新 ModelRevision 构造待评测 `ReleaseSpec`；
3. 在相同 Evaluation Suite 上运行；
4. 与当前 ActiveRelease 做 Case、成本和延迟对比；
5. Hard Case 全过且阈值达标后允许管理员发布；
6. 发布只切换 Agent ActiveRelease；
7. 失败模型保留审计但不能承载生产流量。

微调不能替代 AGENTS、Skills 或工具。适合放进权重的是稳定的大量示范模式；经常变化
的业务规则、可执行流程和实时知识继续留在 Bundle/Tool/Knowledge Base。

## 15. Release 发布、传播和回滚

### 15.1 发布

`PublishCandidate` 在 Agent 分布式锁内执行：

1. 再次校验管理员权限；
2. 校验 TrainingSession=`ready`；
3. 校验 Candidate digest 与成功 CandidateValidationRun 完全一致；
4. 按 `ValidationMode` 校验发布证据：
   - `manual_only`：只允许 Phase 1，要求管理员在本次请求中明确确认“尚未自动评测”；
   - `evaluated`：要求成功 EvaluationRun 的 Candidate、ModelRevision、ToolPolicy 和
     Suite version 与待发布内容完全一致；
5. 校验 BaseRelease 仍是 Agent ActiveRelease；
6. 运行 Gate B 最终检查并生成新的 CandidateValidationRun；若 digest 或 Validator 结论
   变化，回到 `active` 重新审核，否则 Release 引用这次最终报告；
7. 服务端计算 Bundle SemVer 和 Release SemVer；
8. squash merge checkpoint 到 Bundle repository；
9. 创建不可变 Git commit/tag；
10. 物化只读 Release 目录；
11. 创建 BundleVersion 和 AgentRelease；
12. 原子更新 `agents.active_release_id`；
13. 清空 `active_training_session_id`；
14. 给 `follow_latest=true` 的生产会话设置 `pending_release_id`；
15. 写审计和发布事件；
16. 失效旧 Runtime cache，但不杀死正在执行的 Run。

Git 操作先完成，数据库指针最后提交。数据库提交前失败时删除未引用 tag/物化目录并
记录补偿；数据库提交后只允许向前修复，不回滚已经对外可见的 ActiveRelease 指针。

### 15.2 会话懒迁移

生产 Conversation 增加：

```text
agent_id
agent_release_id
pending_release_id
follow_latest
```

下一次 Run 取得会话锁后：

- 有 pending release 时更新 `agent_release_id`；
- 持久化内部 `agent.release.changed` 事件；
- 向该次 Run 注入简短 ContextBlock，告知行为资产已更新，应以当前 Workspace 为准；
- 可配置清除旧 Tool/Thinking 内部轨迹，但保留用户与最终 Assistant 文本历史。

训练和评测会话始终固定 Base/Candidate，不参与生产懒迁移。

### 15.3 回滚

回滚不是修改旧 Release，而是管理员把 `active_release_id` 指向一个已验证的历史
Release，并按同样方式设置会话 pending release。Bundle、模型和工具策略一起回滚。

被回滚 Release 保持不可变；需要修复时从它创建新的 TrainingSession。

切换指针前必须预检 Bundle digest、模型可用性、精确 Tool/MCP 实现版本、Runtime 兼容
范围和所有 SecretRef 是否可解析；任何一项缺失都拒绝激活且不改变生产指针。这里的
“完整回滚”指 Agent 行为配置整体回滚，不承诺撤销外部业务 Tool 已经产生的数据变更。

## 16. HTTP API

### 16.1 Agent 与 Release

```text
POST   /api/v1/agents
GET    /api/v1/agents
GET    /api/v1/agents/:agentID
GET    /api/v1/agents/:agentID/releases
POST   /api/v1/agents/:agentID/releases/:releaseID/activate
```

### 16.2 Training Session

```text
POST   /api/v1/agents/:agentID/training-sessions
GET    /api/v1/training-sessions/:trainingID
POST   /api/v1/training-sessions/:trainingID/runs
POST   /api/v1/training-sessions/:trainingID/cancel
GET    /api/v1/training-sessions/:trainingID/diff
GET    /api/v1/training-sessions/:trainingID/checkpoints
POST   /api/v1/training-sessions/:trainingID/checkpoints/:checkpoint/restore
POST   /api/v1/training-sessions/:trainingID/validate
POST   /api/v1/training-sessions/:trainingID/evaluations
POST   /api/v1/training-sessions/:trainingID/submit
POST   /api/v1/training-sessions/:trainingID/publish
```

Training Run 继续使用 SSE AgentEvent；额外事件：

```text
candidate_changed
candidate_checkpointed
candidate_validation_started|completed
evaluation_started|case_completed|completed
auto_repair_started|completed|stopped
candidate_ready
release_published
```

### 16.3 Fine-tuning

```text
POST   /api/v1/agents/:agentID/training-examples
GET    /api/v1/agents/:agentID/training-examples
POST   /api/v1/agents/:agentID/datasets
GET    /api/v1/datasets/:datasetID
POST   /api/v1/datasets/:datasetID/fine-tune-jobs
GET    /api/v1/fine-tune-jobs/:jobID
POST   /api/v1/fine-tune-jobs/:jobID/cancel
POST   /api/v1/fine-tune-jobs/:jobID/evaluate
POST   /api/v1/fine-tune-jobs/:jobID/publish
```

Fine-tune publish 只接受已成功评测且尚未发布的 Job，由 Release Service 重新校验
ReleaseSpec 与 EvaluationRun 摘要后创建 AgentRelease；它和 Training publish 共用同一
发布锁、版本分配、ActiveRelease CAS、Outbox 和回滚实现。

所有写接口从认证上下文派生管理员和租户，不接受客户端传入 owner/admin 身份。

### 16.4 管理员训练界面

Agent 详情页增加“训练”页签。开始训练前展示当前 ActiveRelease、模型、Bundle 版本和
“训练不会直接修改生产版本”的说明。训练工作台采用一个逻辑页面：

- 左侧是一对一聊天和本轮 Tool/验证轨迹；
- 右侧是按 checkpoint 分组的文件树、diff、Validation 和 Evaluation 报告；
- 顶部固定显示 BaseRelease、CandidateHead、状态、累计 Token/成本和 Session 过期时间；
- 底部操作只有“继续训练”“恢复 checkpoint”“验证/评测”“发布”“取消”；
- 一次 Run 未结束时禁用重复发送，断线重连按 RunID 恢复 SSE，不重复执行；
- Candidate 发生变化后立即清除旧的“可发布”标识；
- `stale` 时只提供放弃或基于最新 Release 重建，不展示可绕过检查的发布入口；
- Phase 1 发布对话框必须单独勾选“我确认此版本尚未经过自动评测”，不能预选；
- Phase 2 发布对话框展示相对基线的通过率、hard failures、成本和延迟变化；
- 发布成功后显示新 Release ID/版本；失败时保留 Candidate 和结构化错误，不能只弹通用
  “发布失败”。

管理员可以查看完整测试定义；发送给 Author Agent 的失败摘要必须由 Evaluation Service
单独生成，不能把隐藏期望、Judge 提示词或完整测试 fixture 拼回训练聊天。

## 17. 代码与文件划分

本节是开发落位约束，不是示意目录。文件名允许在实现计划中做小幅调整，但包职责、
依赖方向和“一个文件只负责一个主要原因”的边界不得合并回巨型 Service。

### 17.1 总体依赖方向

```text
HTTP Controller → Application Service → Domain Repository/Port
                                      → Agent Runtime Manager → pi
Infrastructure Adapter ───────────────→ Domain/Application Port

pi 不 import application/domain/infrastructure
domain 不 import application/infrastructure/pi
application 可以 import domain 和 pi 的公共契约
infrastructure 实现 domain/application 定义的端口
```

不能把 Agent、Training、Evaluation、Fine-tuning 全部写进现有
`application/service/chat/service.go`，也不能让 `cmd/server/app.go` 继续承担 Runtime
组装细节。现有 Chat 只负责面向普通用户的会话用例，训练是独立应用服务。

### 17.2 Phase 1A：`pi` WorkspacePolicy

| 文件 | 动作 | 单一职责 |
| --- | --- | --- |
| `pi/workspace_policy.go` | 新增 | 定义、规范化和校验 WorkspacePolicy |
| `pi/workspace_policy_test.go` | 新增 | 覆盖空策略、前缀逃逸、重复前缀和兼容行为 |
| `pi/agent.go` | 修改 | `Options` 接收策略，并把规范化结果传给 Workspace、sandbox 和 MCP |
| `pi/harness/inspect.go` | 新增 | 实现 `InspectWorkspace`，复用真实 AGENTS/Skill 解析路径 |
| `pi/harness/inspect_test.go` | 新增 | 保证检查结果与 ContextBuilder 的发现结果一致 |
| `pi/harness/tools/filesystem.go` | 修改 | Workspace 在打开写句柄前统一执行可写前缀检查 |
| `pi/harness/tools/filesystem_test.go` | 修改 | 覆盖 symlink、相对路径和 restricted prefix |
| `pi/harness/sandbox/write_policy.go` | 新增 | 把公共 WorkspacePolicy 翻译成后端无关沙箱写规则 |
| `pi/harness/sandbox/runner.go` | 修改 | Runner 构造显式接收写策略，不使用隐式全 Workspace 可写 |
| `pi/harness/sandbox/platform.go` | 修改 | 给 Host/Seatbelt/Bubblewrap 选择器传递同一策略 |
| `pi/harness/sandbox/seatbelt.go` | 修改 | 生成只读 Workspace 加可写子目录的 Seatbelt profile |
| `pi/harness/sandbox/bubblewrap.go` | 修改 | 使用 ro-bind Workspace，再 bind 可写前缀 |
| `pi/harness/sandbox/host.go` | 修改 | restricted + exec 无法保证时 fail closed |
| `pi/harness/sandbox/*_test.go` | 修改 | 对三个后端执行相同策略契约测试 |
| `pi/mcp/sandbox.go` | 修改 | stdio MCP 子进程继承当前 Runtime 的写边界 |
| `config/config.go` | 修改 | 增加服务端 Workspace policy 配置结构 |
| `config/validate.go` | 修改 | 启动时拒绝不安全或不可表达的配置 |
| `config/server_options.go` | 修改 | 把业务配置翻译成 `pi.Options` |
| `config.example.json` | 修改 | 显式展示生产 chat 的 restricted 策略 |
| `cmd/server/app.go` | 修改 | 过渡期显式选择策略，不能依赖零值 |

`edit.go`、`write.go`、`apply_patch.go` 不各自实现一份路径安全逻辑；三者都经过
`Workspace`。`exec/process` 的写限制由 sandbox 实现，Tool middleware 只提供更友好的
错误，不能替代 OS 边界。

### 17.3 Phase 1B：动态 Agent、Release 和 Runtime

新增领域与端口文件：

| 文件 | 内容 |
| --- | --- |
| `application/identity/principal.go` | Principal、Role、Context helper 和 `RequireAdmin` |
| `application/identity/resolver.go` | AdminIdentityResolver 端口和 unavailable 语义 |
| `domain/entity/agent/agent.go` | Agent 聚合根和 active release/training CAS version |
| `domain/entity/agent/template.go` | 原 Profile 的创建模板投影 |
| `domain/entity/agent/bundle_version.go` | 不可变 BundleVersion |
| `domain/entity/agent/model_revision.go` | base/fine-tuned ModelRevision |
| `domain/entity/agent/release.go` | 不可变 AgentRelease 和 ValidationMode |
| `domain/entity/agent/validation_run.go` | CandidateValidationRun |
| `domain/repository/agent/repository.go` | Agent 查询、创建和活跃指针 CAS |
| `domain/repository/agent/release.go` | Bundle/Model/Release/Validation 查询和原子发布提交 |
| `domain/repository/agenttemplate/catalog.go` | 只读模板 Catalog 端口 |
| `application/service/agentcatalog/service.go` | 创建/查询 Agent，解析 Template 初始值 |
| `application/service/agentcatalog/register.go` | Agent Catalog 的 fx 模块 |
| `application/service/agentruntime/spec.go` | RuntimePurpose、RuntimeSpec、ResolvedTool/MCP |
| `application/service/agentruntime/manager.go` | Acquire/Release/Invalidate/Close 生命周期 |
| `application/service/agentruntime/cache.go` | 单飞、引用计数、LRU 和 idle eviction |
| `application/service/agentruntime/resolver.go` | 从 AgentRelease 构造生产 RuntimeSpec |
| `application/service/agentruntime/tool_catalog.go` | Tool revision、Effect 和安全替身选择 |
| `application/service/agentruntime/register.go` | Runtime Manager 的 fx 模块和生命周期 |
| `infrastructure/persistence/agent/repository.go` | Agent 聚合的 MySQL 实现 |
| `infrastructure/persistence/agent/release.go` | 版本/Release/Validation 的 MySQL 实现 |
| `infrastructure/persistence/agent/model.go` | 仅持久化映射需要的私有 row 类型；领域实体不塞 JSON 细节 |
| `infrastructure/driver/agenttemplate/catalog.go` | 读取代码随附的 AgentTemplate Catalog |
| `infrastructure/driver/adminauth/resolver.go` | 把受信任 Admin Session/上游身份映射为 Principal |
| `infrastructure/driver/adminauth/register.go` | Admin Auth Resolver 的 fx 绑定；未配置时提供 disabled 状态 |
| `infrastructure/middleware/admin.go` | `RequireAdmin` HTTP guard；失败直接 401/403，不进入 Controller |
| `cmd/agent-bootstrap/main.go` | 一次性把内置 Profile 物化成初始 Bundle 和 Release |
| `workspaces/agent-templates/catalog.yaml` | 由现有 profiles/catalog.yaml 重命名的模板目录 |
| `workspaces/agent-templates/<code>/**` | 八个内置 Agent 的种子 AGENTS/Skills/references |
| `migrations/0007_agent_runtime.up.sql` | Agent/Bundle/Model/Release/Validation 表及 Conversation nullable 列 |
| `migrations/0007_agent_runtime.down.sql` | 仅回滚 schema；已发布 Bundle 由运维显式处理 |
| `migrations/0008_agent_conversation_finalize.up.sql` | 回填核验后增加 NOT NULL/FK/索引 |
| `migrations/0008_agent_conversation_finalize.down.sql` | 回到兼容 nullable 结构，不删除已迁移数据 |
| `migrations/0009_agent_profile_cleanup.up.sql` | 兼容窗口结束后删除 profile_code 和旧索引 |
| `migrations/0009_agent_profile_cleanup.down.sql` | 只恢复 nullable profile_code，不伪造历史值 |

需要修改的现有文件：

| 文件 | 修改点 |
| --- | --- |
| `domain/entity/conversation/conversation.go` | 增加 type、agent/release/pending/follow_latest/training_session 字段 |
| `domain/repository/conversation/*.go` | 查询条件和创建参数改为 Agent/Release，迁移期只读 ProfileCode |
| `infrastructure/persistence/conversation/*.go` | 映射新列；所有 owner 查询保留 UserID/TenantID 条件 |
| `conversation/store.go` | 业务 RunRequest 增加 TenantID；不把 ReleaseID 暴露给 HTTP 请求 |
| `conversation/runner.go` | 加载 Conversation 后向 Runtime Manager 获取 lease，再调用 `pi.Runner` |
| `conversation/register.go` | 由固定 `pi.Runner` 改为注入 Runtime Provider |
| `application/service/chat/service.go` | 创建会话时服务端解析 Agent.ActiveRelease |
| `application/service/chat/run_manager.go` | 删除 Profile Context 注入，记录 agent/release tracing 属性 |
| `common/dto/chat.go` | CreateConversation 从 `profile_code` 迁移到 `agent_id` |
| `common/vo/chat.go` | 返回 agent_id/release_id，兼容期保留 profile_code |
| `infrastructure/controller/http/chat/controller.go` | 只接收 agent_id，不允许客户端指定 release_id |
| `infrastructure/middleware/visitor.go` | 产出普通 Principal；不得把匿名 Session 提升为管理员 |
| `infrastructure/persistence/register.go` | 绑定 Agent/Release Repository 实现 |
| `infrastructure/controller/http/register.go` | 注册 Agent 查询 API；管理路由挂 RequireAdmin |
| `cmd/server/app.go` | 删除全局 `*pi.Agent`/`newAgentRunner`，改挂 Runtime Manager 生命周期 |

兼容版本保留 `domain/entity/agentprofile`、`domain/repository/agentprofile` 和旧
`GET /agent-profiles` 投影；0009 清理后删除旧包。代码仓库中的
`workspaces/agent-templates` 只用于创建新 Agent/执行 bootstrap，生产 Runtime 永远从
`<data-dir>/tenants/.../releases` 加载，不能回退读取模板目录。

现有 `workspaces/chat/skills` 中被 Template 引用的共享 Skill 在 bootstrap 时复制进初始
Bundle，使 Bundle 自包含；`workspaces/chat/AGENTS.md` 只保留平台级安全与回复纪律，不
复制业务角色内容。后续修改模板不会反向改变已创建 Agent。

身份认证本身是宿主系统责任。`adminauth/resolver.go` 没有配置可信实现时返回
`ErrAdminAuthUnavailable`，而不是读取 `X-Role: admin` 之类可伪造 Header。当前仓库要
独立提供管理员登录时，应另立认证设计，不混在 Training Service 内。Phase 1B 的生产
验收必须指定部署实际使用的 Resolver；只有测试 Fake 而没有生产身份来源不能算完成。

### 17.4 Phase 1C：Training、Candidate Git 和发布

| 文件 | 内容 |
| --- | --- |
| `domain/entity/agenttraining/session.go` | TrainingSession 状态机和不变量 |
| `domain/entity/agenttraining/checkpoint.go` | checkpoint/partial checkpoint 元数据 |
| `domain/repository/agenttraining/repository.go` | Session、lease、checkpoint 和状态 CAS |
| `application/service/agenttraining/service.go` | 依赖集合与公共鉴权/加载逻辑，不放具体用例 |
| `application/service/agenttraining/create.go` | 创建 Session、Conversation 和 Candidate |
| `application/service/agenttraining/run.go` | 获取 lease、执行目标 Agent、保存 Turn/checkpoint |
| `application/service/agenttraining/checkpoint.go` | diff、列表和 restore |
| `application/service/agenttraining/validate.go` | Gate B dry-run 和 ValidationRun |
| `application/service/agenttraining/submit.go` | 提交 Candidate，按部署阶段推进到 ready/evaluating |
| `application/service/agenttraining/cancel.go` | cancel/expire/stale 清理与保留规则 |
| `application/service/agenttraining/recover.go` | 重启恢复、lease 接管和 worktree 重物化 |
| `application/service/agenttraining/register.go` | Training Service fx 模块 |
| `application/service/agentrelease/service.go` | 发布、激活、回滚的统一入口 |
| `application/service/agentrelease/publish.go` | publish lock 内的 Gate B、Git、DB、补偿编排 |
| `application/service/agentrelease/activate.go` | 历史 Release 预检和 ActiveRelease CAS |
| `application/service/agentrelease/register.go` | Release Service fx 模块 |
| `application/tool/agenttraining/inspect.go` | `inspect_candidate` Tool |
| `application/tool/agenttraining/validate.go` | `validate_candidate` Tool |
| `application/tool/agenttraining/submit.go` | `submit_candidate` Tool；无 publish Tool |
| `application/tool/agenttraining/register.go` | 只向 training Runtime 注册作者 Tool |
| `application/port/agentbundle/store.go` | BundleStore、CandidateWorkspace、PublishedBundle 契约 |
| `application/port/agentlock/lock.go` | Agent 粒度分布式锁契约 |
| `infrastructure/driver/agentbundle/git_store.go` | bare repo、ref、tag 和物化目录 |
| `infrastructure/driver/agentbundle/worktree.go` | Candidate 创建、恢复和安全删除 |
| `infrastructure/driver/agentbundle/diff.go` | 真实树差异、digest 和配额统计 |
| `infrastructure/driver/agentbundle/publish.go` | squash commit/tag 与失败补偿 |
| `infrastructure/driver/agentbundle/register.go` | BundleStore 的 fx 绑定 |
| `infrastructure/driver/agentlock/redis.go` | 带 owner token/TTL 的 Redis publish lock |
| `infrastructure/driver/agentlock/register.go` | Agent Lock 的 fx 绑定 |
| `infrastructure/persistence/agenttraining/repository.go` | Training/Checkpoint MySQL 实现 |
| `infrastructure/controller/http/agent/controller.go` | Agent/Release 管理 API |
| `infrastructure/controller/http/agenttraining/controller.go` | Training REST/SSE API；只做绑定和响应 |
| `common/dto/agent.go` | Agent/Release 请求 DTO |
| `common/dto/agent_training.go` | 创建、Run、restore、publish 请求 DTO |
| `common/vo/agent.go` | Agent/Release 响应 VO |
| `common/vo/agent_training.go` | Session、diff、checkpoint、Validation、SSE VO |
| `migrations/0010_agent_training.up.sql` | Training、Checkpoint、idempotency、Outbox 表 |
| `migrations/0010_agent_training.down.sql` | 对应 schema 回滚 |

Gate B 的 Bundle 语义校验放在应用层 `agenttraining/validate.go`，Git 文件枚举与安全打开
放在 `agentbundle` adapter，AGENTS/Skill 解析继续复用 `pi/harness.InspectWorkspace`。
这样 Git 不依赖 `pi`，`pi` 也不依赖发布业务。

### 17.5 Phase 1D：管理员训练工作台

当前前端是 Go template + 原生 JS，不引入新的 SPA 框架。新增：

```text
frontend/templates/pages/admin-agents.html
frontend/templates/pages/admin-agent-training.html
frontend/templates/components/training-header.html
frontend/templates/components/training-chat.html
frontend/templates/components/training-diff.html
frontend/templates/components/training-validation.html
frontend/static/css/pages/admin-agents.css
frontend/static/css/pages/admin-agent-training.css
frontend/static/js/pages/admin-agents.js
frontend/static/js/pages/admin-agent-training.js
frontend/static/js/pages/training-stream.js
frontend/static/js/pages/training-diff.js
frontend/static/js/pages/training-state.js
frontend/static/js/pages/training_stream_test.mjs
frontend/static/js/pages/training_diff_test.mjs
frontend/static/js/pages/training_state_test.mjs
frontend/static/js/pages/admin_agents_test.mjs
```

修改 `frontend/assets.go`、`frontend/templates/partials/scripts.html` 和
`infrastructure/controller/http/page/controller.go` 注册 `/admin/agents` 和训练工作台页面。
Agent 列表负责选择目标 Agent 和展示 ActiveRelease，不复制训练状态逻辑。普通 Chat 的
`chat.js` 不掺入训练状态机，最多复用无业务状态的消息渲染函数。

### 17.6 Phase 2 与 Phase 3 文件

Phase 2 新增：

```text
domain/entity/evaluation/{suite.go,case.go,run.go,result.go}
domain/repository/evaluation/repository.go
application/service/agentevaluation/{service.go,runner.go,scorer.go,repair.go,register.go}
application/port/evaluation/tool_fake.go
application/tool/agenttraining/evaluate.go
infrastructure/persistence/evaluation/repository.go
infrastructure/driver/evaluation/{fake_tools.go,judge.go}
infrastructure/controller/http/evaluation/controller.go
common/dto/evaluation.go
common/vo/evaluation.go
migrations/0011_agent_evaluation.up.sql
migrations/0011_agent_evaluation.down.sql
```

Phase 3 新增：

```text
domain/entity/finetuning/{example.go,dataset.go,job.go}
domain/repository/finetuning/repository.go
application/service/finetuning/{service.go,example.go,dataset.go,job.go,publish.go,register.go}
application/port/finetuning/backend.go
infrastructure/persistence/finetuning/repository.go
infrastructure/driver/finetuning/backend.go
infrastructure/controller/http/finetuning/controller.go
common/dto/finetuning.go
common/vo/finetuning.go
migrations/0012_agent_finetuning.up.sql
migrations/0012_agent_finetuning.down.sql
```

`backend.go` 只实现 Phase 3 评审时选定的第一个真实 Provider，不预先创建空的多 Provider
文件。不同 Provider 的 JSON 转换只存在于 adapter，领域内统一使用规范消息格式。

### 17.7 核心代码骨架

以下代码是待审核的接口骨架，不是已实现代码；实现计划需要为每个接口先写失败测试。

管理员身份只从 Context 获取：

```go
package identity

type Role string

const (
    RoleUser  Role = "user"
    RoleAdmin Role = "admin"
)

type Principal struct {
    TenantID string
    UserID   string
    Role     Role
}

func FromContext(context.Context) (Principal, bool)
func RequireAdmin(context.Context) (Principal, error)
```

Runtime Manager 只接收服务端解析完成的规格：

```go
package agentruntime

type RuntimePurpose string

const (
    PurposeChat       RuntimePurpose = "chat"
    PurposeTraining   RuntimePurpose = "training"
    PurposeEvaluation RuntimePurpose = "evaluation"
)

type ToolEffect string

const (
    ToolReadOnly           ToolEffect = "read_only"
    ToolReversibleWrite    ToolEffect = "reversible_write"
    ToolExternalSideEffect ToolEffect = "external_side_effect"
)

type RuntimeLease interface {
    Runner() pi.Runner
    Release()
}

type RuntimeManager interface {
    Acquire(context.Context, RuntimeSpec) (RuntimeLease, error)
    Invalidate(context.Context, string) error
    Close(context.Context) error
}
```

Repository 不暴露无租户查询，也不让 Controller 直接操作 GORM：

```go
package agent

type Repository interface {
    Create(context.Context, *entity.Agent) error
    Find(context.Context, tenantID, agentID string) (*entity.Agent, bool, error)
    ReserveTraining(
        context.Context, tenantID, agentID, trainingID string, expectedVersion uint64,
    ) error
    ReleaseTraining(
        context.Context, tenantID, agentID, trainingID string, expectedVersion uint64,
    ) error
}

type ReleaseRepository interface {
    FindRelease(context.Context, tenantID, releaseID string) (*entity.AgentRelease, bool, error)
    CommitPublish(context.Context, CommitPublishCommand) error
    Activate(context.Context, ActivateCommand) error
}
```

`CommitPublish` 是一个 Repository 原子操作：插入 BundleVersion/AgentRelease、CAS 更新
Agent、清空 active training、写 Outbox。Application Service 不分别调用四个 Repo 后
假装它们处于一个事务。

状态转换集中在领域实体，不允许 Controller/Repository 任意赋字符串：

```go
package agenttraining

func (s *TrainingSession) Transition(next Status, now time.Time) error {
    if !allowedTransition(s.Status, next) {
        return ErrInvalidTransition
    }
    s.Status = next
    s.Version++
    s.UpdatedAt = now.UTC()
    return nil
}

func (s *TrainingSession) MarkCandidateChanged(head string, now time.Time) error {
    if s.Status != StatusActive && s.Status != StatusReady && s.Status != StatusRepairing {
        return ErrCandidateNotWritable
    }
    s.CandidateHead = head
    s.LastValidationRunID = nil
    s.LastEvaluationRunID = nil
    s.Status = StatusActive
    s.Version++
    s.UpdatedAt = now.UTC()
    return nil
}
```

Bundle/Git 通过应用端口隔离：

```go
package agentbundle

type Store interface {
    CreateCandidate(context.Context, CreateCandidateSpec) (CandidateWorkspace, error)
    RestoreCandidate(context.Context, RestoreCandidateSpec) (CandidateWorkspace, error)
    Checkpoint(context.Context, CheckpointSpec) (Checkpoint, error)
    Diff(context.Context, DiffSpec) (TreeDiff, error)
    Snapshot(context.Context, SnapshotSpec) (ReadOnlySnapshot, error)
    Publish(context.Context, PublishSpec) (PublishedBundle, error)
    DeleteUnreferenced(context.Context, PublishedBundle) error
    DisposeCandidate(context.Context, CandidateWorkspace) error
}
```

Gate B 是可组合 Validator，不与 HTTP 或 Git 命令输出耦合：

```go
type CandidateValidator interface {
    Validate(context.Context, ValidationSpec) (ValidationReport, error)
}

type ValidationSpec struct {
    TenantID        string
    AgentID         string
    TrainingID      *string
    Workspace       agentbundle.ReadOnlySnapshot
    BaseDigest      string
    ToolPolicy      agententity.ToolPolicySnapshot
    RuntimeVersion  string
}

type ValidationReport struct {
    CandidateHead   string
    CandidateDigest string
    ToolPolicyDigest string
    Diagnostics     []Diagnostic
    SmokeResults    []SmokeResult
    Passed          bool
}
```

`ValidationReport.Passed` 由 Validator 根据结构、路径、Secret、Skill diagnostics、配额和
smoke 结果计算；Agent 回复中的“测试通过”文本不能构造该对象。

Training Service 的公开方法与 HTTP 一一对应，但 Controller 不持有 Repository：

```go
package agenttraining

type Service struct {
    sessions  trainingrepo.Repository
    agents    agentrepo.Repository
    runtimes  agentruntime.RuntimeManager
    bundles   agentbundle.Store
    validator CandidateValidator
}

func (s *Service) Create(context.Context, CreateCommand) (*SessionView, error)
func (s *Service) Run(context.Context, RunCommand, pi.EventListener) (pi.RunResult, error)
func (s *Service) Diff(context.Context, Query) (*DiffView, error)
func (s *Service) Restore(context.Context, RestoreCommand) error
func (s *Service) Validate(context.Context, ValidateCommand) (*ValidationView, error)
func (s *Service) Submit(context.Context, SubmitCommand) (*SessionView, error)
func (s *Service) Cancel(context.Context, CancelCommand) error
```

`Publish` 不放在 Training Service，避免 Author 服务拥有生产切换能力：

```go
package agentrelease

func (s *Service) PublishTrainingCandidate(
    ctx context.Context,
    cmd PublishTrainingCommand,
) (*agent.AgentRelease, error) {
    principal, err := identity.RequireAdmin(ctx)
    if err != nil {
        return nil, err
    }
    return s.withAgentLock(ctx, principal.TenantID, cmd.AgentID, func(ctx context.Context) (*agent.AgentRelease, error) {
        session, evidence, err := s.loadAndRevalidate(ctx, principal, cmd)
        if err != nil {
            return nil, err
        }
        published, err := s.bundles.Publish(ctx, publishSpec(session, evidence))
        if err != nil {
            return nil, err
        }
        release := buildRelease(principal, published, evidence)
        if err := s.releases.CommitPublish(ctx, commitCommand(session, release)); err != nil {
            cleanupErr := s.bundles.DeleteUnreferenced(ctx, published)
            return nil, errors.Join(err, cleanupErr)
        }
        s.runtimes.Invalidate(ctx, runtimeKey(session.BaseReleaseID))
        return release, nil
    })
}
```

上面省略了日志字段和错误包装，但顺序是契约：先锁、再重读、再最终验证、再 Git、最后
DB 事务；DB 已提交后 Runtime invalidation 失败只能通过 Outbox/重试向前恢复。

Conversation Runner 动态选择 Runtime，但 `pi.RunRequest` 不出现业务 ID：

```go
conversation, err := repository.FindOwned(ctx, tenantID, userID, conversationID)
if err != nil {
    return pi.RunResult{}, err
}
lease, err := runtimes.AcquireChat(ctx, agentruntime.ChatIdentity{
    TenantID: tenantID,
    AgentID: conversation.AgentID,
    ReleaseID: conversation.AgentReleaseID,
})
if err != nil {
    return pi.RunResult{}, err
}
defer lease.Release()

return lease.Runner().Run(ctx, pi.RunRequest{
    History: history,
    Input: input,
    Context: contextBlocks,
    Limits: limits,
}, listener)
```

HTTP DTO 不接受管理员、租户、CandidatePath、版本号或 ReleaseID 等服务端事实：

```go
type CreateTrainingSessionDTO struct {
    ExpiresInSeconds int `json:"expires_in_seconds" binding:"omitempty,min=600,max=604800"`
}

type RunTrainingDTO struct {
    Content   string   `json:"content" binding:"required"`
    ImageURLs []string `json:"image_urls" binding:"omitempty,max=4,dive,http_url"`
}

// Phase 2 才加入。
type RunTrainingWithEvaluationDTO struct {
    RunTrainingDTO
    AutoRepair bool `json:"auto_repair"`
}

type PublishTrainingDTO struct {
    AcknowledgeManualOnly bool `json:"acknowledge_manual_only"`
}
```

`ValidationMode` 由 Release Service 根据部署阶段和有效证据派生，不能由客户端选择。
Phase 1 的 DTO 不包含 `AutoRepair`，也不注册 `run_candidate_evaluation`；Phase 2 增加该
字段和 Tool，避免向模型暴露永远失败的能力。

fx 组合根只组合模块：

```go
var Register = fx.Options(
    notice.Register,
    chattools.Register,
    agentcatalog.Register,
    agentruntime.Register,
    agenttraining.Register,
    agentrelease.Register,
    infrastructure.Register,
    conversation.Register,
    chatservice.Register,
)
```

`cmd/server/app.go` 不再直接调用一次全局 `pi.New`；只有 Runtime Manager factory 根据
Release/Training spec 调用 `pi.New`，并负责每个实例的 Start/Stop。

### 17.8 测试文件落位

- 每个领域状态机在同包 `<entity>_test.go` 做表驱动状态转换测试；
- 每个应用用例对应同名测试，例如 `run.go` 对应 `run_test.go`，不把所有场景塞进一个
  `service_test.go`；
- `agentbundle` 提供一套 Store contract test，使用临时 bare repo 同时验证 ref、diff、
  checkpoint、publish 和补偿；
- MySQL Repository 延续现有 sqlmock 单测，并增加真实 MySQL integration test 验证 CAS、
  FK 和发布事务；
- 各 `infrastructure/persistence/<module>/migration_test.go` 检查 0007～0012 的 up/down、
  表名、索引和危险 DROP，延续当前 Conversation migration test 的放置方式；
- HTTP Controller 测试只验证鉴权、DTO 绑定、错误码和 SSE framing；业务状态由 Service
  测试负责；
- 身份测试覆盖匿名 Visitor 永远是 user、伪造角色 Header 无效、缺少 Resolver 时管理
  路由不可用、真实 Admin Principal 才能进入 Service；
- Runtime Manager 增加并发 race test，覆盖相同 Key 单飞、活跃 lease 不回收、Stop 逆序
  和 Invalidate 与 Acquire 竞争；
- 前端继续使用 Node 内置 test runner，DOM/stream/state 各自测试；
- Phase 1D 增加 `infrastructure/controller/http/agenttraining/e2e_test.go`，跑通“创建训练
  → 对话修改 → diff → validate → manual-only publish → 普通会话懒迁移 → rollback”。

## 18. 数据库变更

新增表：

```text
agents
agent_bundle_versions
agent_model_revisions
agent_releases
agent_training_sessions
agent_training_checkpoints
agent_candidate_validation_runs
agent_evaluation_suites
agent_evaluation_cases
agent_evaluation_runs
agent_evaluation_case_results
agent_training_examples
agent_dataset_versions
agent_dataset_version_examples
agent_fine_tune_jobs
agent_idempotency_keys
agent_outbox_events
```

表按阶段创建：0007 只创建 Agent/Bundle/Model/Release/Validation，bootstrap 依靠
`tenant_id + template_code` 等唯一键保证幂等；0010 才创建
Training/Checkpoint/Idempotency/Outbox；0011 创建 Evaluation；0012 创建
TrainingExample/Dataset/FineTuneJob。上面的列表表示最终目标，不表示首个 migration 一次
创建全部表。

跨阶段引用先保存 nullable ID，不提前创建指向不存在表的外键：0007 中
`active_training_session_id`、`evaluation_run_id`、`dataset_version_id`、
`fine_tune_job_id` 只建列；0010/0011/0012 在目标表存在后再补对应 FK。0010 的
`last_evaluation_run_id` 同理在 0011 才补 FK。

修改 `agent_conversations` 的目标结构：

```text
ADD tenant_id VARCHAR(32) NOT NULL
ADD conversation_type VARCHAR(16) NOT NULL DEFAULT 'chat'
ADD agent_id VARCHAR(32) NOT NULL
ADD agent_release_id VARCHAR(32) NOT NULL
ADD pending_release_id VARCHAR(32) NULL
ADD follow_latest BOOLEAN NOT NULL DEFAULT TRUE
ADD training_session_id VARCHAR(32) NULL
```

既有表不能直接增加无默认值的 NOT NULL Agent/Release 字段。迁移顺序固定为：

1. 执行 `0007_agent_runtime.up.sql`：创建 Agent/Release 等新表，并以 nullable 形式增加
   `tenant_id`、`agent_id`、`agent_release_id`、`pending_release_id`、
   `training_session_id`；
2. 部署兼容版本，旧代码仍可读 `profile_code`，新代码同时支持新列；
3. 运维显式运行一次 `cmd/agent-bootstrap`，为八个 Profile 创建经验证的 Agent、Bundle、
   ModelRevision 和 AgentRelease；该命令使用幂等 key，可安全重试；
4. bootstrap 先把现有 Conversation 的 `tenant_id` 回填为部署配置的默认租户，再根据
   `profile_code` 分批回填 `agent_id` 和 `agent_release_id`；
5. 独立校验命令检查空值、未知 Profile、Bundle digest 和跨 Agent Release 引用，任何异常
   都停止发布；
6. 执行 `0008_agent_conversation_finalize.up.sql`，把 `tenant_id`、`agent_id`、
   `agent_release_id` 改为 NOT NULL，并创建外键和查询索引；
7. 应用切换为只读 `agent_id/agent_release_id`，停止写 `profile_code`；
8. 经过一个兼容版本后执行 `0009_agent_profile_cleanup.up.sql`，删除 `profile_code` 和旧
   Profile 索引；
9. Phase 1C、2、3 再分别执行 0010 Training、0011 Evaluation、0012 Fine-tuning，禁止
   在 Phase 1A 提前创建空表。

关键唯一约束：

- `agents(id)` 和 `agents(tenant_id, id)`；
- 内置 Agent 使用 `agents(tenant_id, template_code)` 唯一键保证 bootstrap 幂等；
- `agent_bundle_versions(agent_id, version)`；
- `agent_bundle_versions(agent_id, git_commit)`；
- `agent_releases(agent_id, version)`；
- `agent_training_sessions(conversation_id)`；
- `agent_training_examples(agent_id, source_run_id, content_digest)`；
- `agent_dataset_versions(agent_id, content_digest)`；
- `agent_fine_tune_jobs(provider_id, external_job_id)`。
- `agent_conversations(tenant_id, user_id, conversation_id)`。

活跃 Training Session 由 `agents.active_training_session_id` 的 CAS 更新约束，不依赖
MySQL partial unique index。

所有 Agent 聚合根和高风险子表（TrainingSession、Release、EvaluationRun、Dataset、
FineTuneJob）都保存 `tenant_id`。Repository 方法必须把认证上下文中的 TenantID 作为
必填查询条件；不能先按全局 ID 查出对象后再在 Controller 补做租户判断。子记录的
`tenant_id + agent_id` 还需通过应用服务和外键/一致性检查确认属于同一聚合。

## 19. 并发、错误和恢复

### 19.1 并发边界

- 同一 TrainingSession 每次只允许一个 Run；
- 同一 Agent 第一阶段只允许一个 active TrainingSession；
- 发布按 AgentID 获取分布式锁；
- Runtime 创建按 Runtime Key 单飞；
- Evaluation Case 可并发，但同一 Case 只有一个结果；
- FineTune Job 的 webhook/poll 更新通过状态版本做幂等 CAS。

Run lease 只保护一次执行并允许崩溃接管；Session `ExpiresAt` 才控制长期占用。同一 Agent
的活跃指针覆盖 `active|validating|evaluating|repairing|ready` 全部状态，不能通过进入
评测态绕过单 Candidate 限制。

创建 TrainingSession、Training Run、publish 和 FineTune submit 必须支持
`Idempotency-Key`，作用域为 `tenant + actor + route`，并持久化请求摘要和最终响应。
相同 key 但请求摘要不同返回冲突；对已 `published` Session 重试 publish 返回既有
Release，不再分配版本或重复发送事件。

### 19.2 基础 Release 漂移

如果 TrainingSession 创建后 Agent ActiveRelease 已改变：

- Candidate 进入 `stale`；
- 禁止直接发布；
- 管理员选择“放弃”或“基于最新 Release 重建 Candidate 并重新应用 diff”；
- 重建后所有旧 CandidateValidationRun 和 EvaluationRun 失效；
- 不自动把旧 Candidate 静默 merge 到新版本。

### 19.3 进程重启

- Training Conversation、CandidateHead 和 checkpoint 均持久化；
- Runtime 是可重建缓存；
- 重启后从 CandidateHead 重新物化缺失 worktree；
- EvaluationRun 执行中断后标记 `failed_interrupted`，不会猜测成功；
- FineTune Job 从外部 Provider 状态恢复；
- pending conversation migration 在下一 Turn 重试。

### 19.4 部分失败

| 失败点 | 处理 |
| --- | --- |
| Agent Run 失败且无变更 | 保存错误和 Invocation，不创建 checkpoint |
| Agent Run 失败但有变更 | 创建 partial checkpoint，禁止进入 ready |
| Gate B 失败 | 保持 Candidate，返回结构化 diagnostics |
| Evaluation 部分 Case 失败 | 保存所有已完成结果，Run 失败，不允许发布 |
| Bundle tag 后 DB 失败 | 删除未引用 tag/物化目录并记录补偿 |
| ActiveRelease 切换后事件失败 | 发布成功，事件进入 Outbox 重试 |
| 会话懒迁移失败 | 保持旧 release_id 和 pending_release_id，下次重试 |
| FineTune webhook 重复 | external_job_id + provider 幂等更新 |

## 20. 安全模型

### 20.1 信任边界

- 管理员身份来自服务端认证；
- TenantID 来自服务端认证，所有 Repository 查询和文件路径都按租户隔离；
- Agent 输出、Skill 文本、附件和网页内容均不可信；
- Candidate 可执行脚本不可信；
- FineTune Provider 状态是外部输入，需要结构化校验；
- Bundle Git repository 和 Release 表是发布事实来源；
- Evaluation Suite 和 Release Gate 是平台维护资源，绝不挂载可写。

### 20.2 强制要求

- 生产 Bundle 目录只读；
- Training Candidate 与其他 Agent、其他 Session 隔离；
- exec 默认禁止网络并使用最小环境；
- Secret 不进入消息、Bundle、diff、Invocation 内容或训练数据；
- 文件写入必须同时受 Workspace root 和 OS sandbox 限制；
- 发布再次扫描真实文件树，不能信任 Agent 自报修改清单；
- 所有版本和摘要由服务端计算；
- Fine-tuning 样本必须管理员审核；
- 所有发布必须引用同一 Candidate digest 的成功 CandidateValidationRun；
- Phase 2 起发布还必须引用同一 ReleaseSpec 的成功 EvaluationRun；
- Agent 不能修改阈值、跳过 Case 或把失败改成成功；
- 第一阶段不提供 Agent self-publish Tool。

### 20.3 Workify 经验带来的额外防线

Workify 的 Gate A 不解析 bash，说明 Tool 名称/参数 deny-list 不能成为文件安全边界。
go-reagent 必须把以下两项作为硬要求：

1. `chat/evaluation` exec 在 OS 沙箱层只写 scratch/tmp；
2. 任何 Training exec 写入最终都必须经过 Gate B 的真实树检查后才能发布。

## 21. 可观测性与审计

新增 Span：

```text
agent.training.run
agent.candidate.validate
agent.evaluation.run
agent.evaluation.case
agent.release.publish
agent.release.activate
agent.dataset.build
agent.finetune.submit
agent.finetune.poll
```

关键属性只记录 ID、状态、计量和摘要，不记录消息正文、文件正文、Secret 或完整 Tool
参数：

```text
agent.id
agent.release.id
agent.bundle.digest
training.session.id
training.candidate.digest
evaluation.run.id
evaluation.case.id
finetune.job.id
termination.reason
run.total_tokens
run.cost_usd
```

审计事件：

```text
agent.created
agent.training.started
agent.training.checkpointed
agent.training.validation_failed
agent.training.evaluated
agent.training.auto_repair_stopped
agent.training.submitted
agent.training.cancelled
agent.release.published
agent.release.activated
agent.release.rolled_back
agent.training_example.approved
agent.dataset.created
agent.finetune.submitted
agent.finetune.completed
agent.finetune.failed
```

## 22. 现有 Profile 与会话迁移

1. 把八个 `workspaces/chat/profiles/<code>` 重命名并转换到
   `workspaces/agent-templates/<code>`，作为八个内置 AgentTemplate；
2. 为每个 Template 运行 Workspace Validator；全部通过后创建来源为 `migration` 的
   CandidateValidationRun，以及初始 Agent、BundleVersion、基础 ModelRevision 和
   AgentRelease，任一验证失败则停止迁移；
3. `workspaces/chat/AGENTS.md` 中真正的平台通用纪律保留为平台 Context，不复制到每个
   Agent Bundle；Profile AGENTS 成为各初始 Bundle 的 `AGENTS.md`；
4. Profile 专属 Skills 和被模板引用的 `workspaces/chat/skills` 在 bootstrap 时复制进
   对应 Agent Bundle 的 `skills/`；
5. 现有 Conversation 按 `profile_code` 映射到对应内置 Agent 和初始 Release；
6. 新会话 API 改为提交 `agent_id`；
7. `GET /agent-profiles` 在兼容窗口内改为返回 AgentTemplate 投影，前端迁移完成后删除；
8. 迁移期间 `profile_code` 只读保留一个版本，之后通过独立 migration 删除；
9. 不允许缺失映射时静默回退到 general，迁移应失败并报告具体 code。

## 23. 实施阶段

这是总架构设计，不应被展开成一个超大实现计划。审核通过后按以下依赖顺序分别编写、
评审和执行实现计划，每个阶段通过验收后再进入下一阶段：

1. Phase 1A：`pi` WorkspacePolicy、沙箱写边界、InspectWorkspace；
2. Phase 1B：Agent/Release 领域模型、Profile 迁移、Runtime Manager、生产只读运行；
3. Phase 1C：TrainingSession、Candidate Git、checkpoint、Gate A/B、人工发布与回滚；
4. Phase 1D：管理员训练工作台和完整端到端验收；
5. Phase 2：Evaluation 与有限自主修正；
6. Phase 3：Fine-tuning；
7. Phase 4：规模化能力。

本文件获批后首先只为 Phase 1A 编写实现计划，不把后续阶段混进同一个开发批次。

### Phase 1：动态 Agent 与人工发布的资产训练

- Agent、BundleVersion、基础 ModelRevision、AgentRelease、TrainingSession；
- Profile → AgentTemplate 迁移；
- Runtime Manager；
- Tool/MCP Effect Catalog 和 Training 安全替身策略；
- Training Chat、Candidate Workspace、checkpoint；
- `AGENTS.md`、文档、Skills、`.sh/.py` 脚本；
- Gate A、Gate B；
- 不可变 CandidateValidationRun；
- 管理员 diff 审核、发布、回滚；
- Chat Runtime 只读策略；
- Conversation 绑定 AgentRelease。

Phase 1 发布条件：没有自动评测时，只允许管理员明确勾选“尚未自动评测”，并在审计中
标记 `manual_only`。默认发布按钮要求至少通过 Workspace Validator 和脚本 smoke。

### Phase 2：Evaluation 与有限自主修正

- Evaluation Suite/Case/Run；
- 生产权限 Evaluation Runner；
- Tool trace、Schema、安全和 Judge 断言；
- 最多三轮自动修正；
- Candidate ready 门槛；
- 发布强制绑定成功 EvaluationRun；
- 基线 Release 回归对比。

Phase 2 完成后移除 `manual_only` 发布例外。

### Phase 3：Fine-tuning

- TrainingExample 审核；
- DatasetVersion 和脱敏；
- FineTuneBackend 端口及第一个 Provider 实现；
- FineTuneJob 状态机；
- Fine-tuned ModelRevision 生命周期；
- 微调模型与基础模型对照评测；
- Bundle + Model 联合 AgentRelease。

### Phase 4：规模化与高级能力

- 对象存储 Bundle；
- 分布式 Runtime cache；
- 多管理员协作和 Candidate merge；
- Agent 构建其他 Agent；
- Bundle Extensions/Hooks；
- 多 Provider Fine-tuning；
- 灰度流量与在线指标自动回滚。

## 24. 测试策略

### 24.1 `pi` SDK

- `WorkspaceWriteRestricted + AllowWrite=false` 不注册文件写工具；
- Seatbelt/Bubblewrap 下 exec 不能修改 Bundle；
- exec 可写允许的 scratch/tmp；
- Host backend 无法保证策略时启动失败；
- `WorkspaceWriteAll + AllowWrite=true` 保持当前 Coding 行为；
- `InspectWorkspace` 与真实 ContextBuilder 对同一 Workspace 结论一致；
- symlink、路径逃逸和特殊文件被拒绝。

### 24.2 Training Service

- 非管理员不能创建、读取、运行或发布 TrainingSession；
- 同一 Agent 第二个 active TrainingSession 被拒绝；
- Candidate 从准确 BaseRelease 创建；
- 每轮文件变更生成 checkpoint；
- 失败 Run 只生成 partial checkpoint；
- restore 只能选择本 TrainingSession checkpoint；
- Candidate 修改使旧 CandidateValidationRun 和 EvaluationRun 失效；
- Run lease 到期可接管，但不会把 Session 错误标记为 stale；
- Session 到期进入 expired 并释放 Agent 活跃训练指针；
- 服务重启后能恢复 Candidate。

### 24.3 Gate B 与发布

- 合法 AGENTS、Skill、脚本能发布；
- 保留路径、Secret、越界链接、特殊文件、超限 Bundle 被拒绝；
- Skill diagnostics、重复名称、缺失脚本引用被拒绝；
- 空 diff 被拒绝；
- 两个并发发布只产生一个新版本；
- BaseRelease 漂移使 Candidate stale；
- Git/DB 各失败点执行预期补偿；
- Release digest 与物化目录一致；
- 所有 Release 都绑定同 digest 的成功 CandidateValidationRun；
- `manual_only` 只在 Phase 1 且管理员显式确认时可发布；
- 回滚同时切换 Bundle、Model 和 ToolPolicy；
- 历史 Release 的模型、Tool 版本或 SecretRef 不可用时，激活预检失败且生产指针不变。

### 24.4 Evaluation

- 每个 Case 无共享 History；
- Evaluation Runner 不能写 Candidate；
- Training/Evaluation 的副作用 Tool 只能命中 Fake、dry-run 或测试租户；
- 训练 Agent 不能读取或修改隐藏期望；
- hard failure 阻止 ready/publish；
- Candidate digest 变化阻止复用旧报告；
- 自动修正达到轮次/预算/无进展上限会停止；
- Tool 未调用、Tool 报错和业务失败能正确区分。

### 24.5 Fine-tuning

- 未审核 Example 不能进入 Dataset；
- 脱敏失败阻止 Dataset 冻结；
- Dataset digest 稳定；
- webhook/poll 幂等；
- 外部成功不会自动发布；
- 新 ModelRevision 必须重新评测；
- 失败或取消 Job 不能进入 AgentRelease。

### 24.6 端到端

1. 管理员从 writing Agent v1 创建训练会话；
2. 要求新增带 Python 脚本的 Skill；
3. Agent 修改 Candidate，页面展示 diff/checkpoint；
4. 普通用户会话仍运行 v1，且不能看到训练工具；
5. 运行评测，失败摘要触发一次自动修正；
6. Candidate 进入 ready；
7. 管理员发布 v2；
8. 新会话立即使用 v2；
9. 旧会话下次 Turn 懒迁移并收到变更提示；
10. 管理员回滚 v1，Bundle、Model、ToolPolicy 一起恢复；
11. 管理员把一个修正审核成 TrainingExample；
12. Dataset → FineTuneJob → ModelRevision → Evaluation → 新 Release 完成闭环。

## 25. 验收标准

- 管理员能在 UI 内完成创建训练会话、聊天修改、查看 diff、评测、发布和回滚；
- 训练回复确实来自目标 Agent 身份，不是另一个通用 Trainer；
- 普通用户不能获得训练工具或写入 Candidate；
- 任何生产 Run 都不能修改已发布 Bundle；
- 训练失败或服务重启不会污染 ActiveRelease，也不会丢失已 checkpoint 的 Candidate；
- 发布必须基于服务端计算的真实 Candidate digest；
- Phase 2 后，没有成功 EvaluationRun 的 Candidate 不能发布；
- AgentRelease 能完整复现 Bundle、模型、工具策略和运行配置；
- 回滚不依赖重新构建旧配置；
- Fine-tuning 数据只来自管理员审核样本；
- Fine-tuning 成功不等于发布成功，必须重新评测并由管理员批准；
- `pi.Runner`、`pi.RunRequest` 和 `pi.RunResult` 不承载训练业务字段。

## 26. 已定决策

1. 采用 Workify 式同一逻辑 Agent 自训练，不创建独立 Trainer Agent。
2. 使用显式管理员 TrainingSession，不把训练能力开放给普通 Chat。
3. TrainingSession 使用独立 Candidate Workspace 和独立 `pi.Agent` Runtime。
4. 生产 Workspace 只读；允许执行脚本不等于允许修改 Bundle。
5. Training Agent 可修改资产、验证和提交 Candidate，但不能发布。
6. Gate A 提供即时反馈，Gate B 是发布权威边界。
7. 第一阶段一个 Agent 只允许一个 active TrainingSession。
8. Git BundleVersion 与完整 AgentRelease 分层。
9. AgentRelease 固定 Bundle、Model、ToolPolicy、RuntimeConfig、ValidationRun，并在
   `evaluated` 模式固定 EvaluationRun。
10. 现有 Profile 降级为创建 Agent 的 Template，不再承担运行身份。
11. 自动评测由独立 Evaluation Runner 执行，训练 Agent 不修改用例和阈值。
12. 自主修正最多三轮且受独立总预算限制。
13. 第一阶段默认管理员发布，不提供 Agent self-publish Tool。
14. Fine-tuning 在应用/基础设施层实现，`pi` 只消费最终模型引用。
15. 普通聊天不会自动进入训练集，训练样本必须管理员审核。
16. 发布和回滚以 AgentRelease 为单位，模型与 Bundle 一起切换。
