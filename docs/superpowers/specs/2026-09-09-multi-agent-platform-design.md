# 多 Agent 管理、聊天与训练平台详细方案

状态：2026-09-09 用户已明确要求“按照这个思路开发吧”，据此开始实施；本文代码草案仍需通过实际集成与验收。

技术主规格：[Agent 资产自训练与人工发布设计](2026-09-04-agent-self-training-design.md)。
本文细化产品流程，数据库、SDK 权限、摘要、训练状态机、迁移和恢复必须与主规格共同
落实。对应关系和新增待审内容见 §15；不能只完成页面就认定完整功能交付。

## 1. 产品定义

系统管理多个独立的 Agent。每个 Agent 都有自己的名称、含义/用途、行为规则、能力、
模型配置和训练历史。用户通过名称和用途理解它，选择后进入该 Agent 的聊天页面。

例如，“文案助手”用于内容创作，“资料分析”用于整理和分析资料；这些只是命名示例，
用户可按自己的业务自由命名，不限定为岗位或拟人身份。同一用途也可以创建多个 Agent。

核心关系：一个客户组织拥有多个 Agent；一个 Agent 有多个正式版本、多段训练历史，
并能服务多个独立聊天会话。Agent 是长期存在的对象，一次聊天是它的一段会话。

普通用户主流程：登录 → 浏览本组织 Agent → 选择一个 → 聊天 → 继续或管理自己的历史会话。

管理员主流程：创建 Agent → 确定名称和用途 → 训练调整行为 → 检查修改 → 校验 → 人工发布。

## 2. Agent 的组成

| 内容 | 作用 | 修改方式 |
| --- | --- | --- |
| Agent ID | 稳定身份，连接聊天、版本和训练历史 | 系统生成，不随名称变化 |
| 名称 | 列表、聊天标题、历史记录中的显示名称 | 管理员编辑 |
| 含义/用途说明 | 说明这个 Agent 做什么，帮助用户选择 | 管理员编辑 |
| 图标、欢迎语、推荐问题 | 聊天入口和初次交互的展示信息 | 管理员编辑 |
| 行为规则 AGENTS.md | 实际职责、回答原则、工作边界 | 在训练候选中修改，发布后生效 |
| Skills、脚本、参考资料 | 工作步骤及可使用的领域资产 | 在训练候选中修改，发布后生效 |
| 模型和工具配置 | 固定正式运行所用模型、参数和工具权限 | 通过完整版本发布生效 |
| 正式版本、训练记录 | 追踪当前行为来源，支持历史查询和回滚 | 系统记录，历史版本不可改写 |

名称与用途说明是展示信息，不能仅凭改名就声称能力改变。涉及实际行为的用途调整必须
落实到行为规则/Skills 并发布。创建时从选定模板初始化规则；未选择专业模板时使用通用模板。

`workspaces/chat/AGENTS.md` 是通用基础模板，现有 Profiles 是可复用的初始模板来源。
创建后形成独立资产，训练时不会修改公共模板。修改公共模板也不会自动影响已有 Agent。

## 3. 客户组织、用户与权限

| 身份 | 权限 |
| --- | --- |
| 普通用户 | 查看本组织可用 Agent，使用它们聊天，查询、重命名和删除自己的普通聊天历史 |
| 管理员 | 普通用户权限，以及本组织 Agent 的创建、资料编辑、训练、校验、发布、换模型、回滚和归档 |

组织之间的数据不互通。同组织不同用户的聊天内容、附件引用授权和临时文件也不共享。
同一管理员可以训练多个不同 Agent，但同一 Agent 同时只能有一个未结束训练会话。
第一版每段训练仅由创建它的管理员操作和读取，不做多人共同编辑或训练所有权转移。

登录接入尚待审核确认。建议复用客户已有业务系统登录，由可信宿主提供组织、用户及
角色；没有现成登录系统时，需确认是否把账号密码登录纳入本期。当前匿名 Cookie 不能
充当组织登录或管理员身份。固定组织匿名访问仅在部署明确选择时提供普通聊天能力。

没有可信管理员认证时不开放管理页面和管理写接口。后端独立鉴权，不能只靠前端隐藏按钮。
用户提交 Agent ID、会话 ID 或训练 ID 时，每次都重新检查归属；客户端不能自报组织或角色。

## 4. 页面设计与导航

| 页面 | 路径 | 核心内容 |
| --- | --- | --- |
| Agent 列表 | `/agents` | 搜索、卡片、用途说明、开始聊天、分页 |
| 聊天 | `/chat` | 当前 Agent、消息流、图片输入、历史会话、新聊天、切换 Agent |
| Agent 管理 | `/admin/agents` | 全部状态、创建、搜索、管理入口 |
| 创建 Agent | `/admin/agents/new` | 名称、用途、模板、展示信息、允许的初始模型/工具 |
| Agent 详情 | `/admin/agents/:agentID` | 资料、正式版本、模型、训练记录、版本历史和管理操作 |
| 训练工作台 | `/admin/training-sessions/:trainingID` | 训练聊天、文件差异、checkpoint、校验结果、发布操作 |

### 4.1 Agent 列表

客户登录后的默认入口是 Agent 列表。顶部显示当前组织及搜索；主体卡片显示图标、名称、
用途说明和“开始聊天”。只列出本组织已启用且有可用正式版本的 Agent。支持按名称/用途
搜索和分页加载。推荐项只作展示，不自动替用户选择；只有一个 Agent 时也保留选择动作。

没有可用 Agent 是正常空状态。管理员看到创建入口，普通用户看到联系管理员的提示。
加载失败单独显示错误和重试，不能伪装成空列表。管理员还能进入独立的管理列表查看归档
或创建未完成的对象，普通用户列表不承担管理功能。

### 4.2 选择后进入聊天

点击卡片进入 `/chat?agent_id=<id>`，显示名称、用途、欢迎语和推荐问题，不创建空历史。
首次发送时才创建会话并绑定所选 Agent。名字可修改，绑定使用稳定 Agent ID。

页面继续使用现有消息输入、图片输入、流式回复、停止生成等能力。左侧为本用户的历史
会话，条目能看出属于哪个 Agent；主区展示当前消息，底部输入区始终对应当前 Agent。

首次创建或发送失败时保留未发送内容，并显示错误。不更换默认 Agent，不自动重发请求。
进入聊天后目标被停用时，阻止发送并提供返回列表入口。

### 4.3 历史、新聊天与切换

历史地址 `/chat?conversation_id=<id>` 根据经过组织/用户鉴权的会话恢复原 Agent 与消息。
同时传入不同的 agent_id 时拒绝冲突参数。刷新或直达历史不依赖当前列表是否已加载到该 Agent。

“新聊天”回到当前 Agent 的新聊天状态，首条消息才创建新会话。“切换 Agent”返回列表，
选中另一 Agent 后进入它的新聊天状态，或由用户明确选择它已有的历史。原消息、附件和
临时文件不迁移。没有目标参数的 `/chat` 返回列表。

保留历史搜索、重命名和普通会话删除。归档 Agent 的历史可读，不能再提交新消息。

## 5. 创建与日常管理

管理员创建 Agent 时填写名称和用途说明，选择初始模板、图标/欢迎语/推荐问题，并从
服务端允许的配置中选择模型和工具。名称不承担唯一身份校验，内部关系始终使用 Agent ID。
同一模板可以创建多个独立 Agent。

服务端完成模板资产组合、配置校验、初始 Git 快照和版本记录后，建立 v1 并指向它。
初始化失败的 Agent 不对普通用户开放，详情页展示原因以及已实现的恢复或归档操作。
重复 bootstrap 不创建重复内置 Agent；普通创建请求由前端防重复提交，失败后先查询确认。

名称、用途说明、图标和欢迎语可以直接编辑，但这类展示修改不修改已发布行为配置。
实际规则、模型或工具策略必须通过版本流程变更。Agent 详情页明确区分“展示信息”和
“已发布行为”，避免管理员误以为改简介等于训练完成。

归档停止新的聊天和训练准入，已开始的普通聊天轮次可结束，历史保留。有非终态训练时
拒绝归档；先取消并确认进程停止。重新启用不会替换当前正式版本。

## 6. 多 Agent 独立训练

每次训练以某个 Agent 当前正式版本为基线，在独立候选目录中进行。它有独立的训练
会话 ID、管理员身份、聊天记录、候选文件、配置副本和校验结果。

管理员可分别训练 Agent A 和 Agent B，两者的规则、文件、模型配置和训练历史独立。
同一 Agent 第二个未结束训练请求被拒绝，防止两个候选同时争用正式发布入口。

工作台左侧/主区是训练聊天，旁边是实际文件差异、checkpoint 历史和校验结果。管理员
通过多轮对话调整回答风格、工作原则、Skills、脚本和资料，也可选择允许的候选模型参数。
模型本身不获得发布、换模型或操作其他 Agent 的工具。

每轮执行前持久保存输入和操作身份；执行后停止写进程，再枚举真实变更并建立 Git
checkpoint。历史节点可查看和恢复。失败轮次的合法修改可以作为 partial 草稿保留；
危险文件不进入 Git，partial 候选不能成为可发布证据。

训练只改变候选，客户聊天持续使用正式版本。训练进程只能访问自己的候选资产和
临时区，不能修改 Git 管理入口、其他 Agent、正式版本或服务端配置。脚本默认禁网，
不执行真实业务写入，不允许运行时安装依赖。

## 7. 校验、人工发布与版本生效

管理员点击校验时，先停止所有候选写入进程，固定文件 HEAD 和配置摘要，再在隔离
只读副本中检查。检查项包括入口文件、Skills、路径/链接、文件类型和大小、模型/工具/
SecretRef 可用性，以及允许脚本在受限环境中的 smoke 测试。

报告包含检查项、诊断、脚本结果、HEAD、摘要、校验器/策略版本及有效期限。它证明
结构和受限脚本检查完成，不代表回答质量达标。修改、恢复 checkpoint 或改配置都会使旧报告失效。

发布前展示实际差异和配置变化，由管理员明确确认“本版本仅完成结构及脚本校验，未经
自动质量评测”。服务端重新核对候选与证据、生产基线和外部引用。候选异常时拒绝发布；
仅证据过期或校验策略更新时，对同一候选重新校验。不能只凭页面上的“校验成功”放行。

发布创建新的完整不可变版本，例如 v2，并在一个数据库事务中切换正式指针。文件、
模型和工具配置一起固定；旧版本不被覆盖。重复训练发布返回既有版本，不再生成 v3。

新聊天使用当前正式版本。普通历史聊天默认下一轮跟随新版本；已开始的轮次用开始时
固定的版本完成。加载失败时保留原绑定并报错，不静默替换 Agent。聊天历史仍保留。

