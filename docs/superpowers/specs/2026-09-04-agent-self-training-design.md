# Agent 资产自训练与人工发布设计（简化版）

## 状态

本文是技术主规格；[多 Agent 平台详细方案](2026-09-09-multi-agent-platform-design.md)
补充名称/用途、独立训练和选择后聊天的产品流程。两份文档须联动审核；补充方案中的
新增接口及流程在其 §15 显式列出，不视为原规格已经包含或已获开发批准。
2026-09-09 用户已明确要求按补充方案开发，进入实施；部署和真实数据迁移另行授权。

2026-09-09 按已确认的第一版范围收敛：一个客户组织拥有多个不同用途的 Agent，管理员
通过聊天修改目标 Agent 的行为文件，校验后人工发布。本文是设计，不代表已经实现。
原完整平台方案保留在 Git 历史；本版替代其数据库、业务流程和阶段安排，不再要求实现
自动评测、模型微调、独立模型注册或可靠事件投递平台。

当前参考基线：go-reagent `master@9eb5e37` 的运行结构，以及 Workify 本地仓库的
`packages/store-sqlite/src/schema.ts`、`packages/store-spec/src/types.ts` 和训练发布源码。
Phase 1A 实现时仍须以实际 checkout 检查调用位置。

## 1. 范围与主要决策

第一版新增三张业务表：

```text
agents
agent_versions
agent_training_sessions
```

保留以下能力：

- 一个客户组织（Tenant）有多个用户及多个 Agent；管理员只能训练本租户 Agent。
- 同一个逻辑 Agent 在独立训练目录中修改自己的 AGENTS.md、Skills、脚本和资料。
- 一次训练会话包含多轮对话；文件变化由 Git checkpoint 保存，可查看 diff 和恢复。
- 生产行为文件只读；训练候选与生产、其他 Agent、其他会话隔离。
- 服务端校验真实候选内容，管理员明确确认后发布。
- 一个不可变 AgentVersion 同时固定 Git 文件引用、模型配置和工具策略；回滚整体切换。
- 保留当前聊天页面的助手选择、历史记录、图片输入、流式回复和会话管理。

第一版不实现自动判分、自主修正循环、数据集、Fine-tuning、多管理员共同编辑、跨节点
训练接管、Agent 自行发布或创建其他 Agent。也不预建这些功能的空表、DTO 和 API。
部署范围为单个服务进程 + MySQL + 持久化文件盘；这允许服务多个租户，但不承诺多节点
写入协调。现有 Redis 等项目依赖继续按原有用途使用。

安全边界不因表数减少而取消：管理员认证、租户校验、OS 沙箱、实际文件检查、不可变
版本、发布原子切换及重复请求处理仍属于第一版要求。

## 2. Workify 的参考方式

Workify 的相关真实表及用法：

| 表 | 用途 |
| --- | --- |
| `agents` | 租户、Agent 身份、实时配置、能力、Bundle 路径及最新版本 |
| `agent_versions` | Bundle Git commit/tag、发布者、来源会话 |
| `sessions`、`session_members` | 会话及参与者，成员保存 Agent 版本、配置快照、独立 cwd 和权限覆盖 |
| `messages`、`turns`、`runtime_events` | 聊天及执行过程 |
| `model_providers`、`model_profiles` | 模型提供方和运行配置 |
| `audit_logs`、`outbox` | 审计和异步投递 |

训练复用普通会话和成员，通过临时 `agent.train_self` 能力修改成员 cwd，完成训练后
生成 Git 版本并登记 agent_versions。运行实例按 member.id 创建。证据在
`packages/server/src/session-member-provisioning.ts`、`agent-runtime/manager.ts` 和
`routes/agent-training.ts`。Workify 的版本只固定 Bundle，模型配置是实时状态。

Workify 使用两层检查：工具调用时检查路径和权限，发布时再次检查真实 Git diff。
Gate A 不解析 bash 写入，因此不能作为唯一安全边界。对应文件为
`packages/agent/src/runtime-adapters/gate-a.ts` 与 `packages/server/src/bundle-commit.ts`。
其 Bundle gitignore 中的附件由 `.workify/` 条目覆盖，并非独立条目。

本版借鉴会话独立目录、Git 文件历史和人工发布，不复制 Workify 全部业务表。
go-reagent 没有 SessionMember 模型，因此保留一张独立训练会话表，复用现有聊天存储；
为保证整体回滚，把模型和工具快照直接放入 agent_versions，而不再拆 Bundle/Model/Release。

## 3. 身份、归属和现有代码边界

```text
客户组织 Tenant
└── 多个 Agent
    ├── 各自的 AgentVersion 历史
    └── 各自的 TrainingSession 历史
        └── 一个管理员 + 多轮训练聊天 + Git checkpoint
```

Tenant 与 User 不同：Tenant 表示客户组织，User 表示操作人。可信宿主认证提供
`Principal{TenantID, UserID, Role}`，不接受请求正文或任意 Header 自报管理员/租户。
没有客户身份系统的明确单客户部署可以固定 TenantID 为 `default`；同一部署服务多个
客户时必须使用不同租户身份。未配置可信管理员认证则不注册管理页面和管理写 API。

当前 visitor.go 只提供匿名 Cookie 用户；它不能自动成为客户组织登录或管理员身份。
现有普通聊天记录按用户隔离，迁移后同时按 tenant_id + user_id 隔离；训练历史仅本会话
管理员可访问。ID、工作目录名和 Git ref 均不能代替服务端归属校验。

当前 server 在启动时构造一次 pi.Agent，Profile 只是只读目录及上下文注入。
pi.Agent 固定 WorkDir、模型和工具，聊天历史由上层传入。因此应用层应按会话/版本
创建运行实例；不把业务 ID、数据库或 Git 放进 pi.RunRequest。

## 4. 数据模型：三张表各自负责什么

### 4.1 agents：稳定身份和当前指针

| 字段 | 作用 |
| --- | --- |
| id、tenant_id | 稳定身份和所属客户 |
| name、description、status | 展示信息；status 为 enabled 或 archived |
| presentation_json | 图标、欢迎语、推荐问题和排序 |
| template_code、bootstrap_key | 创建来源；只有内置迁移种子设置 bootstrap_key |
| active_version_id | 当前生产版本，创建未完成时允许为空 |
| active_training_session_id | 当前活跃训练会话；同一 Agent 只允许一个 |
| row_version | CAS 并发版本，与业务发布版本号区分 |
| created_by、created_at、updated_at | 创建与更新时间 |

同租户可以从同一模板创建多个 Agent；只对 `(tenant_id, bootstrap_key)` 建唯一键，
普通创建使用 NULL，不对 template_code 建唯一键。展示信息从模板复制，之后独立编辑，
不直接注入系统提示词；真正行为规则保存在 AGENTS.md。

第一版不做租户持久化默认助手设置，不增加设置表。列表按 presentation.order、id 排序，
服务端将第一个可用 Agent 作为 default_agent_id 返回；无可用项则为 null。这个字段是
列表响应的计算结果。归档后自然排除，不存在需要同步清理的数据库默认指针。

### 4.2 agent_versions：完整、不可变的运行版本

| 字段 | 作用 |
| --- | --- |
| id、tenant_id、agent_id、version | 版本身份及 Agent 内单调递增整数版本号 |
| bundle_commit、bundle_tag、bundle_digest | Git 文件树引用和服务端计算的内容摘要 |
| model_config_json | Provider、具体模型 ID、推理参数、必要能力配置和 SecretRef |
| tool_policy_json、runtime_config_json | 精确工具/MCP 实现引用、参数、沙箱和运行限制 |
| spec_digest | 文件摘要 + 规范化运行配置的联合摘要 |
| validation_json | 本次发布校验结果、校验器版本、摘要、时间、脚本 smoke 结果 |
| source_training_session_id | 来源训练会话；初始创建为 NULL |
| published_by、published_at、change_summary | 发布人、时间及简短变更说明 |

字段在创建后不可改写；激活状态只由 agents.active_version_id 表达。没有单独的 Bundle、
ModelRevision、Release 或验证记录表。同一文件树可以被多个完整版本引用。

摘要协议第一版固定为 `digest_version=1`，校验、发布、激活和恢复共用同一实现：

- `bundle_digest` 为 `sha256:<小写十六进制>`，输入为 RFC 8785 JCS 规范化后的
  `{digest_version:1, entries:[...]}` UTF-8 字节。entries 枚举完整 Git tree 的叶子项，
  按仓库相对路径的 UTF-8 字节序排序；每项固定包含字符串 `path`、六位八进制字符串
  `mode`（100644/100755/120000）、非负整数字节数 `size` 和不带前缀的 64 位小写
  十六进制字符串 `content_sha256`。普通文件对原始内容字节计算摘要；允许的相对符号
  链接对链接目标原始字节计算摘要，不跟随链接。路径必须是合法 UTF-8，使用 `/`，不做 Unicode 或
  换行归一化。目录由路径隐含，mtime、owner、commit 作者/时间、tag 不参与摘要。
  Git tree OID 仅用于定位，不直接充当 bundle_digest；§5.3 临时区不进入树或清单，
  其余未跟踪/脏文件不得静默忽略。危险类型按 §7.3 拒绝，不能靠摘要合法化。
- `spec_digest` 同样为 `sha256:<小写十六进制>`，输入为 JCS 规范化后的
  `{digest_version:1, bundle_digest, model_config, tool_policy, runtime_config}`。
  三份配置按版本化 schema 校验，拒绝未知字段、重复键及不符合 JCS 的数值；先解析
  服务端允许的引用并补齐有效默认值，再保存并计算摘要。对象键及数字按 JCS 处理，
  数组保序；可选字段缺省统一省略，除 schema 明确允许外拒绝 null。之后从已保存的
  有效快照计算，不用新的宿主默认值重填。SecretRef 参与摘要，Secret 正文不参与。
- validation_json 同时保存 `digest_version`、`bundle_digest`、`spec_digest`；算法或
  配置 schema 的语义变化须升级 digest_version。旧版本摘要按其记录的协议验证，
  不改写历史摘要；不支持的协议拒绝激活并报告原因。

基础模型来自服务端已有 Provider 配置；管理员可在创建或训练会话配置中选择允许的
模型，平台校验可用性和能力，再把有效配置完整复制到版本行。配置文件后续修改不能
悄然改变已发布版本。密钥只存引用，不保存正文。

有固定模型版本 ID 时优先固定；平台只能固定所请求的模型引用和参数，不能保证外部
Provider 永远不修改权重或接口，也不承诺模型输出逐字可复现。旧模型/工具/密钥引用
不可用时拒绝重新激活并返回具体原因。

validation_json 使用有界结构保存检查项、诊断、摘要和 smoke 结果，不塞无限日志。
没有自动评测时明确记录 `review_mode=manual`；不能把结构验证成功标成模型质量评测通过。
训练页面的临时检查记录可被下一次检查替换，真正发布证据保存在版本行中不可变。

### 4.3 agent_training_sessions：一次训练的候选状态

| 字段 | 作用 |
| --- | --- |
| id、tenant_id、agent_id、admin_user_id | 训练身份、归属及创建管理员 |
| conversation_id、base_version_id | 关联已有聊天会话，固定创建时的生产基线 |
| candidate_head、candidate_config_json | 当前 Git HEAD 和拟发布的完整配置副本 |
| candidate_partial | 当前候选是否仍含未完成的失败 Turn 改动 |
| status、row_version、expires_at | 训练状态、CAS 和明确过期时间 |
| validation_json | 最近校验结果，必须绑定当前 candidate_head 与 spec_digest |
| operation_json | 单个当前操作：ID、类型、请求摘要、状态、结果摘要、时间及恢复信息 |
| result_version_id | 发布成功后的版本 ID，用于重试返回既有结果 |
| created_at、updated_at | 时间信息 |

候选路径由服务端根据三个归属 ID 派生，不在业务表保存机器绝对路径。
operation_json 是单操作槽，不追加无限数组：历史消息、工具输出和模型计量分别复用
agent_messages 和已有 invocation 存储；checkpoint 历史通过 Git 引用查询。

训练期限由 `agent_training.session_ttl_seconds` 配置，默认 86400（24 小时），必须为
正整数；创建事务使用数据库 UTC 时间设置 `expires_at=created_at+TTL`，固定期限，
聊天、校验、查询均不续期，后续配置变更不改已有 expires_at。Phase 1C 将该默认值
写入 config.example.json。过期准入和运行中操作的停止规则见 §8.4。

### 4.4 复用与延后