## 8. 快捷换模型与回滚

详情页提供“更换模型”：从当前正式版本复制完整配置，只替换允许的模型字段，保留
文件树、工具策略和运行限制，完整校验后创建新版本并切换指针。不要求先创建训练聊天，
仍需人工确认、变更说明和并发检查；旧版本完全保留。

回滚通过选择历史完整版本并重新验证引用可用性来切换正式指针，同时恢复文件、模型
和工具配置。回滚不承诺撤销工具已造成的外部业务写入。模型或凭证引用已失效时明确拒绝。

发布、换模型或回滚改变正式基线后，旧训练在下一次操作发现基线变化即转为 stale，
不能覆盖较新的正式版本。运行中的训练操作与这些管理变更互斥；普通聊天不等待整个训练过程。

## 9. 数据模型与资产保存

仅新增三张业务表。`agents` 保存组织、稳定 ID、名称、用途说明、展示资料、状态、
当前正式版本、活跃训练指针和 CAS 版本；`agent_versions` 保存不可变的文件引用/摘要、
模型/工具/运行配置、验证证据、来源及发布者；`agent_training_sessions` 保存管理员、
基线、候选、状态、期限、当前操作和发布结果。

聊天和模型调用记录复用已有存储，补组织、Agent、版本及会话类型归属。训练消息必须
单独做管理员归属检查，不能从普通聊天 API 绕过。数据库生成列唯一键保证同 Agent 最多
一条非终态训练，指针与状态仍在事务中共同维护。

文件按组织 → Agent → 正式版本/训练会话分开保存。Git 保存行为资产和 checkpoint；
正式版本目录只读，每次聊天有独立运行副本及 scratch/.tmp。候选、正式文件和聊天产物
不共用可写目录或硬链接。密钥只保存服务端引用，不进入版本 JSON、Git、日志或校验报告。

bundle_digest 采用完整文件清单的 SHA-256；spec_digest 采用文件摘要与有效运行配置的
规范化 JSON 联合 SHA-256，协议固定 digest_version。键排序、数组顺序、默认值、null、
文件 mode/内容/链接目标和未知字段处理按技术规格统一，不在各接口自行实现一套。

## 10. 接口范围

| 类别 | 接口 |
| --- | --- |
| Agent 查询 | `GET /api/v1/agents`、`GET /api/v1/agents/:agentID` |
| Agent 管理 | `POST /api/v1/agents`、`PATCH /api/v1/agents/:agentID` |
| 允许的初始化选项 | `GET /api/v1/agent-templates`、`GET /api/v1/agent-model-options` |
| 版本历史 | `GET /api/v1/agents/:agentID/versions`，游标分页 |
| 历史激活/回滚 | `POST /api/v1/agents/:agentID/versions/:versionID/activate` |
| 快捷换模型 | `POST /api/v1/agents/:agentID/model-config-releases` |
| 创建/列举训练 | `POST/GET /api/v1/agents/:agentID/training-sessions` |
| 训练详情 | `GET /api/v1/training-sessions/:trainingID` |
| 训练执行 | `POST /api/v1/training-sessions/:trainingID/runs` |
| 修改与历史 | 训练路径下 `GET /diff`、`GET /checkpoints`、`POST /checkpoints/:checkpointID/restore` |
| 训练配置与发布 | 训练路径下 `PATCH /config`、`POST /validate`、`POST /publish`、`POST /cancel` |
| 普通聊天 | 复用现有 Conversation/消息/运行/取消/重命名/删除接口，改为绑定 agent_id |

管理、模板、模型选项和版本管理接口要求管理员。接口不接受客户端指定租户、密钥正文、
任意宿主目录、发布摘要或权限扩张。参数错误、权限不足、CAS 冲突、容量繁忙分别返回
可识别错误，前端保留草稿并提示正确下一步，不盲目重试变更。

## 11. 失败、中断和恢复

训练状态为 active → validating → ready → published，另有 cancelled、stale、expired
终态。失败校验回 active；终态不能继续编辑，需要新建训练。取消和过期须确认子进程
全部停止后才能清除操作槽、释放指针，超时不等于进程已停止。

断线重连重新获取会话、消息、差异和操作快照，不自动再发一次训练 Run。历史 run_id
有持久记录，重复旧请求不能触发第二次工具执行。发布依靠来源训练 ID 永久去重；
快捷模型发布依靠预期行版本 CAS，旧请求返回冲突及当前版本。

Git 和数据库不是同一个事务。操作先保留准备记录，再准备文件，最后提交版本和指针。
重启时对账：已经提交的版本保留，未提交的仅清理本操作且未被引用的产物。初始创建与
快捷模型发布使用平台独占的持久准备文件，训练发布使用训练操作槽，不增加第四张表。

备份覆盖同批数据库、Git 对象和全部 refs/tags，以及实际持久化的附件/必要平台状态。
恢复先核对对象、引用和摘要，再重建运行目录；缺失版本不能回退读取公共模板。

## 12. 建议默认值（待审核）

| 项目 | 建议值 | 规则 |
| --- | --- | --- |
| 训练期限 | 24 小时 | 创建时固定，不因聊天或查询续期 |
| 校验证据期限 | 30 分钟 | 不晚于训练期限；已发布版本不因此自动失效 |
| 实例上限 | 全局 32、单组织 8 | 创建中、缓存及运行实例均计数 |
| 校验预留 | 全局 2、单组织 1 | 普通聊天/训练不能占用预留槽，不抢占活跃任务 |
| 空闲回收/等待 | 15 分钟 / 最长 5 秒 | 容量不足返回可重试繁忙 |
| 文件限制 | 单文件 1 MiB，整包 32 MiB/2,000 路径 | SKILL.md 单独限制 256 KiB |
| 脚本超时 | 每脚本 30 秒 | 解释器/依赖需显式批准，默认允许清单为空 |
| 版本分页 | 默认 20，上限 100 | 稳定游标分页 |

以上是本次提交审核的建议，不视为用户已经确认。配置中给出明确默认值，不能由不同
实现路径自行选取。文件校验和 OS 边界负责不同层面的约束，不能互相替代。

## 13. 第一版边界与验收

包含多 Agent 列表和选择聊天、组织/用户隔离、独立训练、checkpoint、校验、人工发布、
快捷换模型、完整回滚、迁移和恢复验证。这里的“训练”是调整行为资产与配置，不是模型权重微调。

第一版不做自动质量评测、自主发布、自主创建其他 Agent、多人共同训练、跨会话共享
长期记忆、事件重放平台或多节点训练接管。单进程配合 MySQL 和持久化文件盘运行。

验收必须覆盖：创建两个不同名称/用途的 Agent；分别训练且互不影响；客户选择后才
聊天；历史绑定正确；未发布修改不可见；发布后新会话/下一轮按规则更新；换模型和回滚
保留历史；两组织/两用户数据隔离；断线不重复执行；进程未停止不释放训练；Git/数据库
故障可对账；图片、流式回复和历史管理继续工作。

## 14. 审核与开发安排

先由用户审阅本方案及技术规格，提出修改，并明确批准后才恢复开发。已有提前写出的
底层代码已暂停、保留，不构成默认批准，也不限制本方案修改。

审核通过后按四个阶段实施：通用权限与只读检查基础；Agent 管理、正式版本、列表和
聊天；独立训练、校验、发布与恢复；管理员完整工作台、快捷换模型及端到端验收。
每阶段提供可审查的实际结果和测试证据，未经授权不部署或迁移真实客户数据。

审核时需明确的产品选择是登录接入、训练是否仅创建管理员可见、普通会话是否默认
跟随最新正式版本，以及上述建议默认值。其他内容按本方案给出完整流程，不以未决事项
为由提前开始代码实现。

## 15. 与技术主规格联动的开发范围

| 产品能力 | 技术主规格对应章节 | 必须落地的代码和验证 |
| --- | --- | --- |
| 多个独立名称/用途的 Agent | §3、§4.1、§9.1 | Agent 实体、租户查询、创建/编辑接口；稳定 ID 不随改名变化，同模板可创建多个 |
| 模板变为独立资产 | §4.2、§5.1、§10.1 | 模板组合、初始 Git 快照、不可变 v1、bootstrap 幂等；公共模板修改不影响已有 Agent |
| 选择 Agent 后聊天 | §5.2、§9.3 | 列表/详情 API、目录和聊天页面、Conversation 绑定、历史恢复；无目标不自动代选 |
| 聊天文件和历史隔离 | §3、§5、§6、§10.1 | tenant+user 存储边界、每会话 Runtime/目录、工具与 MCP 沙箱；跨组织/会话攻击测试 |
| 多段独立训练 | §4.3、§7.1–§7.2 | 第三张表、训练创建和操作槽、聊天持久化、同 Agent 非终态唯一约束 |
| 修改记录和恢复 | §5.3、§7.2 | 真实文件枚举、checkpoint refs、diff、restore 和 partial 元数据；危险文件不入库 |
| 校验和人工发布 | §4.2、§7.3、§8.1 | 统一摘要、只读验证、证据期限、CAS/事务、重复发布去重；变更和过期证据拒绝复用 |
| 下一轮更新与回滚 | §8.2 | Runtime 先就绪再更新会话版本、历史版本引用重检和整体激活；失败保留旧绑定 |
| 中断、取消、过期 | §8.3–§8.4 | 执行前持久记录、进程停止确认、惰性过期、Git/DB 对账；重连不重发执行 |
| 数据迁移和恢复 | §10.1、§11.3 | 0007–0010 分阶段迁移、可信归属回填、外键/唯一键、备份恢复演练；不盲目升级存量库 |
| 管理员页面 | §9.2–§9.3 | 页面背后的真实权限和用例服务、训练/差异/版本/发布操作；禁止只注册空接口或模拟成功 |

本补充方案对原规格的新增/细化内容，需要一起审核：独立 `/agents` 入口、首条消息才
创建会话、明确的 URL/切换规则，以及快捷换模型接口、管理员模型选项接口。快捷模型
发布还要求允许非训练来源的新版本，以及平台独占的持久发布准备记录；这两点必须
同步进入主规格 §4.2、§5.1、§8 和 §10.1 后实施，不能由接口私自绕过版本流程。

代码责任保持原规格 §10.2 的分层：`pi/` 负责通用权限和 Workspace 检查；
`application/identity/` 负责可信身份；`agentcatalog`、`agentruntime`、`agenttraining`、
`agentversion` 服务分别负责目录、运行实例、训练和发布；Git/文件适配器负责资产；
Repository/迁移负责存储；HTTP 控制器和前端调用真实服务，不复制业务状态机。