| 内容 | 第一版落点 |
| --- | --- |
| 普通/训练聊天 | 现有 agent_conversations、agent_messages |
| 模型执行与 Token/成本 | 现有 model invocation 存储 |
| checkpoint | Git commit + 持久 refs，训练表保存当前 HEAD |
| 最近校验 / 发布证据 | 训练表 validation_json / 版本表 validation_json |
| 发布重试 | 唯一来源训练会话 + result_version_id |
| 前端重连 | 查询当前操作、消息和候选快照，不保证逐事件完整回放 |
| 自动评测、训练数据、微调 | 不在本版创建表或注册接口 |
| 独立事件日志、幂等键平台、Outbox | 不在本版建设；具体重试与发布传播见 §8 |

现有模型调用流水并非任意 SSE 事件日志，不能宣称复用它就能按游标重放所有事件。

## 5. 文件布局与运行加载

### 5.1 目录

`<data-dir>` 是可配置的持久化数据根目录。按客户 → Agent → 版本/训练组织：

```text
<data-dir>/tenants/<tenant-id>/agents/<agent-id>/
├── bundle.git/                           # 权威 Git 仓库
├── versions/<version-id>/                 # 已发布行为文件，纯文件快照，只读
├── training/<training-session-id>/
│   ├── candidate/                        # 独立 worktree，作者只修改这里
│   │   └── .tmp/                         # 平台临时区，不进入版本
│   └── state/                            # 平台临时报告，不供作者根外访问
└── runtime-cache/
    ├── chat/<conversation-id>/<version-id>/
    │   ├── AGENTS.md                      # 版本文件只读副本，其余文件同样物化
    │   ├── skills/
    │   ├── scratch/                       # 本会话临时产物
    │   └── .tmp/
    └── validation/<operation-id>/         # 校验/脚本 smoke 独立只读副本和临时区
```

Bundle 内容固定为：

```text
AGENTS.md
skills/<skill-id>/SKILL.md
skills/<skill-id>/scripts/
skills/<skill-id>/references/
skills/<skill-id>/assets/
documents/
assets/
```

简单身份和单条规则放 AGENTS.md；专题规则放 documents 并简短引用；多步骤流程放
skills，必要时附脚本。第一版采用固定入口和目录，不引入 agent.yaml 清单协议；兼容性
由平台版本和发布校验记录判断。大知识库、密钥和平台生成配置不进入 Bundle。

第一版每个 versions/<version-id>/ 都全量物化，包括仅模型/工具配置变化的版本；Git
文件树可以复用，磁盘快照仍独立。这里有意以磁盘占用换取物化、权限和回收的简单性，
不使用跨版本可写硬链接，也不在第一版引入文件去重存储。

### 5.2 加载路径

1. 页面列举 Agent 时仅按租户查数据库，不扫描所有目录或启动全部实例。
2. 新聊天提交 agent_id，服务端核验租户及 enabled 状态，绑定 active_version_id。
3. 消息执行前加载该会话及 AgentVersion，将版本文件复制到会话 WorkDir，再装配快照
   中的模型、工具和权限；只读文件不能使用连接权威文件的可写硬链接或越界符号链接。
4. WorkDir 根和行为文件只读，仅 scratch/.tmp 可写，系统临时目录不得暴露其他会话数据。
5. 当前会话历史单独传入 pi.RunRequest；不会因为多个会话选了同一个 Agent 就共享历史。

应用层 Runtime Manager 按以下键按需创建、复用同会话实例：

```text
chat:tenantID:agentID:conversationID:versionID:specDigest
training:tenantID:agentID:trainingID:operationID:specDigest
validation:tenantID:agentID:operationID:specDigest
```

不跨会话共用 WorkDir 或 MCP 子进程。训练操作结束即回收作者实例及写入进程，校验
实例结束即关闭。Runtime Manager 对创建中、空闲缓存及执行中的实例统一计数，并在
同一准入锁内预占容量；活跃执行不淘汰。Phase 1B 在 config.example.json 写明以下
`agent_runtime` 配置默认值，Phase 1C 的训练和校验接入同一配额：

| 配置 | 默认值 | 含义 |
| --- | --- | --- |
| max_instances | 32 | 全进程实例上限 |
| max_instances_per_tenant | 8 | 单租户实例上限 |
| validation_reserved_instances | 2 | 全局仅供校验的预留槽位 |
| validation_reserved_instances_per_tenant | 1 | 每租户额度中仅供校验的预留槽位 |
| idle_ttl_seconds | 900 | 空闲实例回收期限 |
| acquire_timeout_seconds | 5 | 获取容量的最长等待时间 |

总量及租户上限同时适用；非校验实例分别不得超过上限减预留值，空闲缓存也不能占用
预留槽。校验可使用普通空余槽及预留槽；等待按满足配额条件的同类请求 FIFO 唤醒，
不让单租户的超额请求阻塞其他租户，也不抢占执行中任务。
容量紧张先回收可释放的空闲实例，再有界等待，超时返回可重试的繁忙错误。上限、
期限必须为正整数，租户上限不得大于全局上限，预留值必须大于零且小于相应上限，
每租户预留值不得大于全局预留值。
该策略限制单租户占用并为校验留出容量，不承诺任意负载下无等待。不同 Agent 可以
同时训练。发布触发的重校验同样使用 validation 配额。

缓存目录不是持久会话记忆，重启或版本切换后可清理；重要产物必须由平台另行持久保存。
目录回收前确认没有进程使用。版本、训练 HEAD 和消息记录始终可从数据库及 Git 恢复。

### 5.3 平台临时目录与 Git 边界

candidate 根级 .tmp 由平台创建，写入临时文件允许，但目录本身不得替换为链接/特殊
文件。checkpoint 枚举只排除已确认的根级 .tmp 和平台 Git 管理入口，不依赖模型可改写
的 .gitignore，不忽略其他未跟踪文件。服务端按检查后的清单暂存，禁止直接 git add -A。
.tmp、scratch、state 不能出现在 Bundle commit 中，平台根级保留目录的符号链接也拒绝。
发布前重新校验临时目录身份和完整树。Agent 不可访问 bundle.git 或修改 worktree 的
Git 管理入口；平台负责 Git 操作，训练沙箱必须保护这些管理文件不被 shell 改写。

## 6. SDK 和运行权限

### 6.1 Phase 1A：通用文件写策略

```go
type WorkspaceWriteMode string
const (
    WorkspaceWriteRestricted WorkspaceWriteMode = "restricted"
    WorkspaceWriteAll WorkspaceWriteMode = "all"
)
type WorkspacePolicy struct {
    WriteMode WorkspaceWriteMode
    WritablePrefixes []string
}
```

pi.Options 接收 WorkspacePolicy，仍保留 WorkDir、AllowWrite、AllowExec。内部
规范化对象由底层公共包持有，避免 tools/sandbox 反向 import pi 形成循环。

- AllowWrite 控制 write/edit/apply_patch 注册，不决定 shell 可以修改哪些路径。
- restricted 的文件变更和 exec 只能写声明的相对目录；禁止绝对路径、父级分量及链接
  逃逸。可写子根使用受保护的 os.Root，拒绝写穿到同 Workspace 的只读文件。
- all + AllowWrite=true 保持 Coding 行为；业务训练还须在后续阶段保护平台 Git 管理路径。
- 启用 AllowWrite、AllowExec 或 stdio MCP 时模式必须显式给定；无进程只读使用可采用
  空可写集，不能为了探针而在权威目录创建 .tmp。
- chat 和校验设置 AllowWrite=false，exec 只写各自 WorkDir 的 scratch/.tmp。
- Seatbelt/Bubblewrap 必须在 OS 层实施同一规则；Host 无法保证 restricted 进程执行时
  fail closed，不能降级；MCP、Subagent 继承同一限制。
- 生产进程需要 .tmp 时它必须列入可写前缀，不能静默扩大配置。

### 6.2 Phase 1A：Workspace 检查

```go
type WorkspaceReport struct {
    AgentInstructionsDigest string
    Skills []skills.Summary
    Diagnostics []skills.Diagnostic
}
func InspectWorkspace(ctx context.Context, workDir string) (WorkspaceReport, error)
```

复用 PromptComposer 的 AGENTS 读取校验及 skills.Discover，和真实 ContextBuilder
共享加载结果；接口只读，不创建 Runner、临时目录或执行脚本。检查返回诊断，业务层
决定发布是否可接受，不在 SDK 内实现训练状态机或 Git 验证器。

### 6.3 业务权限

| 执行目的 | 可写内容 | 工具 |
| --- | --- | --- |
| chat | 本会话 scratch/.tmp | 版本固定的生产工具 |
| training | 当前候选行为文件与 .tmp | 文件作者工具、受控 exec 和无副作用业务工具 |
| validation | 本次校验 scratch/.tmp | 校验器及无副作用 smoke |

train_self 不是 Agent 永久能力；只有经过认证的训练管理员，在 active 状态且持本次
作者操作授权时派生。validating/ready/终态不启动可写作者实例。第一版不向模型提供
publish、模型配置编辑、评测或训练其他 Agent 的工具。

训练与 smoke 默认不允许外部副作用。优先只开放文件工具与受控脚本；业务工具只有
明确只读或已有安全替身时才加入，否则不注册。不建立新的完整 Effect Catalog 平台。
训练默认禁网策略在 Phase 1C 业务执行边界实现；Phase 1A 不扩展网络策略接口。

## 7. 训练、checkpoint 和校验

### 7.1 简单状态机

```text
active → validating → ready → published
validating → active                 校验失败或中断
ready → active                      继续修改前失效旧报告，或发布重检发现候选/引用异常
ready → validating                  发布时证据过期或校验器/校验策略变化
active / validating / ready → cancelled / stale / expired
```

| 起点 | 终点与条件 |
| --- | --- |
| active | validating：停止作者进程并固定候选，开始校验 |
| validating | ready：校验成功；active：失败或中断 |
| ready | active：继续编辑、恢复 checkpoint、更改候选配置，或发布重检发现候选/引用异常，先清空旧验证 |
| ready | validating：候选未变，但证据过期或校验器/校验策略变化，重新校验 |
| ready | published：管理员发布事务成功 |
| 任一非终态 | cancelled、stale、expired：停止本次操作后关闭并释放活跃训练指针 |
| 终态 | 无出边；需要继续时创建新训练会话 |

没有 evaluating/repairing，也没有自动修正循环。管理员根据校验反馈继续聊天修改即可。

### 7.2 开始和每轮聊天

创建训练在 Agent 锁及数据库事务内检查管理员、enabled 状态、当前版本和活跃训练指针；
创建 TrainingSession 及 type=training 的已有 Conversation，设置活跃指针，从基础版本
创建 candidate worktree 和持久 `refs/training/<trainingID>/head`。文件物化失败时将本次
会话取消并释放指针；重启可根据记录重新物化，不创建第二个匿名候选。
创建前若既有训练已到期，先按 §8.4 完成停止及终态清理；未完成清理时拒绝新建。

每轮执行：

1. 校验 tenant + agent + admin + conversation 归属、状态及操作 ID；ready 先转 active。
2. 将 operation_json 持久化为本次运行中状态，再启动绑定 candidate 的目标 Agent。
3. 复用现有消息和 invocation 存储保存输入、回复、工具输出及计量。
4. 关闭作者工具调度，停止 exec/MCP/Subagent 子进程，然后计算实际文件变化。
5. 执行 §7.3 的入库前检查子集：路径/UTF-8 路径编码、文件类型和链接边界、平台
   保留项、Secret/原生可执行文件拒绝规则，以及单文件/总量/路径数限制；排除项仅按
   §5.3 处理。通过且有变化则生成 Git checkpoint，并更新 candidate_head。此时不要求
   AGENTS/Skill 语义检查、模型/工具可用性或脚本 smoke 成功，允许保存待修复草稿。
6. 记录操作结果摘要并清除旧 validation_json，向页面返回回复和 diff 摘要。

模型能修改的文件不等于可发布文件；完整发布校验仍在下一步执行。模型不能操作 Git
仓库和平台文件；变更列表由平台枚举。失败 Turn 产生合法资产变更时保存 partial checkpoint，
不能直接作为发布证据，candidate_partial=true 时拒绝校验进入 ready；下一次成功 Turn
完成残留问题检查并清除标志后重新校验，恢复 checkpoint 则从对应元数据恢复此标志。
Turn 产生不合法文件时保留工作目录和失败诊断，candidate_partial=true，拒绝提交危险
文件；清理后才能继续 checkpoint，不把旧 HEAD 的成功验证拿来批准脏目录。

checkpoint commit 元数据只记录 trainingID、runID、actorID、时间和 partial 标志，不放
聊天正文或 Secret。每个 checkpoint 创建不可变 `refs/training/<trainingID>/checkpoints/<id>`，
防止 HEAD 恢复到旧点后历史被 GC。列表通过本 Session 的 refs 获取，不另建 checkpoint 表。
restore 只能选择本会话 checkpoint，必须在无正在执行操作的 active 状态进行；ready
先失效证据。取消/过期可删除工作目录，但持久 refs 和聊天历史保留供审计。