批准后的交付范围是这些模块连通的完整功能代码及测试、迁移与运行说明。验收同时对照
本文 §13 和主规格 §11，不以某个页面能打开、一个底层包通过测试或接口返回固定数据
代替完整链路可用。真实宿主登录和生产迁移仍需要对应部署信息和单独的操作授权。


## 16. 代码草案的阅读方式与文件落位

以下是提交审核的代码草案，仍属于本文档，不代表已写入生产目录或已完成集成。
Go 核心示例统一放在 `agentdraft` 包中，方便跨代码块检查类型；批准后按下表拆回
原技术设计 §10.2 的实际包。端口是明确的待实现契约，不允许用返回固定成功的适配器交付。
SQL 展示各阶段的目标结构，迁移器须按实际 schema 断点执行，不能未经回填就直接整段运行。

| 目标文件/目录 | 本文代码 |
| --- | --- |
| `application/identity/principal.go` | §17 的 Principal 与可信认证边界 |
| `domain/entity/agent/`、`domain/entity/agenttraining/` | §17 的实体、状态、配置与证据 |
| `migrations/0007…0010` | §18 的建表和约束 |
| `application/service/agentcatalog/`、`agentruntime/` | §19 的创建、聊天绑定、运行租约 |
| `application/service/agenttraining/`、`agentversion/` | §20–§22 的训练、证据、发布与换模型 |
| `infrastructure/persistence/agent/`、`agenttraining/` | §21 的真实 SQL 事务 |
| `infrastructure/controller/http/agent/`、`agenttraining/` | §23 的请求绑定、错误和路由 |
| `frontend/static/js/pages/` | §24 的选择、首次建会话和重连 |
| 对应 `*_test.go`、`*_test.mjs` | §25 的可验收行为用例 |

### 16.1 必须与现有代码对接的变化

当前 `CreateConversationDTO` 只有 `profile_code`，需增加 `agent_id` 并在兼容期拒绝两者
同时出现；现有 `ConversationVO` 要增加 Agent 身份和展示信息。`conversation.Runner`
不能继续在 Web 路径中自动创建无归属会话，聊天运行前必须有已经校验的绑定。

HTTP 层继续使用现有 `ginsdk.Send`/统一错误码封装，SSE 继续使用现有事件协议；普通聊天
不得因为新增训练功能就改变现有停止、图片或消息持久化行为。下面的前端示例仍消费
`{code,msg,data}` 响应，不另建第二套响应格式。

## 17. 实体、身份、配置快照与证据代码

目标：身份位于 application，实体位于 domain；不得把 TenantID/AgentID 放入 `pi.RunRequest`。
SecretRef 只能由服务端允许配置解析，模型快照不能直接序列化含 APIKey 的 `providers.Options`。
`RawMessage` 字段入库前必须经过版本化 schema、重复键和未知字段检查，不表示任意 JSON 都合法。

```go
package agentdraft

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid input")
	ErrBusy         = errors.New("runtime busy")
	ErrExpired      = errors.New("training expired")
	ErrStale        = errors.New("training base changed")
)

type Principal struct {
	TenantID string
	UserID   string
	Role     string
}

func (p Principal) Validate() error {
	if p.TenantID == "" || p.UserID == "" {
		return ErrUnauthorized
	}
	if p.Role != "user" && p.Role != "admin" {
		return ErrForbidden
	}
	return nil
}

func (p Principal) RequireAdmin() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Role != "admin" {
		return ErrForbidden
	}
	return nil
}

type Authenticator interface {
	// 验证宿主凭证后返回身份；不得直接信任 X-Tenant-ID/X-Role。
	Authenticate(*http.Request) (Principal, error)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, error) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	return p, p.Validate()
}

type Starter struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

type Presentation struct {
	Icon     string    `json:"icon"`
	Welcome  string    `json:"welcome"`
	Starters []Starter `json:"starters"`
	Order    int       `json:"order"`
}

type Agent struct {
	ID                      string
	TenantID                string
	Name                    string
	Description             string
	Status                  string // enabled / archived
	Presentation            Presentation
	TemplateCode            string
	BootstrapKey            *string
	ActiveVersionID         *string
	ActiveTrainingSessionID *string
	RowVersion              uint64
	CreatedBy               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type ModelConfig struct {
	ProviderRef  string          `json:"provider_ref"`
	Protocol     string          `json:"protocol"`
	BaseURL      string          `json:"base_url"`
	ModelID      string          `json:"model_id"`
	SecretRef    string          `json:"secret_ref"`
	Parameters   json.RawMessage `json:"parameters"`
	Capabilities []string        `json:"capabilities"`
}

type Snapshot struct {
	DigestVersion int             `json:"digest_version"`
	Model         ModelConfig     `json:"model_config"`
	ToolPolicy    json.RawMessage `json:"tool_policy"`
	RuntimeConfig json.RawMessage `json:"runtime_config"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type Validation struct {
	Head                   string          `json:"head"`
	DigestVersion          int             `json:"digest_version"`
	BundleDigest           string          `json:"bundle_digest"`
	SpecDigest             string          `json:"spec_digest"`
	ValidatorVersion       string          `json:"validator_version"`
	ValidationPolicyDigest string          `json:"validation_policy_digest"`
	Passed                 bool            `json:"passed"`
	ReviewMode             string          `json:"review_mode"`
	Diagnostics            []Diagnostic    `json:"diagnostics"`
	Smoke                  json.RawMessage `json:"smoke"`
	ValidatedAt            time.Time       `json:"validated_at"`
	ValidUntil             time.Time       `json:"valid_until"`
}

type AgentVersion struct {
	ID                      string
	TenantID                string
	AgentID                 string
	Number                  uint64
	BundleCommit            string
	BundleTag               string
	BundleDigest            string
	Snapshot                Snapshot
	SpecDigest              string
	Validation              Validation
	SourceTrainingSessionID *string
	PublishedBy             string
	PublishedAt             time.Time
	ChangeSummary           string
}

type Operation struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	RequestDigest   string          `json:"request_digest"`
	State           string          `json:"state"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	TargetVersionID string          `json:"target_version_id,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
}