### 7.3 冻结与发布校验

所有操作通过单进程的 Agent 级准入互斥与数据库 CAS 设置持久操作槽；写入、restore、
配置修改、校验、发布互斥，操作未结束时新变更请求返回 409。互斥只在准入/完成事务
期间持有，不跨整个作者 Turn；执行期间由操作槽阻止第二个写入方。生产普通聊天只
在准入时检查 Agent 状态和版本，不因该 Agent 正在训练而阻塞，也不影响其他 Agent。
取消可先发出停止信号，但只有子进程停止后才能清除操作槽和活跃训练指针。

先禁止新作者工具调用，等待当前调用结束并终止所有可写子进程，保存 checkpoint，再
进入 validating。无法确认旧进程停止则校验失败且不开放新写入。校验固定 HEAD 和
candidate_config_json 的联合 spec_digest，在隔离只读副本中运行，不让作者边改边测。

服务端检查：

- 实际文件树与记录 HEAD 一致，排除 §5.3 确认的平台临时区后不得有遗漏的脏文件。
- AGENTS.md 是非空普通 UTF-8 文本；Skills 调用 InspectWorkspace，诊断逐条反馈。
- 拒绝越界/绝对链接、特殊文件、嵌套仓库、submodule、平台配置、Secret 和原生可执行文件。
- 单文件最多 1 MiB，Bundle 最多 32 MiB/2,000 路径，SKILL.md 最多 256 KiB。
- 脚本仅 skills/<id>/scripts 下被 Skill 引用的 .sh/.py 文本，使用已批准解释器和依赖；
  不允许运行时安装。smoke 使用固定的受限环境、超时和无网络权限，不执行真实业务写入。
- 校验模型/工具/SecretRef 可用性，记录具体配置和版本；Secret 内容不得出现在日志或报告。
- 文件及配置都没变化时拒绝发布；纯模型配置变化可复用 Git 文件树，但仍生成新完整版本。

解释器及依赖允许清单由宿主 `agent_training.approved_interpreters` 配置，默认 `[]`；
初始版本创建和训练校验共用，Phase 1B 在 config.example.json 明示。每项固定扩展名、解释器绝对路径、版本/内容
摘要和预装依赖清单；候选文件及管理员候选配置不能扩张该清单。无脚本时无需配置，
包含脚本却无匹配项时校验失败，不通过 PATH 自动寻找解释器。smoke 的每脚本超时由
`agent_training.smoke_timeout_seconds` 配置，默认 30，必须为正整数。
上述 smoke 配置同在 Phase 1B 写入 config.example.json，Phase 1C 复用。

结果写入训练表 validation_json，包含 head、digest_version、bundle_digest、spec_digest、
validator_version、validation_policy_digest、结果、诊断、smoke 摘要、validated_at 和
valid_until。validator_version 固定校验器实现修订；validation_policy_digest 使用 §4.2
同版 JCS/SHA-256 摘要协议，覆盖有效检查限制、解释器/依赖清单、smoke 环境及沙箱
策略、超时和证据 TTL；运行环境引用须可核验，变化视为策略变化。
`agent_training.validation_ttl_seconds` 默认 1800（30 分钟），必须为正整数，在
Phase 1B 写入 config.example.json，Phase 1C 复用。valid_until 为数据库 UTC 校验完成
时间加该 TTL，训练校验再与训练 expires_at 取较早值；初始版本创建使用相同摘要和
证据规则，但不受训练期限约束。
校验完成时 CAS 比较当前候选仍相同且训练未到期，
成功进入 ready。此期限只控制发布前证据复用，不使已发布版本自动失效。
继续编辑或更改候选配置必须先失效该报告。完整验证与展示字段编辑不混淆。

## 8. 发布、回滚、重试与恢复

### 8.1 人工发布

发布请求必须由管理员发起，并明确确认“本版本仅完成结构及脚本校验，未经自动质量
评测”。Agent 不可自我声明发布成功。发布流程：

1. 获取本 Agent 的操作锁，校验管理员及 ready 状态；已 published 则返回 result_version_id。
2. 重读当前 active_version_id；与 base_version_id 不同则 stale，拒绝静默合并。
3. 按下述规则重检真实树、配置、外部引用及验证证据，决定复用、重校验或拒绝发布。
4. 服务端分配 version 和 version ID，准备 Git commit/tag 和只读 versions 物化目录。
5. 一个数据库事务写入 agent_versions，CAS 切换 active_version_id，设置训练 published
   和 result_version_id，清空 active_training_session_id。
6. 事务成功后返回结果；缓存失效、页面推送失败不撤销已发布版本。

第 3 步及第 5 步的判定规则：

- 每次发布均确认作者进程已停止、真实目录与 candidate_head 一致（排除项仅按 §5.3）、
  candidate_partial=false，并按 §4.2 重算摘要，检查模型/工具/SecretRef 仍可用。
  发现脏树、候选 HEAD/摘要与报告不符或引用不可用时，清空旧证据并回到 active，
  返回具体诊断；须完成候选修复/checkpoint 和显式校验后再发布，不自动收编脏文件。
- 仅当成功报告绑定的 head、digest_version、bundle_digest、spec_digest 均匹配，
  validator_version 和 validation_policy_digest 与当前实现/策略一致，且数据库 UTC
  当前时间严格早于 valid_until 和训练 expires_at 时，才可复用。缺少字段或候选不
  匹配时清空证据、回 active 并要求显式校验；未知摘要协议拒绝发布并报告不兼容，
  不自动转换摘要或绕过验证。
- 候选和摘要协议匹配，仅证据过期或校验器/校验策略变化时，在同一持久发布操作槽内
  将 ready 转 validating，按 §7.3 完整重校验；成功转 ready 后继续发布，失败回 active。
  在转 validating 前先获取校验容量，获取失败则保留 ready、禁止本次发布并返回繁忙；
  完成操作槽后允许重试。
  训练会话本身到期则走 §8.4，不以重校验延长训练期限。
- 第 5 步事务中再次检查候选 CAS、生产基线、报告绑定及上述两个期限，策略在本次
  操作内固定；准备文件期间证据到期则拒绝本次发布，按操作记录清理未引用产物，
  下次发布重新校验。基线变化转 stale，训练到期按 §8.4 处理。

版本表对 source_training_session_id 建唯一约束；并发或重复 publish 只能产生一个版本。
Git 和 DB 不是同一事务：操作记录在写 Git 前保存目标 version ID、tag 和摘要，失败可
按记录补偿本次新建且未引用的资源，不能删除历史版本或复用的 commit。

Agent 初始创建使用同样的“文件就绪后写版本并切换指针”顺序。agents.id 为每次创建的
服务端 ID；初始创建/恢复用已有行定位，bootstrap_key 保证内置种子幂等。未完成创建
的 Agent 不可被聊天选择；管理页面可重试完成或归档，不伪装成已有生产版本。

### 8.2 会话更新与回滚

不建立 Outbox，也不批量写 pending version。每个 follow_latest=true 的普通会话在
下一轮准入时直接读取 Agent.active_version_id，先物化新目录并取得新 Runtime，再 CAS
更新会话的 agent_version_id。加载失败保留原版本并返回错误，下次可重试。

已开始的 Turn 使用已固定版本跑完；follow_latest=false 的会话保持原版本。版本更新
通过本轮 Context 简短告知，保留用户可见聊天历史。训练始终固定自己的 base/candidate。
正确性依赖数据库指针，不依赖实时通知成功送达。

回滚通过管理员 activate 某个历史 AgentVersion 实现；先校验文件摘要、模型/工具及
SecretRef 可用性，再切 active_version_id。文件、模型和工具配置一起切换，不修改历史
行，也不承诺撤销工具曾产生的外部业务写入。活跃训练在下次操作发现基线变化后进入 stale。

### 8.3 有界幂等与断线恢复

已有训练会话的写操作携带 request_id（训练 Run 直接使用 run_id）。训练表 operation_json 保存当前
或最近完成操作的 ID、请求摘要和结果摘要；同 ID 同请求返回当前状态或已有结果，
同 ID 不同内容返回 409。历史 Run ID 再次出现时查询该训练 Conversation 的既有消息/
invocation，返回历史状态或明确不能重放的 409，绝不自动再执行。历史 restore/配置
请求携带 expected_row_version，过期版本一律冲突，不因操作槽被覆盖而重复修改。

执行前持久化操作记录和用户输入，完成后持久化结果；中断且无法确认执行结果时标为
interrupted，不猜测成功或自动重跑。用户重试执行须用新 ID，仍受候选状态检查。
发布以 source_training_session_id/result_version_id 为永久去重依据，独立于操作槽。
创建训练请求携带当前 Agent.row_version，并在事务中校验；重复的旧版本请求返回冲突
及有权查看的活跃训练 ID，客户端查询既有会话，不重新创建。创建 Agent 的普通请求不
承诺通用幂等 API，前端防重复提交；内置 bootstrap 则由 bootstrap_key 永久去重。

SSE 仅用于当前连接展示，不新增事件表或 Last-Event-ID 完整回放承诺。断线后前端重新
获取训练会话（含当前/最近操作）、消息和 diff，正在运行则轮询状态；POST /runs 不因
重连自动重发。查询和重连始终检查 tenant + admin + training + conversation 归属，
不能仅凭 run_id 读取其他人的训练内容。未持久化的进度动画允许丢失，最终消息和操作
结果不应丢失。

### 8.4 重启、备份与单进程限制

操作记录用于恢复，不用 TTL 过期冒充已停止进程。恢复前必须确认旧实例及其子进程组
不再持有 Candidate 写权限，不能确认则拒绝恢复并报告；第一版不支持两个服务进程
同时管理同一数据目录。部署必须先停旧写进程再启动接管实例。

从持久 CandidateHead 重建 worktree；未 checkpoint 改动不在恢复保证内，可保留旧目录
供诊断。中断的作者/校验回到 active 并清空验证，已 published 不回退。发布中断先对账：
DB 已有来源版本则补齐 Session 结果；DB 未提交则清理本次未引用产物后重新校验，不能
直接假定成功。过期/取消在停止进程后释放活跃指针。

过期采用惰性检查，不新增后台扫描任务：训练详情、写操作/发布准入、创建新训练、
归档检查及启动恢复均检查数据库 UTC 时间是否 `>= expires_at`。已经到期的训练
禁止新操作；有执行中操作时发停止信号，保持操作槽、非终态及活跃指针，确认全部
子进程停止后才在事务内转 expired、清空验证并释放指针。不能确认停止时返回具体
阻塞原因，继续拒绝新操作。每个运行操作还须绑定剩余训练期限作为 deadline，到期
触发同一停止流程；完成事务再次检查期限，不能把跨过期限的结果写成 ready/published。
无访问且无运行操作的过期会话可暂存原状态，下一次上述入口负责清理。终态不因
expires_at 改写；已提交发布的重试/恢复始终先返回或对账既有发布结果。

上线需有异机备份：同批数据库一致性快照 + bundle.git 对象及全部 refs/tags + 已持久
附件；密钥由宿主系统单独保存。第一版以协调停写窗口取得备份，记录恢复清单，迁移前
额外备份；频率和保留期由部署明确配置。恢复演练必须执行 git fsck、版本/HEAD 引用及
摘要核对，再重建目录；缺失对象阻止对应 Agent 激活，不回退读取模板。临时缓存不必备份。

## 9. API 与页面

### 9.1 Agent 与版本

```text
GET    /api/v1/agents
GET    /api/v1/agents/:agentID
POST   /api/v1/agents
PATCH  /api/v1/agents/:agentID
GET    /api/v1/agents/:agentID/versions
POST   /api/v1/agents/:agentID/versions/:versionID/activate
GET    /api/v1/agent-templates
```

普通用户只查询本租户可用 Agent；管理操作和模板查询要求管理员。GET /agents 返回
items、next_cursor 和计算出的 default_agent_id；条目包含展示配置及 selectable。
GET /agents/:agentID/versions 接受 `cursor`、`limit`（默认 20，上限 100，必须为正整数），
返回 items、next_cursor，按 version 降序使用键集分页。cursor 为绑定 tenant/agent
及上一页末尾 version 的服务端校验令牌；非法或跨归属游标拒绝，翻页仍独立鉴权。
创建从 Template 初始化行为资产及展示字段，模型/工具选择只能引用服务端允许的配置。