type TrainingSession struct {
	ID                string
	TenantID          string
	AgentID           string
	AdminUserID       string
	ConversationID    string // agent_conversations.id 内部主键
	BaseVersionID     string
	CandidateHead     string
	CandidateSnapshot Snapshot
	CandidatePartial  bool
	Status            string
	RowVersion        uint64
	ExpiresAt         time.Time
	Validation        *Validation
	Operation         *Operation
	ResultVersionID   *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func IsTerminal(status string) bool {
	switch status {
	case "published", "cancelled", "stale", "expired":
		return true
	default:
		return false
	}
}

func CheckTrainingOwner(p Principal, s TrainingSession) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}
	if s.TenantID != p.TenantID || s.AdminUserID != p.UserID {
		return ErrNotFound
	}
	return nil
}
```

数据库实体不直接作为 API 响应返回。VO 只暴露必要字段；`row_version` 建议作为十进制
字符串传给 JavaScript，避免 uint64 超过 JS 安全整数范围后破坏 CAS。UI 不自行递增该值。

## 18. 建表 SQL 与迁移边界

### 18.1 0007：Agent、不可变版本与 nullable 会话归属

新增表的标识字段使用二进制排序规则，避免不同大小写的租户身份被视为同一个组织。
已有 Conversation 内部主键目前是 `utf8mb4_unicode_ci`；后续训练表关联列显式与它匹配。
存量 user_id/conversation_id 的查询要按可信身份精确匹配，不利用旧不区分大小写的比较扩大权限。

```sql
CREATE TABLE agents (
    id VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'enabled',
    presentation_json JSON NOT NULL,
    template_code VARCHAR(64) NOT NULL,
    bootstrap_key VARCHAR(64) NULL,
    active_version_id VARCHAR(32) NULL,
    active_training_session_id VARCHAR(32) NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agents_tenant_id (tenant_id, id),
    UNIQUE KEY uq_agents_bootstrap (tenant_id, bootstrap_key),
    CONSTRAINT ck_agents_status CHECK (status IN ('enabled','archived'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE agent_versions (
    id VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    agent_id VARCHAR(32) NOT NULL,
    version BIGINT UNSIGNED NOT NULL,
    bundle_commit VARCHAR(64) NOT NULL,
    bundle_tag VARCHAR(255) NOT NULL,
    bundle_digest VARCHAR(71) NOT NULL,
    model_config_json JSON NOT NULL,
    tool_policy_json JSON NOT NULL,
    runtime_config_json JSON NOT NULL,
    spec_digest VARCHAR(71) NOT NULL,
    validation_json JSON NOT NULL,
    source_training_session_id VARCHAR(32) NULL,
    published_by VARCHAR(128) NOT NULL,
    published_at DATETIME(6) NOT NULL,
    change_summary TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_versions_number (agent_id, version),
    UNIQUE KEY uq_versions_scope (tenant_id, agent_id, id),
    UNIQUE KEY uq_versions_training (source_training_session_id),
    CONSTRAINT fk_versions_agent FOREIGN KEY (tenant_id, agent_id)
        REFERENCES agents (tenant_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE agents ADD CONSTRAINT fk_agents_active_version
    FOREIGN KEY (tenant_id, id, active_version_id)
    REFERENCES agent_versions (tenant_id, agent_id, id);

ALTER TABLE agent_conversations
    ADD tenant_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
    ADD conversation_type VARCHAR(16) NOT NULL DEFAULT 'chat',
    ADD agent_id VARCHAR(32) COLLATE utf8mb4_bin NULL,
    ADD agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NULL,
    ADD follow_latest BOOLEAN NOT NULL DEFAULT TRUE;
```

`active_version_id` 初始为 NULL，先插入 Agent，再插入 v1，最后在同一事务中切指针。
`source_training_session_id` 的目标表尚不存在，不能在 0007 提前添加训练外键。
不得对名称或 template_code 擅自增加唯一约束；稳定身份是 id，同模板可重复创建。

### 18.2 0008/0009：回填验证之后收紧归属

以下 DDL 的前置条件是：停写、已确认历史租户映射、bootstrap 完成、所有会话归属与
版本摘要验证通过。迁移器逐条检查 `information_schema` 决定执行/跳过，支持 MySQL DDL 中断恢复。

```sql
ALTER TABLE agent_conversations
    MODIFY tenant_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    MODIFY agent_id VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
    MODIFY agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
    ADD UNIQUE KEY uq_agent_conversations_tenant_owner
        (tenant_id, user_id, conversation_id),
    ADD UNIQUE KEY uq_agent_conversations_agent_row (tenant_id, agent_id, id),
    ADD CONSTRAINT ck_conversations_type
        CHECK (conversation_type IN ('chat','training')),
    ADD CONSTRAINT fk_conversations_agent FOREIGN KEY (tenant_id, agent_id)
        REFERENCES agents (tenant_id, id),
    ADD CONSTRAINT fk_conversations_version
        FOREIGN KEY (tenant_id, agent_id, agent_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id);

ALTER TABLE agent_conversations DROP INDEX uq_agent_conversations_owner;

-- 0008 down 前先执行；存在任意结果则拒绝回滚，不删除冲突数据。
SELECT user_id, conversation_id, COUNT(*) AS duplicate_count
FROM agent_conversations
GROUP BY user_id, conversation_id
HAVING COUNT(*) > 1;

-- 0009：仅在旧 Profile 客户端退役、代码不再读写此列后执行。
-- 先依据实际 schema 删除依赖 profile_code 的旧索引，再删除该列。
ALTER TABLE agent_conversations DROP COLUMN profile_code;
```

0008 down 先恢复旧唯一键，再移除本次外键/新索引并放宽归属列。0009 down 不能凭空恢复
新建 Agent 的旧 Profile 来源，恢复策略必须明确记录，不伪造映射。

### 18.3 0010：训练表和真正的单活约束

```sql
CREATE TABLE agent_training_sessions (
    id VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    agent_id VARCHAR(32) NOT NULL,
    admin_user_id VARCHAR(128) NOT NULL,
    conversation_id VARCHAR(32) COLLATE utf8mb4_unicode_ci NOT NULL,
    base_version_id VARCHAR(32) NOT NULL,
    candidate_head VARCHAR(64) NOT NULL,
    candidate_config_json JSON NOT NULL,
    candidate_partial BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL,
    active_slot TINYINT GENERATED ALWAYS AS (
        CASE WHEN status IN ('active','validating','ready') THEN 1 ELSE NULL END
    ) STORED,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    expires_at DATETIME(6) NOT NULL,
    validation_json JSON NULL,
    operation_json JSON NULL,
    result_version_id VARCHAR(32) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_training_scope (tenant_id, agent_id, id),
    UNIQUE KEY uq_training_conversation (conversation_id),
    UNIQUE KEY uq_training_active (tenant_id, agent_id, active_slot),
    CONSTRAINT ck_training_status CHECK
        (status IN ('active','validating','ready','published','cancelled','stale','expired')),
    CONSTRAINT fk_training_agent FOREIGN KEY (tenant_id, agent_id)
        REFERENCES agents (tenant_id, id),
    CONSTRAINT fk_training_base FOREIGN KEY (tenant_id, agent_id, base_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id),
    CONSTRAINT fk_training_conversation FOREIGN KEY (tenant_id, agent_id, conversation_id)
        REFERENCES agent_conversations (tenant_id, agent_id, id),
    CONSTRAINT fk_training_result FOREIGN KEY (tenant_id, agent_id, result_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE agents ADD CONSTRAINT fk_agents_active_training
    FOREIGN KEY (tenant_id, id, active_training_session_id)
    REFERENCES agent_training_sessions (tenant_id, agent_id, id);

ALTER TABLE agent_versions ADD CONSTRAINT fk_versions_source_training
    FOREIGN KEY (tenant_id, agent_id, source_training_session_id)
    REFERENCES agent_training_sessions (tenant_id, agent_id, id);
```

终态 active_slot 为 NULL，可以保留多条历史。不能用包含不同 Session ID 的唯一键来
冒充“同 Agent 单活”约束。外键保证所属 Agent 一致，Conversation 的 type/admin 关系
还必须由创建事务检查；进程停止前不能仅因 expires_at 到期就释放槽位。


## 19. 创建 Agent、目录查询和聊天运行代码

### 19.1 创建用例及端口

`PrepareInitial` 必须实际复制模板、保存准备记录、建立 Git 引用、物化只读目录并完整
校验，不能返回伪造的 commit/摘要。`CommitInitial` 在事务中插入版本并 CAS 切指针。
保留创建未完成的 Agent，失败不能把它投影成 selectable。以下请求中不包含 tenant_id、
created_by、Secret 正文、工作目录或客户端提供的摘要。

```go
package agentdraft

import (
	"context"
	"strings"
	"unicode/utf8"
)

type CreateAgentRequest struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	TemplateCode string       `json:"template_code"`
	Model        ModelChoice  `json:"model_config"`
	Presentation Presentation `json:"presentation"`
}

type IDSource interface{ NextID() string }

type CatalogStore interface {
	ReserveDraft(context.Context, Agent) (Agent, error)
	CommitInitial(context.Context, Agent, AgentVersion, uint64) error
	FindAgent(context.Context, string, string) (Agent, error)
	FindVersion(context.Context, string, string, string) (AgentVersion, error)
}

type InitialVersionBuilder interface {
	// 必须按服务端允许配置解析请求；HTTP DTO 不接收 SecretRef/BaseURL。
	PrepareInitial(context.Context, Principal, Agent, string, ModelChoice) (AgentVersion, error)
}

type CatalogService struct {
	IDs     IDSource
	Store   CatalogStore
	Builder InitialVersionBuilder
}

func (s *CatalogService) Create(ctx context.Context, p Principal, in CreateAgentRequest) (Agent, error) {
	if err := p.RequireAdmin(); err != nil {
		return Agent{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 128 || !utf8.ValidString(in.Name) {
		return Agent{}, ErrInvalid
	}
	if in.TemplateCode == "" {
		in.TemplateCode = "general"
	}
	a, err := s.Store.ReserveDraft(ctx, Agent{
		ID: s.IDs.NextID(), TenantID: p.TenantID,
		Name: in.Name, Description: strings.TrimSpace(in.Description),
		Status: "enabled", Presentation: in.Presentation,
		TemplateCode: in.TemplateCode, CreatedBy: p.UserID,
		// 普通创建不设置 BootstrapKey，不能以 TemplateCode 去重。
	})
	if err != nil {
		return Agent{}, err
	}
	v, err := s.Builder.PrepareInitial(ctx, p, a, s.IDs.NextID(), in.Model)
	if err != nil {
		return a, err
	} // 返回 draft ID 供管理端查询，不宣称创建完成。
	if v.TenantID != a.TenantID || v.AgentID != a.ID || v.Number != 1 {
		return a, ErrInvalid
	}
	if err := s.Store.CommitInitial(ctx, a, v, a.RowVersion); err != nil {
		return a, err
	}
	return s.Store.FindAgent(ctx, p.TenantID, a.ID)
}
```

请求校验还包括有界 description、图标允许清单、推荐问题数量，以及模型 JSON 严格
schema；它们由 HTTP 严格绑定与 InitialVersionBuilder 共用验证器落实。上面的业务代码
展示身份、草稿、准备和提交顺序，不能替代参数验证器或 Git/DB 适配器。

目录查询不能扫描所有资产或启动 Runtime；SQL 首次查询就带租户。排序使用
presentation.order/id；分页游标绑定租户、关键词、状态过滤及最后一项位置。下面是
已解码且验证过游标后的查询形状，limit 先限制为 1–100，再取 limit+1 判断下一页：

```sql
SELECT a.id, a.name, a.description, a.presentation_json, a.row_version,
       a.active_version_id
FROM agents AS a
JOIN agent_versions AS v
  ON v.tenant_id = a.tenant_id AND v.agent_id = a.id AND v.id = a.active_version_id
WHERE a.tenant_id = ? AND a.status = 'enabled'
  AND (a.name LIKE ? OR a.description LIKE ?)
  AND (
    CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.presentation_json,'$.order')), '0') AS SIGNED) > ?
    OR (
      CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.presentation_json,'$.order')), '0') AS SIGNED) = ?
      AND a.id > ?
    )
  )
ORDER BY CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.presentation_json,'$.order')), '0') AS SIGNED), a.id
LIMIT ?;
```

首屏不带游标时省略整段游标谓词，不能伪造一个可能跳过负排序值的起始游标。
`default_agent_id` 单独对完整可用集合计算，不从当前搜索页第一项猜测。SQL 参数全部绑定，
搜索中的 `%`/`_` 按产品选择转义为字面量，不把用户输入拼进 SQL。

### 19.2 会话绑定与版本加载

新会话创建事务必须检查 Agent enabled、所属租户和当前正式版本，并保存绑定；读取
历史用 tenant+user+conversation_id 且 type=chat，不能按裸 ID 读取后再判断归属。
首次发送的“创建会话”与“运行”是两个现有 API 动作，运行路径不得自动补建无绑定记录。

```go
package agentdraft

import "context"

type Binding struct {
	TenantID       string
	UserID         string
	ConversationID string
	AgentID        string
	VersionID      string
	RowVersion     uint64
	FollowLatest   bool
}

type RuntimeKey struct {
	Purpose, TenantID, AgentID, ConversationID, VersionID, OperationID, SpecDigest string
}

type RuntimeLease interface {
	// 实际实现暴露 pi.Runner；业务身份不下沉到 pi.RunRequest。
	Release()
}

type RuntimeManager interface {
	Acquire(context.Context, RuntimeKey, AgentVersion) (RuntimeLease, error)
}

type ChatBindingStore interface {
	FindOwnedChat(context.Context, Principal, string) (Binding, error)
	FindAgent(context.Context, string, string) (Agent, error)
	FindVersion(context.Context, string, string, string) (AgentVersion, error)
	// 事务中锁 Agent，重检 enabled/当前指针和会话 CAS，再更新会话版本。
	// follow_latest=false 时也检查 Agent 状态，但保留固定版本。
	CommitAdmission(context.Context, Principal, Binding, string) (Binding, error)
}

type ChatAdmission struct {
	Store    ChatBindingStore
	Runtimes RuntimeManager
}