PATCH 允许 name、description、presentation、status，拒绝未知字段和空名称，至少修改
一个字段。status 仅 enabled/archived，不改变 AGENTS.md 或版本配置。归档拒绝任何非
终态训练；归档、新训练和生产 Run 准入串行检查状态。归档后不接受新执行，已准入的
生产 Turn 可完成；历史可读。重新启用不会更换版本，可用版本不存在时仍不可聊天。

### 9.2 训练

```text
POST   /api/v1/agents/:agentID/training-sessions
GET    /api/v1/agents/:agentID/training-sessions
GET    /api/v1/training-sessions/:trainingID
POST   /api/v1/training-sessions/:trainingID/runs
GET    /api/v1/training-sessions/:trainingID/diff
GET    /api/v1/training-sessions/:trainingID/checkpoints
POST   /api/v1/training-sessions/:trainingID/checkpoints/:checkpointID/restore
PATCH  /api/v1/training-sessions/:trainingID/config
POST   /api/v1/training-sessions/:trainingID/validate
POST   /api/v1/training-sessions/:trainingID/publish
POST   /api/v1/training-sessions/:trainingID/cancel
```

GET 训练详情返回 conversation_id、status、基线、候选摘要、最近验证及当前/最近操作。
消息复用已有 Conversation 查询，必须增加 training 类型鉴权，不能走普通匿名 Chat
路径读写训练会话。普通 Chat 的创建/运行/列表/删除均排除 training；训练消息只由
Training Service 驱动。config 仅修改服务端允许的模型参数，不接受客户端权限扩张。

没有 submit/evaluate/fine-tune/model-registry/default-setting 等接口。验证和发布由
管理员按钮触发，避免模型发起检查后继续写盘引入额外的排队和交接状态。

### 9.3 保留聊天页面，新增管理员工作台

- 当前卡片继续选择助手，数据源改为本租户 Agent；创建和筛选参数由 profile_code 改为 agent_id。
- 图标、欢迎语、推荐问题和排序从 Agent 展示字段读取，不随模板后续修改联动。
- 分页需有加载更多/搜索；默认项不在首屏则查询详情。Conversation VO 包含 Agent 名称、
  图标和状态，历史展示不依赖当前可选列表是否包含该 Agent。
- 无 Agent、无可用版本是正常空状态，默认项为 null；请求失败才显示加载错误。
  创建前 Agent 被停用时刷新选择器，不静默换 Agent 发送用户原消息。
- 保留图片、消息流、历史搜索、重命名和普通会话删除；一个 Conversation 固定 Agent 身份。
- 管理员新增 Agent 列表/创建/详情和训练入口，训练工作台包含聊天、文件 diff、checkpoint、
  校验结果及“校验、继续训练、恢复、发布、取消”。没有评测分数和微调入口。
- 管理入口仅在服务端启用且有权限时显示；后端独立鉴权。环境/客户信息由服务端提供。

## 10. 迁移与代码分工

### 10.1 数据迁移

第一版只新增三张表，不预留旧完整版的业务表或指向未来模块的外键。
已有 agent_conversations 增加 tenant_id、conversation_type、agent_id、agent_version_id、
follow_latest；训练表通过 conversation_id 关联它，不再反向增加 training_session_id。
现有消息表仍引用 Conversation 内部主键，不复制消息到训练专用表。

迁移顺序：

1. 0007 创建 agents/agent_versions，给 Conversation 增加 nullable 归属列，类型默认 chat。
   agents.active_training_session_id 暂为 nullable，无目标表 FK。
2. 兼容代码支持旧 Profile 与新 Agent ID，bootstrap 为明确租户创建八个内置 Agent 及
   初始完整版本，复制被引用的共享 Skills；验证失败停止。对 bootstrap_key 幂等重试。
3. 回填已有会话 tenant/agent/version，核对空值、未知 Profile、跨租户/Agent 引用和
   Git 摘要。已确认单客户的旧匿名数据回填配置租户；旧 schema 没有租户列不等于能
   证明部署只服务过一个客户。若存量归属不明或历史上混用多个客户，必须提供经宿主
   确认的逐会话租户映射；缺失或冲突则停止迁移，不按匿名 Cookie 猜测组织归属。
4. 0008 在停写窗口将归属字段改 NOT NULL，补 FK/索引；先建
   uq_agent_conversations_tenant_owner(tenant_id,user_id,conversation_id)，再删旧
   uq_agent_conversations_owner(user_id,conversation_id)。
5. 前端只发送 agent_id；兼容期旧 code 只映射本租户内置种子，单请求不能同时传两种身份。
   确认旧客户端退役后 0009 删除 profile_code 及旧索引。
6. Phase 1C 的 0010 只创建 agent_training_sessions，并补活跃训练指针 FK；不创建其他表。

唯一约束：agents(tenant_id,id)、agents(tenant_id,bootstrap_key)、
agent_versions(agent_id,version)、agent_versions(tenant_id,agent_id,id)、
agent_versions(source_training_session_id)、agent_training_sessions(conversation_id)。
0010 同时在 agent_training_sessions 增加生成列 active_slot：status 为
active/validating/ready 时值为 1，终态时为 NULL；status 约束为 §7.1 的合法枚举，
并建 UNIQUE(tenant_id,agent_id,active_slot)，利用 MySQL 允许多个 NULL 保留终态历史。
这是“同 Agent 最多一条非终态训练”的数据库兜底；不在 agents 上增加包含 Session ID
的冗余唯一键。训练表另建 UNIQUE(tenant_id,agent_id,id)，供 agents 的
(tenant_id,id,active_training_session_id) 复合外键指向同租户、同 Agent 的训练行。
生成列唯一键不保证指针与状态一致：创建/终态转换/恢复仍须在 Agent 锁及事务中同时
维护状态、操作槽和指针，清空指针须 CAS 匹配该 Session ID；恢复发现不一致时拒绝
新训练，确认进程停止后完成对账。过期时间本身不参与生成列计算，停止完成前不释放槽位。
来源训练列在 0007 先 nullable，0010 再补 FK；初始版本来源 NULL，可重复。
每个 Repository 以认证 TenantID 查询，关联 ID 必须属于同租户同 Agent；CAS 与应用
一致性检查配合复合外键，禁止先按裸 ID 查询再补租户判断。

0008 down 先检查旧 (user_id,conversation_id) 是否因跨租户重复而冲突，有冲突拒绝回滚，
不删客户数据；无冲突则先恢复旧唯一索引，再删除新索引，最后撤销本次 FK/NOT NULL。
MySQL DDL 不假定可事务回滚，迁移必须支持按实际 schema 断点继续。Git 资产不由 down
脚本自动删除。