func (s *ChatAdmission) Prepare(ctx context.Context, p Principal, conversationID string) (Binding, RuntimeLease, error) {
	if err := p.Validate(); err != nil {
		return Binding{}, nil, err
	}
	b, err := s.Store.FindOwnedChat(ctx, p, conversationID)
	if err != nil {
		return Binding{}, nil, err
	}
	a, err := s.Store.FindAgent(ctx, p.TenantID, b.AgentID)
	if err != nil {
		return Binding{}, nil, err
	}
	if a.Status != "enabled" || a.ActiveVersionID == nil {
		return Binding{}, nil, ErrConflict
	}
	target := b.VersionID
	if b.FollowLatest {
		target = *a.ActiveVersionID
	}
	v, err := s.Store.FindVersion(ctx, p.TenantID, b.AgentID, target)
	if err != nil {
		return Binding{}, nil, err
	}
	lease, err := s.Runtimes.Acquire(ctx, RuntimeKey{
		Purpose: "chat", TenantID: p.TenantID, AgentID: b.AgentID,
		ConversationID: b.ConversationID, VersionID: v.ID, SpecDigest: v.SpecDigest,
	}, v)
	if err != nil {
		return Binding{}, nil, err
	}
	admitted, err := s.Store.CommitAdmission(ctx, p, b, target)
	if err != nil {
		lease.Release()
		return Binding{}, nil, err
	}
	return admitted, lease, nil
}
```

调用方取得 lease 后再用既有 Conversation Runner 装载历史并执行，结束、取消及出错
都释放 lease。相同会话还有独立的运行互斥；RuntimeManager 不能允许两个持有者同时
使用同一实例。准备期间正式版本变化返回冲突，可由服务端有界重试，不能静默覆盖绑定。

RuntimeManager 的缓存键、容量预占、关闭失败隔离和 FIFO 由技术主规格 §5.2/Phase 1B
计划实现。Factory 使用保存的有效配置并强制 chat/validation `AllowWrite=false`，
受限进程仅写 scratch/.tmp；不能根据新宿主默认值悄悄重新解释旧快照。

## 20. 训练操作、checkpoint 和验证决策代码

### 20.1 单操作槽及失效规则

开始训练、占操作槽、持久输入属于短事务。Agent 锁不跨整轮模型执行；运行中的持久
操作槽负责拒绝第二个写入者。以下函数只能在持有对应数据库行锁的事务内调用。
状态检查前必须先处理同一已完成操作/已发布结果的幂等返回；不能因旧请求重复到达而重新执行。

```go
package agentdraft

import "time"

func BeginAuthorOperation(p Principal, s *TrainingSession, expected uint64, op Operation, now time.Time) error {
	if err := CheckTrainingOwner(p, *s); err != nil {
		return err
	}
	if s.RowVersion != expected {
		return ErrConflict
	}
	if IsTerminal(s.Status) {
		return ErrConflict
	}
	if !now.Before(s.ExpiresAt) {
		return ErrExpired
	}
	if s.Operation != nil && s.Operation.State == "running" {
		return ErrConflict
	}
	if s.Status != "active" && s.Status != "ready" {
		return ErrConflict
	}
	if op.ID == "" || op.RequestDigest == "" || op.Kind != "run" {
		return ErrInvalid
	}
	s.Status = "active"
	s.Validation = nil
	op.State, op.StartedAt = "running", now
	s.Operation = &op
	s.RowVersion++
	return nil
}

func CloseTrainingAfterStop(s *TrainingSession, terminal string, stopped bool) error {
	if IsTerminal(s.Status) {
		return nil
	}
	if terminal != "cancelled" && terminal != "stale" && terminal != "expired" {
		return ErrInvalid
	}
	if !stopped {
		return ErrBusy
	}
	s.Status, s.Validation, s.Operation = terminal, nil, nil
	s.RowVersion++
	return nil
}
```

`stopped` 只能来自平台 Supervisor 的 StopAndConfirm 结果，不能由请求 JSON 提供。
同一事务要按 Session ID CAS 清除 Agent 活跃指针；仅更新 Session 不够。过期查询可以
触发停止，但进程未停时保持非终态和占用。published 终态的重复请求先返回 result_version_id。

训练输入使用现有消息唯一键 `(conversation_id,turn_version,ordinal)`：BeginTurn 预留
turn_version 并写入 ordinal=0 的用户输入，完成时 FinishTurn 只追加后续输出和 invocation。
把当前输入从传给模型的历史窗口排除，避免本轮输入出现两次。历史 run_id 从持久输入/
invocation 判断是否已执行，不能依赖可能被覆盖的 operation_json。

checkpoint 的端口和必要执行顺序如下；这些端口执行真实 Git 操作，不向模型暴露 Git 权限：

```go
package agentdraft

import "context"

type Checkpoint struct {
	ID, Head, RunID, ActorID string
	Partial                  bool
}

type CandidateStore interface {
	// 排除项只有已确认的平台 .tmp/.git；危险路径/类型/Secret 不进入提交。
	Checkpoint(context.Context, TrainingSession, string, bool) (Checkpoint, error)
	Restore(context.Context, TrainingSession, string) (Checkpoint, error)
}

type AuthorProcesses interface {
	CloseAdmission()
	StopAndConfirm(context.Context) error
}

func FreezeCandidate(ctx context.Context, processes AuthorProcesses, bundles CandidateStore,
	s TrainingSession, runID string, partial bool) (Checkpoint, error) {
	processes.CloseAdmission()
	if err := processes.StopAndConfirm(ctx); err != nil {
		return Checkpoint{}, err
	}
	return bundles.Checkpoint(ctx, s, runID, partial)
}
```

checkpoint 先写不可变 `refs/training/<id>/checkpoints/<checkpointID>`，再推进持久 head ref，
最后在操作槽完成事务 CAS candidate_head；记录目标 ref 以恢复中断。恢复只能选择本会话
可枚举的 checkpoint，恢复其 partial 元数据，保留较新的历史 refs。

### 20.2 验证证据复用的具体条件

`CurrentCandidate` 必须从真实目录、固定 HEAD 和有效配置重算；`RuntimeReferencesOK`
来自当次发布的外部引用检查，不是只读取旧报告。now 使用数据库 UTC 时间。

```go
package agentdraft

import "time"

type CurrentCandidate struct {
	Head, BundleDigest, SpecDigest string
	DigestVersion                  int
	TreeClean, RuntimeReferencesOK bool
}

type ValidatorIdentity struct {
	Version, PolicyDigest string
	DigestVersion         int
}

type ValidationDecision string

const (
	RejectEvidence ValidationDecision = "reject"
	Revalidate     ValidationDecision = "revalidate"
	ReuseEvidence  ValidationDecision = "reuse"
)

func DecideValidation(s TrainingSession, c CurrentCandidate, impl ValidatorIdentity,
	now time.Time) (ValidationDecision, error) {
	if !now.Before(s.ExpiresAt) {
		return RejectEvidence, ErrExpired
	}
	if s.Status != "ready" || s.CandidatePartial || !c.TreeClean || !c.RuntimeReferencesOK {
		return RejectEvidence, ErrConflict
	}
	v := s.Validation
	if v == nil || !v.Passed || v.ReviewMode != "manual" {
		return RejectEvidence, ErrConflict
	}
	if v.Head == "" || v.BundleDigest == "" || v.SpecDigest == "" ||
		v.ValidatorVersion == "" || v.ValidationPolicyDigest == "" ||
		v.ValidatedAt.IsZero() || v.ValidUntil.IsZero() {
		return RejectEvidence, ErrInvalid
	}
	if c.DigestVersion != impl.DigestVersion || v.DigestVersion != c.DigestVersion {
		return RejectEvidence, ErrInvalid
	}
	if c.Head != s.CandidateHead || v.Head != c.Head ||
		v.BundleDigest != c.BundleDigest || v.SpecDigest != c.SpecDigest {
		return RejectEvidence, ErrConflict
	}
	if now.Before(v.ValidatedAt) || v.ValidUntil.After(s.ExpiresAt) ||
		!v.ValidUntil.After(v.ValidatedAt) {
		return RejectEvidence, ErrInvalid
	}
	if !now.Before(v.ValidUntil) || v.ValidatorVersion != impl.Version ||
		v.ValidationPolicyDigest != impl.PolicyDigest {
		return Revalidate, nil
	}
	return ReuseEvidence, nil
}
```

决策为 reject 时失效旧证据并按原因回 active、stale 或走过期停止流程；为 revalidate 时
先取得校验配额，再 ready→validating，完整重检成功后回 ready；为 reuse 时仍要在最终
提交事务检查期限和绑定。初始版本/快捷模型发布没有训练期限，不能给它伪造 TrainingSession，
应复用相同的候选/证据检查器并在其外层省去训练状态和训练 expires_at 条件。


## 21. 发布的数据库事务代码

下面给出真实 SQL 事务形状：锁顺序固定为 Agent → TrainingSession，检查归属、基线、
操作槽、候选和证据，插入不可变版本后原子切换两张表的指针/状态。`PreparedPublication`
仅由服务端冻结与校验流程创建，不作为 HTTP 请求类型。其目录必须已物化、无写进程，
且同一 Agent 的变更预留在整个准备/提交期间有效。

`SnapshotCodec` 必须实现严格版本化配置解析和 §4.2 的 JCS/SHA-256，不能用普通 JSON
序列化替代；它用于从数据库候选配置重新计算摘要，防止只比较两份客户端字段。
DB 连接使用 `parseTime=true&loc=UTC`。以下 SQL 不包含 Git I/O，也不在事务中等待模型或脚本。

```go
package agentdraft

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type SnapshotCodec interface {
	ParseSnapshot([]byte) (Snapshot, error)
	SpecDigest(bundleDigest string, snapshot Snapshot) (string, error)
}

type PreparedPublication struct {
	AgentID, TrainingID, OperationID      string
	ExpectedAgentRow, ExpectedTrainingRow uint64
	Current                               CurrentCandidate
	Validator                             ValidatorIdentity
	Version                               AgentVersion
}

func CommitPreparedTraining(ctx context.Context, db *sql.DB, codec SnapshotCodec,
	p Principal, prepared PreparedPublication) (string, error) {
	if err := p.RequireAdmin(); err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() // 已提交时返回 ErrTxDone；不把它变成新的业务失败。

	var status string
	var active, activeTraining sql.NullString
	var agentRow uint64
	err = tx.QueryRowContext(ctx, `
        SELECT status, active_version_id, active_training_session_id, row_version
        FROM agents WHERE tenant_id=? AND id=? FOR UPDATE`,
		p.TenantID, prepared.AgentID).Scan(&status, &active, &activeTraining, &agentRow)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	s := TrainingSession{ID: prepared.TrainingID, TenantID: p.TenantID,
		AgentID: prepared.AgentID, AdminUserID: p.UserID}
	var configJSON string
	var validationJSON, operationJSON, resultID sql.NullString
	err = tx.QueryRowContext(ctx, `
        SELECT base_version_id, candidate_head, candidate_config_json, candidate_partial,
               status, row_version, expires_at, validation_json, operation_json, result_version_id
        FROM agent_training_sessions
        WHERE tenant_id=? AND agent_id=? AND id=? AND admin_user_id=? FOR UPDATE`,
		p.TenantID, prepared.AgentID, prepared.TrainingID, p.UserID).Scan(
		&s.BaseVersionID, &s.CandidateHead, &configJSON, &s.CandidatePartial,
		&s.Status, &s.RowVersion, &s.ExpiresAt, &validationJSON, &operationJSON, &resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	// 永久幂等先于旧 row_version/过期检查；不重新执行发布。
	if s.Status == "published" {
		if !resultID.Valid {
			return "", ErrConflict
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT id FROM agent_versions
            WHERE tenant_id=? AND agent_id=? AND source_training_session_id=?`,
			p.TenantID, prepared.AgentID, s.ID).Scan(&existing)
		if err != nil {
			return "", err
		}
		if existing != resultID.String {
			return "", ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return existing, nil
	}
	if status != "enabled" || !active.Valid || !activeTraining.Valid || activeTraining.String != s.ID {
		return "", ErrConflict
	}
	if active.String != s.BaseVersionID {
		return "", ErrStale
	}
	if agentRow != prepared.ExpectedAgentRow || s.RowVersion != prepared.ExpectedTrainingRow {
		return "", ErrConflict
	}
	if !validationJSON.Valid || !operationJSON.Valid {
		return "", ErrConflict
	}
	if err := json.Unmarshal([]byte(validationJSON.String), &s.Validation); err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(operationJSON.String), &s.Operation); err != nil {
		return "", err
	}
	if s.Operation == nil || s.Operation.ID != prepared.OperationID ||
		s.Operation.Kind != "publish" || s.Operation.State != "running" {
		return "", ErrConflict
	}
	s.CandidateSnapshot, err = codec.ParseSnapshot([]byte(configJSON))
	if err != nil {
		return "", err
	}
	digest, err := codec.SpecDigest(prepared.Current.BundleDigest, s.CandidateSnapshot)
	if err != nil {
		return "", err
	}
	if digest != prepared.Current.SpecDigest {
		return "", ErrConflict
	}

	var now time.Time // 统一从 DB 取 UTC，不用客户端时间。
	if err := tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&now); err != nil {
		return "", err
	}
	decision, err := DecideValidation(s, prepared.Current, prepared.Validator, now)
	if err != nil {
		return "", err
	}
	if decision != ReuseEvidence {
		return "", ErrConflict
	}

	v := prepared.Version
	if v.ID == "" || v.TenantID != p.TenantID || v.AgentID != s.AgentID ||
		v.SourceTrainingSessionID == nil || *v.SourceTrainingSessionID != s.ID ||
		v.BundleCommit != s.CandidateHead || v.BundleDigest != prepared.Current.BundleDigest ||
		v.SpecDigest != digest || v.PublishedBy != p.UserID {
		return "", ErrInvalid
	}
	versionDigest, err := codec.SpecDigest(v.BundleDigest, v.Snapshot)
	if err != nil {
		return "", err
	}
	if versionDigest != digest {
		return "", ErrConflict
	}
	var next uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1
        FROM agent_versions WHERE tenant_id=? AND agent_id=?`,
		p.TenantID, s.AgentID).Scan(&next); err != nil {
		return "", err
	}
	if v.Number != next {
		return "", ErrConflict
	}

	modelJSON, err := json.Marshal(v.Snapshot.Model)
	if err != nil {
		return "", err
	}
	evidenceJSON, err := json.Marshal(s.Validation) // DB 中当前有效证据才是权威值。
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_versions
        (id,tenant_id,agent_id,version,bundle_commit,bundle_tag,bundle_digest,
         model_config_json,tool_policy_json,runtime_config_json,spec_digest,validation_json,
         source_training_session_id,published_by,published_at,change_summary)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),?)`,
		v.ID, p.TenantID, s.AgentID, v.Number, v.BundleCommit, v.BundleTag, v.BundleDigest,
		string(modelJSON), string(v.Snapshot.ToolPolicy), string(v.Snapshot.RuntimeConfig),
		v.SpecDigest, string(evidenceJSON), s.ID, p.UserID, v.ChangeSummary)
	if err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agents
        SET active_version_id=?, active_training_session_id=NULL, row_version=row_version+1
        WHERE tenant_id=? AND id=? AND row_version=? AND status='enabled'
          AND active_version_id=? AND active_training_session_id=?`,
		v.ID, p.TenantID, s.AgentID, agentRow, s.BaseVersionID, s.ID)
	if err := exactlyOne(result, err); err != nil {
		return "", err
	}

	finished := now
	s.Operation.State, s.Operation.FinishedAt = "completed", &finished
	s.Operation.Result, err = json.Marshal(map[string]string{"version_id": v.ID})
	if err != nil {
		return "", err
	}
	operation, err := json.Marshal(s.Operation)
	if err != nil {
		return "", err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_training_sessions
        SET status='published',result_version_id=?,operation_json=?,row_version=row_version+1
        WHERE tenant_id=? AND agent_id=? AND id=? AND row_version=? AND status='ready'
          AND expires_at>UTC_TIMESTAMP(6) AND UTC_TIMESTAMP(6)<?`,
		v.ID, string(operation), p.TenantID, s.AgentID, s.ID, s.RowVersion, s.Validation.ValidUntil)
	if err := exactlyOne(result, err); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return v.ID, nil
}

func exactlyOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
```

`ErrStale`/`ErrExpired` 不能只向前端返回：外层用例必须停止进程并在新的受控事务中完成
对应终态与指针清理；当前失败事务先完整回滚。提交返回网络错误时不能假定 DB 未提交，
恢复必须先按 source_training_session_id 查询，再决定保留还是补偿文件。

人工确认在 HTTP DTO 与服务入口校验；数据库函数不负责展示确认弹窗。所有版本创建路径
都要遵守同一 Agent 锁、版本分配和 CAS 规则，不能让快捷模型发布绕过它。

## 22. 摘要、快捷换模型与恢复代码

### 22.1 统一摘要函数

JCS 是具体序列化协议，不是 `json.Marshal` 的别名。下面保留可替换的标准实现端口，
批准实现时选定符合 RFC 8785 的库并固定版本，使用官方测试向量验证数字和 Unicode 键序。
文件清单在摘要前必须已经通过路径/类型/大小验证，`entries` 按 UTF-8 路径字节序排序。

```go
package agentdraft

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

type CanonicalJSON interface{ Encode(any) ([]byte, error) }

type BundleEntry struct {
	Path          string `json:"path"`
	Mode          string `json:"mode"`
	Size          int64  `json:"size"`
	ContentSHA256 string `json:"content_sha256"`
}

func BundleDigest(jcs CanonicalJSON, entries []BundleEntry) (string, error) {
	ordered := append([]BundleEntry{}, entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for i, e := range ordered {
		if e.Path == "" || e.Size < 0 || (i > 0 && ordered[i-1].Path == e.Path) {
			return "", ErrInvalid
		}
	}
	encoded, err := jcs.Encode(struct {
		DigestVersion int           `json:"digest_version"`
		Entries       []BundleEntry `json:"entries"`
	}{1, ordered})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}

func CombinedDigest(jcs CanonicalJSON, bundle string, snapshot Snapshot) (string, error) {
	if snapshot.DigestVersion != 1 || bundle == "" {
		return "", ErrInvalid
	}
	encoded, err := jcs.Encode(struct {
		DigestVersion int         `json:"digest_version"`
		BundleDigest  string      `json:"bundle_digest"`
		Model         ModelConfig `json:"model_config"`
		ToolPolicy    any         `json:"tool_policy"`
		RuntimeConfig any         `json:"runtime_config"`
	}{1, bundle, snapshot.Model, snapshot.ToolPolicy, snapshot.RuntimeConfig})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}
```

JCS 适配器必须把 RawMessage 解码为经过 schema 校验的 JSON 值，不能把配置编码成
base64 字节或字符串。省略/默认值/null 的处理在严格 SnapshotCodec 中统一完成；
`CombinedDigest` 只对已解析的有效快照计算，不重新填宿主默认配置。

### 22.2 快捷模型发布服务

下面的端口共享正式版本验证与事务能力。`AcquireMutation` 同时检查正在运行的训练
操作，持有的是变更预留，不是整个耗时操作期间的 Agent mutex；普通聊天继续读正式指针。

```go
package agentdraft

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ModelChoice struct {
	ProviderRef string          `json:"provider_ref"`
	ModelID     string          `json:"model_id"`
	Parameters  json.RawMessage `json:"parameters"` // 禁止 SecretRef/BaseURL 输入。
}

type ModelReleaseRequest struct {
	ExpectedRowVersion  uint64
	BaseVersionID       string
	Model               ModelChoice
	ChangeSummary       string
	ConfirmManualReview bool
}

type ModelReleasePort interface {
	AcquireMutation(context.Context, Principal, string) (func(), error)
	Current(context.Context, Principal, string) (Agent, AgentVersion, error)
	ResolveAllowedModel(context.Context, Principal, ModelChoice) (ModelConfig, error)
	// 分配新 version ID/number；保存 intent；复制原工具/运行快照；拒绝有效配置无变化。
	PrepareModelVersion(context.Context, Principal, Agent, AgentVersion, ModelConfig, string) (AgentVersion, error)
	ValidatePrepared(context.Context, Principal, AgentVersion) (AgentVersion, error)
	// 重检基线/CAS/证据期限，写新版本与正式指针；不修改原版本或自动重试旧 CAS。
	CommitModelVersion(context.Context, Principal, Agent, AgentVersion) error
	// 按目标 ID 对账，只有 DB 已确认未提交时才能清理未引用产物；失败保留 intent。
	ReconcilePreparation(context.Context, Principal, string) error
	ReportReconciliationFailure(context.Context, string, error)
}

func ReleaseModel(ctx context.Context, port ModelReleasePort, p Principal,
	agentID string, in ModelReleaseRequest) (out AgentVersion, resultErr error) {
	if err := p.RequireAdmin(); err != nil {
		return AgentVersion{}, err
	}
	if !in.ConfirmManualReview || in.ChangeSummary == "" {
		return AgentVersion{}, ErrInvalid
	}
	release, err := port.AcquireMutation(ctx, p, agentID)
	if err != nil {
		return AgentVersion{}, err
	}
	defer release()
	a, base, err := port.Current(ctx, p, agentID)
	if err != nil {
		return AgentVersion{}, err
	}
	if a.Status != "enabled" || a.RowVersion != in.ExpectedRowVersion || base.ID != in.BaseVersionID {
		return AgentVersion{}, ErrConflict
	}
	model, err := port.ResolveAllowedModel(ctx, p, in.Model)
	if err != nil {
		return AgentVersion{}, err
	}
	prepared, err := port.PrepareModelVersion(ctx, p, a, base, model, in.ChangeSummary)
	if prepared.ID != "" {
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			cleanupErr := port.ReconcilePreparation(cleanupCtx, p, prepared.ID)
			if cleanupErr == nil {
				return
			}
			port.ReportReconciliationFailure(cleanupCtx, prepared.ID, cleanupErr)
			if out.ID == "" {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
			// 已提交版本仍返回成功；保留 intent，由下一次准入/启动恢复继续对账。
		}()
	}
	if err != nil {
		return AgentVersion{}, err
	}
	validated, err := port.ValidatePrepared(ctx, p, prepared)
	if err != nil {
		return AgentVersion{}, err
	} // intent 保留，进入对账/清理，不伪装成功。
	if err := port.CommitModelVersion(ctx, p, a, validated); err != nil {
		return AgentVersion{}, err // 包含提交结果未知；恢复先查 DB，禁止盲删文件。
	}
	return validated, nil
}
```

PrepareModelVersion 一旦分配目标 ID，即使返回错误也返回该 ID，供 defer 完成有界对账。
对账失败时，受影响 Agent 的变更继续阻塞。上面的 `defer release()` 仅释放请求持有的
内存租约；AcquireMutation 的
准入还必须检查尚未完成的持久 intent，不能因 HTTP 请求退出就允许第二次冲突写入。

恢复核心分支如下，记录中只允许服务端派生的目标 ID，路径由适配器生成，不接受任意绝对路径：

```go
package agentdraft

import "context"

type PublicationIntent struct {
	TenantID, AgentID, VersionID, BaseVersionID, Tag, BundleDigest, SpecDigest string
	ExpectedRowVersion                                                         uint64
}

type RecoveryPort interface {
	FindPreparedVersion(context.Context, PublicationIntent) (AgentVersion, bool, error)
	VerifyCommitted(context.Context, PublicationIntent, AgentVersion) error
	DeleteUnreferencedOwnedArtifacts(context.Context, PublicationIntent) error
	RemoveIntent(context.Context, PublicationIntent) error
}

func RecoverPublication(ctx context.Context, port RecoveryPort, intent PublicationIntent) error {
	v, found, err := port.FindPreparedVersion(ctx, intent)
	if err != nil {
		return err
	}
	if found {
		if err := port.VerifyCommitted(ctx, intent, v); err != nil {
			return err
		}
	} else {
		if err := port.DeleteUnreferencedOwnedArtifacts(ctx, intent); err != nil {
			return err
		}
	}
	return port.RemoveIntent(ctx, intent)
}
```

`VerifyCommitted` 验证 tenant/agent/id、文件和配置摘要及版本记录一致性；不能要求该版本
现在仍是 active_version_id，因为之后可能已有新发布或回滚。任何无法确定的结果都保留
intent 并阻止危险清理，不删除共享 commit、历史 tag 或其他训练引用。


## 23. HTTP 严格绑定与路由代码

### 23.1 拒绝未知字段、重复键、额外 JSON 和超深嵌套

普通 `ShouldBindJSON` 不足以保证重复键拒绝。以下解码器用于管理配置/发布请求，读取
最多 1 MiB，先检查全树重复键，再按 DTO 拒绝未知字段。模型 parameters 的允许字段仍由
模型 schema 验证，不能因为它是 RawMessage 就跳过检查。

```go
package agentdraft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

func DecodeManagementJSON(w http.ResponseWriter, r *http.Request, out any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: request body", ErrInvalid)
	}
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	first, err := scan.Token()
	if err != nil || first != json.Delim('{') {
		return ErrInvalid
	}
	if err := scanJSONObject(scan, 1); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: DTO", ErrInvalid)
	}
	return nil
}

func scanJSONObject(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if err := scanJSONValue(d, depth+1); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return ErrInvalid
	}
	return nil
}

func scanJSONValue(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return scanJSONObject(d, depth)
	case '[':
		for d.More() {
			if err := scanJSONValue(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}
```

### 23.2 Agent 列表、创建和训练发布控制器

下面继续使用 Gin；`SendResult` 在实际控制器层适配已有 `ginsdk.Send` 和错误码映射。
ErrUnauthorized/Forbidden/NotFound/Conflict 映射 401/403/404/409，容量 ErrBusy 映射
503；内部错误日志记录在服务端，返回内容不能暴露宿主路径或凭证。

```go
package agentdraft

import (
	"context"
	"github.com/gin-gonic/gin"
	"strconv"
)

type AgentCard struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Selectable  bool   `json:"selectable"`
	RowVersion  uint64 `json:"row_version,string"`
}

type AgentPage struct {
	Items          []AgentCard `json:"items"`
	NextCursor     string      `json:"next_cursor"`
	DefaultAgentID *string     `json:"default_agent_id"`
}

type ListAgentQuery struct {
	Keyword, Cursor string
	Limit           int
}

type PublishTrainingDTO struct {
	RequestID           string `json:"request_id"`
	ExpectedRowVersion  uint64 `json:"expected_row_version,string"`
	ChangeSummary       string `json:"change_summary"`
	ConfirmManualReview bool   `json:"confirm_manual_review"`
}

type AgentHTTPService interface {
	List(context.Context, Principal, ListAgentQuery) (AgentPage, error)
	Create(context.Context, Principal, CreateAgentRequest) (Agent, error)
	Publish(context.Context, Principal, string, PublishTrainingDTO) (string, error)
}

type SendResult func(*gin.Context, any, error)

type AgentController struct {
	Service AgentHTTPService
	Send    SendResult
}

func (c *AgentController) List(ctx *gin.Context) {
	p, err := PrincipalFrom(ctx.Request.Context())
	if err != nil {
		c.Send(ctx, nil, err)
		return
	}
	limit := 20
	if raw, present := ctx.GetQuery("limit"); present {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			c.Send(ctx, nil, ErrInvalid)
			return
		}
	}
	query := ListAgentQuery{Keyword: ctx.Query("keyword"), Cursor: ctx.Query("cursor"), Limit: limit}
	if len(query.Keyword) > 1024 || len(query.Cursor) > 4096 {
		c.Send(ctx, nil, ErrInvalid)
		return
	}
	data, err := c.Service.List(ctx.Request.Context(), p, query)
	c.Send(ctx, data, err)
}

func (c *AgentController) Create(ctx *gin.Context) {
	p, err := PrincipalFrom(ctx.Request.Context())
	if err != nil {
		c.Send(ctx, nil, err)
		return
	}
	if err := p.RequireAdmin(); err != nil {
		c.Send(ctx, nil, err)
		return
	}
	var in CreateAgentRequest
	if err := DecodeManagementJSON(ctx.Writer, ctx.Request, &in); err != nil {
		c.Send(ctx, nil, err)
		return
	}
	a, err := c.Service.Create(ctx.Request.Context(), p, in)
	// 不直接输出完整实体、版本快照或凭证引用。
	data := AgentCard{ID: a.ID, Name: a.Name, Description: a.Description, Icon: a.Presentation.Icon,
		RowVersion: a.RowVersion, Selectable: err == nil && a.Status == "enabled" && a.ActiveVersionID != nil}
	c.Send(ctx, data, err)
}

func (c *AgentController) Publish(ctx *gin.Context) {
	p, err := PrincipalFrom(ctx.Request.Context())
	if err != nil {
		c.Send(ctx, nil, err)
		return
	}
	if err := p.RequireAdmin(); err != nil {
		c.Send(ctx, nil, err)
		return
	}
	var in PublishTrainingDTO
	if err := DecodeManagementJSON(ctx.Writer, ctx.Request, &in); err != nil {
		c.Send(ctx, nil, err)
		return
	}
	if !in.ConfirmManualReview || in.RequestID == "" || in.ChangeSummary == "" {
		c.Send(ctx, nil, ErrInvalid)
		return
	}
	versionID, err := c.Service.Publish(ctx.Request.Context(), p, ctx.Param("trainingID"), in)
	c.Send(ctx, gin.H{"version_id": versionID}, err)
}

func RegisterAgentCore(r *gin.RouterGroup, c *AgentController, managementEnabled bool) {
	r.GET("/agents", c.List)
	if managementEnabled {
		r.POST("/agents", c.Create)
		r.POST("/training-sessions/:trainingID/publish", c.Publish)
	}
}
```

其余 §10 列出的接口使用同样的边界：先从已验证的 Request.Context 取得 Principal，
严格绑定请求，再调用相应用例服务。只有真实用例完成后才注册路由。页面管理入口和
managementEnabled 由宿主认证配置决定，不能从请求 Header 推导。

变更接口还需要请求来源/CSRF 检查：复用宿主已有机制；Cookie 认证接入若没有现成保护，
在管理写路由加入 CSRF token 或严格同源 Origin 校验。认证方式未确定前不虚构可直接
上线的登录/管理员凭证协议；路由单测使用注入的可信测试身份。

## 24. 前端代码：选择、首次会话和训练重连

### 24.1 目标解析与卡片渲染

这些函数落入新的 `agent-navigation.js`/`agents.js`，与现有消息渲染、图片和流处理模块组合。
名称、用途和错误必须以 textContent 渲染。卡片 URL 只带服务端返回的 ID，不能把名称当身份。

```javascript
export function resolveChatTarget(search) {
  const query = new URLSearchParams(search);
  if (query.getAll("agent_id").length > 1 || query.getAll("conversation_id").length > 1) {
    throw new Error("聊天目标参数重复");
  }
  const agentID = query.get("agent_id") || "";
  const conversationID = query.get("conversation_id") || "";
  if (!agentID && !conversationID) return { kind: "directory" };
  return { kind: conversationID ? "history" : "new", agentID, conversationID };
}

export function assertConversationAgent(conversation, selectedAgentID) {
  if (!conversation.agent_id || (selectedAgentID && conversation.agent_id !== selectedAgentID)) {
    throw new Error("历史会话与所选 Agent 不一致");
  }
}

export function createAgentCard(agent) {
  const card = document.createElement("article");
  const title = document.createElement("h2");
  const description = document.createElement("p");
  const link = document.createElement("a");
  title.textContent = agent.name;
  description.textContent = agent.description;
  link.textContent = "开始聊天";
  link.href = `/chat?agent_id=${encodeURIComponent(agent.id)}`;
  if (!agent.selectable) {
    link.removeAttribute("href");
    link.setAttribute("aria-disabled", "true");
    link.textContent = "暂不可用";
  }
  card.append(title, description, link);
  return card; // 不发送创建 Conversation 的 POST。
}
```

历史模式先 GET 已鉴权的会话详情，再检查 agent_id 并加载消息。详情 VO 必须带 Agent
名称、图标和状态，不能从当前分页列表猜。归档历史禁用发送，但仍显示原消息。

### 24.2 首条消息才创建会话

下面依赖现有 `requestJSON`、`startRun` 和界面错误展示函数，通过参数注入便于测试。
此处不新建 SSE 协议；state.running 在创建请求前置为 true，防止连点创建两个会话。

```javascript
export async function sendSelectedAgentMessage(state, content, imageURLs, io) {
  if (state.running) return;
  if (!content.trim() && imageURLs.length === 0) return;
  if (!state.selectedAgentID) throw new Error("请先选择 Agent");
  if (state.readOnly) throw new Error("此会话只能查看历史");
  state.running = true;
  try {
    if (!state.currentConversationID) {
      const conversation = await io.requestJSON("/api/v1/conversations", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ agent_id: state.selectedAgentID }),
      });
      assertConversationAgent(conversation, state.selectedAgentID);
      state.currentConversationID = conversation.id;
      io.replaceURL(`/chat?conversation_id=${encodeURIComponent(conversation.id)}`);
    }
    await io.startRun(state.currentConversationID, content, imageURLs);
  } catch (error) {
    io.showError(error.message); // 保留草稿和图片；不换默认 Agent、不自动重试 POST。
  } finally {
    state.running = false;
  }
}
```

创建响应丢失时，前端先刷新已有会话列表确认结果，再允许用户显式重试；第一版普通
创建不承诺通用幂等协议。`startRun` 继续接现有流事件；只能在确认请求已被接纳后清空
输入草稿，服务端错误和断线不能触发自动再执行。

### 24.3 训练断线恢复和人工发布

```javascript
export async function recoverTrainingView(trainingID, io, signal) {
  const prefix = `/api/v1/training-sessions/${encodeURIComponent(trainingID)}`;
  while (!signal.aborted) {
    const session = await io.requestJSON(prefix, { signal });
    const [messages, diff] = await Promise.all([
      io.loadAuthorizedTrainingMessages(session.conversation_id, signal),
      io.requestJSON(`${prefix}/diff`, { signal }),
    ]);
    io.renderSnapshot(session, messages, diff);
    if (session.operation?.state !== "running") return session;
    await io.wait(1000, signal); // 取消等待必须响应 AbortSignal。
  }
}

export async function publishTraining(session, summary, confirmed, io) {
  if (!confirmed) throw new Error("请先确认本版本未经自动质量评测");
  if (session.status !== "ready" || session.operation?.state === "running") {
    throw new Error("当前训练不能发布");
  }
  return io.requestJSON(`/api/v1/training-sessions/${encodeURIComponent(session.id)}/publish`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      request_id: io.newRequestID(),
      expected_row_version: session.row_version,
      change_summary: summary,
      confirm_manual_review: true,
    }),
  });
}
```

重连函数没有 POST `/runs`。训练消息通过单独的管理员归属入口读取，不能让普通消息
API 仅凭 conversation_id 返回训练内容。发布按钮在请求期间禁用；丢失响应时查询
Session 的 result_version_id，不自动以新请求覆盖当前操作。

## 25. 代码级验收与测试示例

### 25.1 证据绑定、到期及停止确认

以下测试直接检验业务规则。成功复用报告不能只测“函数返回 nil”，必须改变候选、
时间和策略证明旧证据会被拒绝或触发重检。

```go
package agentdraft

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func validEvidenceFixture() (TrainingSession, CurrentCandidate, ValidatorIdentity, time.Time) {
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	s := TrainingSession{TenantID: "t", AdminUserID: "u", Status: "ready", CandidateHead: "h",
		ExpiresAt: now.Add(time.Hour), Validation: &Validation{
			Head: "h", DigestVersion: 1, BundleDigest: "bundle", SpecDigest: "spec", Passed: true,
			ReviewMode: "manual", ValidatorVersion: "validator-v1", ValidationPolicyDigest: "policy-v1",
			ValidatedAt: now.Add(-time.Minute), ValidUntil: now.Add(30 * time.Minute),
		}}
	c := CurrentCandidate{Head: "h", BundleDigest: "bundle", SpecDigest: "spec", DigestVersion: 1,
		TreeClean: true, RuntimeReferencesOK: true}
	impl := ValidatorIdentity{Version: "validator-v1", PolicyDigest: "policy-v1", DigestVersion: 1}
	return s, c, impl, now
}

func TestEvidenceDecision(t *testing.T) {
	cases := []struct {
		name   string
		change func(*TrainingSession, *CurrentCandidate, *ValidatorIdentity, *time.Time)
		want   ValidationDecision
	}{
		{"same candidate", func(*TrainingSession, *CurrentCandidate, *ValidatorIdentity, *time.Time) {}, ReuseEvidence},
		{"changed head", func(_ *TrainingSession, c *CurrentCandidate, _ *ValidatorIdentity, _ *time.Time) { c.Head = "new" }, RejectEvidence},
		{"dirty tree", func(_ *TrainingSession, c *CurrentCandidate, _ *ValidatorIdentity, _ *time.Time) { c.TreeClean = false }, RejectEvidence},
		{"expired evidence", func(s *TrainingSession, _ *CurrentCandidate, _ *ValidatorIdentity, n *time.Time) {
			s.Validation.ValidUntil = *n
		}, Revalidate},
		{"changed policy", func(_ *TrainingSession, _ *CurrentCandidate, i *ValidatorIdentity, _ *time.Time) {
			i.PolicyDigest = "v2"
		}, Revalidate},
		{"expired training", func(s *TrainingSession, _ *CurrentCandidate, _ *ValidatorIdentity, n *time.Time) { s.ExpiresAt = *n }, RejectEvidence},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s, c, impl, now := validEvidenceFixture()
			tt.change(&s, &c, &impl, &now)
			got, err := DecideValidation(s, c, impl, now)
			if got != tt.want {
				t.Fatalf("decision=%s want=%s err=%v", got, tt.want, err)
			}
			if tt.want != RejectEvidence && err != nil {
				t.Fatal(err)
			}
			if tt.want == RejectEvidence && err == nil {
				t.Fatal("rejection must carry a reason")
			}
		})
	}
}

func TestRunningProcessDoesNotReleaseTraining(t *testing.T) {
	s := TrainingSession{Status: "active", RowVersion: 7}
	if err := CloseTrainingAfterStop(&s, "expired", false); err == nil {
		t.Fatal("accepted live process")
	}
	if s.Status != "active" || s.RowVersion != 7 {
		t.Fatal("released state before process stopped")
	}
	if err := CloseTrainingAfterStop(&s, "expired", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != "expired" || s.RowVersion != 8 {
		t.Fatal("terminal transition missing")
	}
}

func TestManagementJSONRejectsAmbiguity(t *testing.T) {
	for _, body := range []string{
		`{"name":"a","name":"b"}`, `{"name":"a","tenant_id":"other"}`,
		`{"name":"a"}{"name":"b"}`, `{"model_config":{"parameters":{"x":1,"x":2}}}`,
	} {
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var in CreateAgentRequest
		if err := DecodeManagementJSON(httptest.NewRecorder(), req, &in); err == nil {
			t.Fatalf("accepted ambiguous input %s", body)
		}
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"资料分析","model_config":{"parameters":{}}}`))
	var in CreateAgentRequest
	if err := DecodeManagementJSON(httptest.NewRecorder(), req, &in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "资料分析" || !json.Valid(in.Model.Parameters) {
		t.Fatal("valid request lost data")
	}
}
```

### 25.2 前端不得自动代选、重复建会话或重发训练

```javascript
import test from "node:test";
import assert from "node:assert/strict";
import { resolveChatTarget, sendSelectedAgentMessage, recoverTrainingView } from "./agent-draft.mjs";