### 10.2 文件职责

| 模块 | 第一版职责 |
| --- | --- |
| pi/workspace_policy.go、pi/internal/workspacepolicy/ | 公共策略及内部规范化 |
| pi/harness/tools/filesystem.go | 所有文件变更的统一子根边界 |
| pi/harness/sandbox/、pi/mcp/sandbox.go | exec/MCP 的 OS 写策略 |
| pi/harness/inspect.go、context.go、prompt.go | 检查与真实加载复用 |
| config/、cmd/server/app.go、cmd/cli/main.go | 显式策略配置及兼容入口 |
| domain/entity/agent/、domain/repository/agent/ | Agent、AgentVersion 及存储契约 |
| domain/entity/agenttraining/、domain/repository/agenttraining/ | 训练状态及单操作记录 |
| application/identity/、infrastructure/middleware/ | 宿主认证、租户和管理员守卫 |
| application/service/agentcatalog/ | 创建、展示、归档、模板投影 |
| application/service/agentruntime/ | 版本解析、会话 WorkDir、实例缓存 |
| application/service/agenttraining/ | 创建、聊天、checkpoint、restore、validate、恢复 |
| application/service/agentversion/ | 发布、激活、回滚及 Git/DB 补偿 |
| application/port/agentbundle/、infrastructure/driver/agentbundle/ | Git、物化、快照和真实树检查 |
| infrastructure/persistence/agent/、agenttraining/ | 三张表及事务实现 |
| infrastructure/controller/http/agent/、agenttraining/ | API 绑定和响应，不实现业务状态机 |
| frontend/templates/pages/、frontend/static/js/pages/ | 当前聊天适配、管理员列表/详情/训练页面 |

用例按 create/run/checkpoint/validate/publish/activate/recover 分文件，不把所有代码塞进
chat/service.go 或 server/app.go；也不为每个 JSON 字段新建服务和表。具体文件及 TDD
步骤放各阶段计划，本文不预生成后续代码骨架。

## 11. 实施阶段和验收

### 11.1 分四个小阶段

1. Phase 1A：WorkspacePolicy、OS 写边界、MCP 继承与 InspectWorkspace。现有
   `2026-09-08-agent-workspace-policy-phase-1a.md` 计划继续适用，需按本版引用评审。
2. Phase 1B：agents/agent_versions、模板迁移、管理员身份接入、会话 Runtime 和聊天页适配。
3. Phase 1C：第三张训练表、Candidate/Git checkpoint、校验、人工发布、回滚及恢复。
4. Phase 1D：管理员完整工作台、迁移与备份演练、端到端验收。

每阶段单独编写和评审实施计划，完成本阶段验收再进入下一阶段。本轮只修设计，
Phase 1A 计划仍待评审，不开始实现。自动评测、微调和分布式部署未来按真实需求另立
设计，不作为第一版验收条件。

### 11.2 Phase 1A 验收

- restricted + AllowWrite=false 不注册文件写工具；可写模式不由 AllowExec 隐式决定。
- Seatbelt/Bubblewrap 实测 exec 不能修改/删除/改名行为文件，能写显式允许的临时目录。
- Host 无法保证受限进程时启动失败，stdio-only 也不能绕过；只读无进程不创建临时文件。
- all + AllowWrite=true 保持 CLI Coding 行为。
- InspectWorkspace 与 ContextBuilder 对同一 AGENTS/Skills 读取和诊断一致。
- symlink、路径逃逸、特殊文件以及可写前缀兄弟目录被拒绝。
- MCP/Subagent 不放大父策略；两个原生 OS 的实测证据与纯参数测试分开记录。

### 11.3 业务与端到端验收

- 同租户不同 Agent 可同时训练；同 Agent 第二个活跃训练被拒；跨租户/管理员引用被拒。
- 0010 的生成列唯一键拒绝同 Agent 两条非终态记录，允许多条终态记录；跨租户/Agent
  的活跃指针被复合外键拒绝，恢复不能绕过状态与指针一致性检查。
- 两个聊天会话即使使用同一个版本也不能互读临时文件；版本目录没有运行产物。
- 训练失败保存可恢复的 partial checkpoint；恢复旧 checkpoint 不丢失后续历史 refs。
- 校验期间无写入进程；修改候选或配置使旧证据失效，发布检查实际 HEAD 和联合摘要。
- 摘要固定测试向量覆盖键序、默认值、null、数组顺序、文件 mode/内容/链接目标及
  未知字段拒绝；校验、发布和恢复得到同一摘要，旧协议不会被新默认值悄然重算。
- 证据到期或校验策略变化触发重校验，脏树及绑定不符拒绝发布；训练到期停止进程后
  才释放活跃槽位，运行中/发布准备中跨过期限也不能提交成功状态。
- 单租户占满普通配额不占用其他租户额度或校验预留槽；空闲实例可回收、活跃实例不
  被抢占，等待超时返回繁忙；版本分页在新增版本时不重复返回已翻过的版本。
- 重复请求不重复执行，重复发布只产生一个版本；Git/DB 各故障点能对账或补偿。
- 新会话用新版本，已有 follow_latest 会话下一轮迁移，加载失败保留旧版本；不依赖事件投递。
- 回滚恢复文件、模型和工具配置；不可用历史引用拒绝激活。
- 归档拒绝活跃训练，阻止新执行但保留历史；页面正常处理空租户、归档及分页默认项。
- 普通 Chat API 不能读写训练会话；断线恢复获取快照不重发执行。
- 校验页面明确“未自动质量评测”，发布要求人工确认；没有自动分数或微调入口。
- bootstrap 可重试、同模板可创建多个 Agent，0008 回滚冲突不删数据。
- 从同批 DB/Git 备份恢复后验证所有版本和 checkpoint 引用，缺失对象阻止激活。

完整业务用例：创建客服 Agent → 管理员训练新增 Skill → 展示 diff/checkpoint → 校验
→ 人工发布 v2 → 普通聊天下一轮使用 v2 → 回滚 v1。销售 Agent 和其他客户全过程不受影响。

## 12. 简化后的边界

三张表分别回答“是谁”“哪个完整版本”“这一次训练进行到哪里”。文件历史由 Git
负责，聊天和执行计量由现有存储负责。减少的是独立模块、状态机和数据流水线；保留
真实写入边界、租户归属检查、发布证据和故障恢复所需的最小记录。