test("missing target returns directory", () => {
  assert.deepEqual(resolveChatTarget(""), { kind: "directory" });
});

test("first send creates one bound conversation and next send reuses it", async () => {
  const state = { running: false, selectedAgentID: "a1", currentConversationID: "" };
  let creates = 0;
  const runs = [];
  const io = {
    requestJSON: async (_path, options) => {
      creates++;
      assert.deepEqual(JSON.parse(options.body), { agent_id: "a1" });
      return { id: "c1", agent_id: "a1" };
    },
    replaceURL: () => {},
    startRun: async (id, text) => { runs.push([id, text]); },
    showError: (message) => { throw new Error(message); },
  };
  await sendSelectedAgentMessage(state, "first", [], io);
  await sendSelectedAgentMessage(state, "second", [], io);
  assert.equal(creates, 1);
  assert.deepEqual(runs, [["c1", "first"], ["c1", "second"]]);
});

test("reconnect fetches snapshot and never posts a run", async () => {
  const requests = [];
  const signal = new AbortController().signal;
  const io = {
    requestJSON: async (path, options = {}) => {
      requests.push([path, options.method || "GET"]);
      return path.endsWith("/diff") ? { text: "" } : { conversation_id: "c1", operation: { state: "completed" } };
    },
    loadAuthorizedTrainingMessages: async () => [],
    renderSnapshot: () => {},
    wait: async () => { throw new Error("completed operation must not poll"); },
  };
  await recoverTrainingView("t1", io, signal);
  assert.equal(requests.length, 2);
  assert.ok(requests.every(([,method]) => method === "GET"));
});
```

### 25.3 必须追加的真实适配器验证

审核批准后的实现还必须覆盖：真实 MySQL 中同 Agent 两条非终态训练被唯一键拒绝、
跨租户复合外键拒绝、发布任一 SQL 失败完整回滚、重复发布只生成一行、并发模型发布
CAS 冲突、0008 down 冲突不删数据，以及 Git 文件准备/数据库提交/响应丢失的故障矩阵。

OS 测试要实际运行 exec/MCP，证明不能写正式文件或其他会话目录；检查目录字符串或
仅 mock Runner 不算通过。前端还要浏览器验证 Agent 选择、刷新、历史、归档、图片、
停止、训练重连与人工确认。登录接入使用真实宿主适配器单独验收，测试身份不算登录交付。

本文代码块可提取到临时目录做语法、类型及纯规则示例测试；这些只验证方案代码自身，
不等于仓库功能已实现。生产目录、迁移文件和业务路由在用户审核通过前不继续开发。

### 25.4 本次方案代码核验记录

2026-09-09 已将本文 13 个 Go 代码块提取为同一临时模块，使用仓库的依赖版本执行
`gofmt` 和 `go test ./...`，语法、类型检查及 §25.1 示例测试通过。将 §24 的三个
JavaScript 代码块组合为 `agent-draft.mjs`，执行 §25.2 的 `node --test`，3 项测试通过。

4 个 SQL 代码块是待审核的迁移/查询草案，本次未连接数据库执行；真实适配器、并发
事务、迁移与浏览器验证仍按 §25.3 在获批开发后完成。本记录仅证明文档示例的上述
检查通过，不代表平台功能已开发或端到端验收通过。
