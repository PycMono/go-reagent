# Conversation-bound Agent Profile Design

## 目标

把当前单一通用聊天页面升级为面向 C 端的助手角色选择体验。用户在空白新对话中选择一个 Agent Profile，发送第一条消息时创建会话并持久化 `profile_code`；该会话后续始终使用同一个角色的身份规则和专属 Skills，不允许中途切换。

本设计采用“通用 Workspace 能力 + 会话 Profile 覆盖层”。Profile 是业务产品概念，不进入 `pi` 核心，也不为每个 Profile 构造独立模型 Provider、Runner 或工具 Registry。

## 产品模型

页面不使用“行业”作为用户入口。医疗、法律和汽车仍可作为专业领域助手，但写作、学习、职场和育儿也属于同一层级的助手角色。统一称为 Agent Profile。

第一期提供：

| Code | 页面名称 | 定位 |
| --- | --- | --- |
| `general` | 通用助手 | 日常问答、分析与决策 |
| `writing` | 写作助手 | 文案、改写、总结和平台内容 |
| `learning` | 学习导师 | 概念讲解、练习和学习计划 |
| `health` | 健康助手 | 健康知识和就医信息整理 |
| `legal` | 法律助手 | 法律常识和材料梳理 |
| `automotive` | 汽车顾问 | 选车、用车和维修知识 |
| `workplace` | 职场助手 | 汇报、邮件、方案和沟通 |
| `parenting` | 育儿助手 | 育儿知识和亲子内容 |

`general` 是既有会话的迁移值，也是新对话页面默认选中的 Profile。默认选中不代表后端允许省略字段；创建会话 API 仍要求显式提交 `profile_code`。

## 核心约束

1. Profile 在会话创建时确定，创建后不可修改；需要其他角色时新建会话。
2. Run API 不接收 Profile，由服务端读取会话中的 `profile_code`，防止客户端伪造切换。
3. 通用 AGENTS、通用 Skills 和现有真实工具对所有 Profile 可用。
4. Profile 只增加角色身份、领域边界和专属 Skill 目录；第一期不按 Profile 过滤真实工具。
5. Profile 定义随仓库版本发布，不增加数据库管理后台、在线编辑或热更新。
6. 不修改 `pi` 目录和公共运行契约；使用现有 `pi.ContextBlock` 注入业务上下文。
7. `profile_code` 是稳定机器标识，不存页面中文名，不允许自由文本。

## 方案比较

### 方案 A：业务层 Profile Context 注入（采用）

服务启动时加载不可变 Profile Catalog。每次 Run 根据会话 `profile_code` 构造 Profile AGENTS 和专属 Skill 清单，通过 `conversation.RunRequest.Context` 传给现有 Runner。

优点是保持一个 Provider、Runner 和工具 Registry，不修改 `pi`，同时精确控制每个会话看见的角色目录。代价是 Profile Skill 的可见性属于提示词和目录契约，不是文件系统安全隔离；Profile 内容本身不是秘密，因此第一期接受该边界。

### 方案 B：每个 Profile 一套 Fx Runner（不采用）

该方案能提供独立 WorkDir，但会重复构造 ContextBuilder、模型 Provider、工具 Registry 和生命周期依赖。Profile 数量增加时装配复杂度线性增长，也不适合仓库配置驱动的角色扩展。

### 方案 C：修改 pi 支持每轮 WorkDir（不采用）

该方案会改变 SDK 公共请求、ContextBuilder 和工具根目录契约，影响面远大于当前产品需求。只有未来 Profile 需要真正不同的文件系统权限或工具集合时再单独设计。

## Workspace 目录

```text
workspaces/chat/
├── AGENTS.md
├── skills/
│   ├── decision-support/SKILL.md
│   ├── learning-explanation/SKILL.md
│   ├── weather-assistance/SKILL.md
│   └── writing-assistance/SKILL.md
└── profiles/
    ├── catalog.yaml
    ├── general/
    │   └── AGENTS.md
    ├── writing/
    │   ├── AGENTS.md
    │   └── skills/content-writing/SKILL.md
    ├── learning/
    │   ├── AGENTS.md
    │   └── skills/guided-learning/SKILL.md
    ├── health/
    │   ├── AGENTS.md
    │   └── skills/health-information/SKILL.md
    ├── legal/
    │   ├── AGENTS.md
    │   └── skills/legal-information/SKILL.md
    ├── automotive/
    │   ├── AGENTS.md
    │   └── skills/vehicle-advice/SKILL.md
    ├── workplace/
    │   ├── AGENTS.md
    │   └── skills/workplace-communication/SKILL.md
    └── parenting/
        ├── AGENTS.md
        └── skills/parenting-guidance/SKILL.md
```

根 `AGENTS.md` 改为所有助手共享的基础纪律。`profiles/<code>/AGENTS.md` 只描述当前角色的身份、职责、领域限制和回答方式，不复制核心纪律。

Profile Skills 位于根 `skills/` 之外，因此现有 Harness 的通用发现不会把所有角色 Skill 一次性暴露给模型。Catalog Loader 针对选中的 Profile 发现并校验 Skills，再把全局 WorkDir 相对路径 `profiles/<code>/skills/.../SKILL.md` 注入本轮上下文。模型仍使用已注册的 `read` 工具读取匹配 Skill。

## Catalog 契约

`workspaces/chat/profiles/catalog.yaml` 示例：

```yaml
version: 1
default_profile: general
profiles:
  - code: general
    name: 通用助手
    description: 日常问答、分析与决策
    icon: message-circle
    order: 10
    selectable: true
    welcome: 今天想一起完成什么？
    starters:
      - title: 帮我分析一个问题
        prompt: 帮我分析这个问题：
```

第一期字段固定为：

- `code`：小写字母开头，只允许小写字母、数字和连字符，最长 64；
- `name`：页面名称，非空，最长 32 个 Unicode 字符；
- `description`：选择卡片的一行说明，非空，最长 120 个字符；
- `icon`：前端允许列表中的图标 key，不接受 SVG、HTML 或 URL；
- `order`：稳定排序值，同值再按 code 排序；
- `selectable`：是否允许创建新会话；
- `welcome`：空白页欢迎标题；
- `starters`：0 到 4 个推荐问题，每项包含 `title` 和可直接填入输入框的 `prompt`。

启动时必须验证：版本受支持、`default_profile` 存在且可选、code 唯一、字段长度合法、Profile 目录不越界、AGENTS 是非空 UTF-8 普通文件、Skill 目录可发现。配置或文件非法时 Web 服务启动失败。

Profile 下架使用 `selectable: false`。它不再出现在新建列表，但 Catalog 仍可解析它，既有会话仍能继续运行。已经被会话引用的 code 不得直接从仓库删除；删除前必须先提供数据迁移。

## 领域与应用边界

新增领域包：

```text
domain/entity/agentprofile/
domain/repository/agentprofile/
```

领域实体保存 Profile 公共元数据、运行指令和已校验 Skill 摘要。Catalog 接口至少提供：

```go
List() []agentprofile.Profile
Find(code string) (agentprofile.Profile, bool)
```

Catalog 是启动后只读的不可变快照，实现放在 `infrastructure/driver/agentprofile/`。Driver 使用安全的根目录读取和 YAML 解析，不信任路径字段；Profile 目录由 code 按约定推导，Catalog 不能指定任意宿主路径。

`application/service/chat.Service` 增加 Catalog 依赖，负责：

- 返回可选择 Profile 列表；
- 创建会话前验证 code 存在且 `selectable=true`；
- StartRun 时从已拥有的会话读取 code，解析运行 Profile；
- 生成高优先级 Profile AGENTS ContextBlock；
- 生成次高优先级 Profile Skill Catalog ContextBlock；
- 把两个块传给现有 `conversation.Runner`。

Profile Context 放在历史消息之前，因此每一轮都重新建立稳定角色边界。客户端不会传递 Profile 指令或 Skill 路径。

如果既有会话引用 `selectable=false` Profile，Run 继续允许。如果 code 在 Catalog 中完全缺失，Run 返回内部配置错误，不静默降级为 `general`，避免会话身份悄然改变。

## 数据库迁移

新增：

```text
migrations/0004_agent_profiles.up.sql
migrations/0004_agent_profiles.down.sql
```

Up：

```sql
ALTER TABLE agent_conversations
    ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general' AFTER name,
    ADD INDEX idx_agent_conversations_user_profile_updated
        (user_id, profile_code, updated_at, id);
```

Down 删除索引和字段。默认值把所有既有会话归入 `general`。数据库不建立 Profile 外键，因为权威 Catalog 在仓库文件中；也不使用 CHECK 枚举，以免每次新增 Profile 都修改表结构。

`Conversation` 实体和所有会话列表投影增加 `ProfileCode`。Persistence 的兼容边界把非 Web 调用方创建时的空 code 规范化为 `general`，但 Web 创建 API 保持严格必填。

Repository 不增加 UpdateProfile 方法。现有 PATCH 只允许修改 `name`，从接口能力上保持 Profile 不可变。

## HTTP API

### 查询 Profile

```http
GET /api/v1/agent-profiles
```

返回按 `order/code` 排序的全部已知 Profile 公共数据。已下架 Profile 仍需返回，供既有会话显示名称和图标；新建选择器只展示 `selectable=true` 的项：

```json
{
  "items": [
    {
      "code": "general",
      "name": "通用助手",
      "description": "日常问答、分析与决策",
      "icon": "message-circle",
      "selectable": true,
      "welcome": "今天想一起完成什么？",
      "starters": [{"title":"帮我分析一个问题","prompt":"帮我分析这个问题："}]
    }
  ],
  "default_profile": "general"
}
```

不返回 AGENTS 正文、Skill 路径或内部安全规则。

### 创建会话

```http
POST /api/v1/conversations
Content-Type: application/json

{"profile_code":"writing"}
```

缺少、未知或不可选择 code 返回现有非法参数错误。返回的 `ConversationVO` 增加 `profile_code`。

### 列表筛选

```http
GET /api/v1/conversations?profile_code=writing
```

`profile_code` 为空表示全部；非空 code 必须存在于 Catalog。筛选继续与 keyword、cursor 和 limit 组合，游标排序契约保持 `updated_at DESC, id DESC`。

### 运行与管理

`POST /api/v1/conversations/:id/runs` 请求体保持只有消息内容。重命名、删除、消息详情和取消接口保持不变。所有 Profile 相关会话操作继续使用 Cookie 用户做所有权校验。

## 前端交互

页面启动时并行加载 Profile Catalog 和会话列表。Profile 加载失败时禁止创建新会话并显示错误，不猜测本地默认数据；已经选中的既有会话仍可发送消息，因为 Run 使用服务端持久化的 Profile。

空白新对话状态：

1. 欢迎区域展示 8 个 Profile 选择项；
2. `general` 默认选中，用户可直接输入；
3. 选择项显示图标、名称和一行描述；
4. 选择变化时更新欢迎语和推荐问题；
5. 点击推荐问题只填充输入框，不自动发送；
6. 发送第一条消息时先调用创建接口并提交当前 code，再建立 SSE Run；
7. 创建成功后锁定 Profile。

已有会话状态：

- 顶部标题旁显示不可点击的 Profile 徽标；
- 会话列表项显示 Profile 图标或短名称；
- 搜索框下增加 Profile 筛选菜单，包含“全部助手”和可选择 Profiles；
- 重命名/删除入口不提供 Profile 修改；
- 点击“新对话”恢复空白状态和默认 Profile，不立即写数据库。

桌面 Profile 选择区使用 4 列、两行；移动端使用 2 列。控件维持稳定高度，长描述截断或换行，不允许卡片改变整体布局。图标 key 在前端映射到受控图标模板，不把配置内容作为 HTML 注入。

## 安全与内容边界

Profile AGENTS 和 Skills 必须把专业帮助限定为信息整理和一般知识，不冒充持证专业人士：

- Health：不得诊断、开药或替代医生；紧急风险建议及时联系当地急救或专业医疗机构；
- Legal：区分一般信息与具体法律意见，涉及案件时确认司法辖区并建议专业复核；
- Automotive：制动、电池、高压系统、举升等高风险操作建议专业检修；
- Parenting：避免把一般经验描述成医疗诊断或绝对育儿结论。

风险提示只在相关请求中出现，不在每条普通回复机械附加免责声明。

Profile 隔离是行为边界，不是权限边界。所有 Profile 第一阶段仍共享 `calculate`、`get_current_time`、`get_weather` 和 Workspace `read`。Profile 目录中不得存放秘密、凭据或某个用户的私有资料。

## 错误处理

- Catalog 结构、目录或文件非法：启动失败；
- 创建时 code 非法、未知或已下架：非法参数；
- 列表筛选 code 未知：非法参数；
- 既有会话 code 在 Catalog 中缺失：内部配置错误，不降级；
- Profile Context 构造失败：本轮 Run 失败且不写入用户消息；
- Profile 列表请求失败：前端禁止新会话的首次发送，不使用硬编码备用角色；既有会话发送保持可用。

错误响应和日志只记录稳定 code 与诊断类型，不输出完整 AGENTS、Skill 内容、用户消息或模型凭据。

## 测试设计

Catalog Driver：

- 解析并稳定排序 8 个 Profiles；
- 拒绝重复/非法 code、未知版本、无效 default、路径问题和非法 AGENTS；
- 发现 Profile Skills 并生成全局 WorkDir 相对路径；
- `selectable=false` 仍进入公开展示列表并可 Find，但不能用于新建会话；
- 返回数据是不可变副本。

Domain、Persistence 和迁移：

- ProfileCode 映射到 GORM 和列表投影；
- 既有数据迁移为 general；
- Profile 筛选与用户、keyword、cursor 正确组合；
- Create 的空 code 兼容规范化为 general；
- migration up/down 与真实 MySQL 结构一致。

Application 和 HTTP：

- 创建会话严格校验可选择 code；
- VO 和列表返回 `profile_code`；
- Run 只信任持久化 code，并注入正确 AGENTS 与 Skill 清单；
- Profile 不可选择后既有会话仍能运行；
- 缺失 Catalog code 不降级；
- Profile API 不泄露内部指令；
- Cookie 所有权、活动 Run 冲突、重命名和删除不回归。

Frontend：

- general 默认选中且空白页不提前创建会话；
- 选择、推荐问题、首次创建和 SSE 顺序正确；
- 已有会话显示锁定徽标；
- 筛选、搜索、分页和移动端侧栏可组合；
- Profile API 失败时发送被禁用；
- Playwright 在桌面和移动视口检查无重叠、无溢出和正确状态切换。

最终运行 `go test ./...`、`go test -race ./...`、server 构建、真实 MySQL migration 检查和浏览器聊天闭环验证。

## 实施顺序

1. Profile Catalog 领域契约、文件 Driver 和 8 套 Workspace 数据；
2. 数据库迁移、Conversation 实体与 Repository 映射；
3. Profile API、创建会话契约和列表筛选；
4. Run 时 Profile Context/Skill 注入；
5. 前端 Profile 选择、徽标、推荐问题和筛选；
6. 文档、全量测试、迁移和浏览器验证。

每一步使用测试先行，提交边界与以上顺序一致。实现必须保留工作树中与模型流式输出相关的现有改动，并在重叠文件中基于其当前行为增量开发。

## 非目标

第一期不包含：

- 在线新增、编辑或删除 Profile；
- Profile 数据库表或管理后台；
- Profile 热加载；
- Profile 专属模型或平台；
- Profile 工具白名单和行业外部 API；
- Profile 文件系统安全隔离；
- 修改 `pi` 公共 API、Harness 或 Tool Registry；
- 在同一会话中切换 Profile；
- 自动把历史会话分类到专业 Profile。

## 验收标准

1. 新对话默认 general，并能在首条消息前选择任一可用 Profile；
2. 首条消息创建会话并持久化正确 `profile_code`；
3. 已创建会话没有任何修改 Profile 的 API 或页面入口；
4. 每轮模型上下文包含通用规则、当前 Profile 规则和当前 Profile Skill 清单，不包含其他 Profile Skill 清单；
5. 既有会话迁移为 general，列表、消息、重命名、删除和 SSE 不回归；
6. Profile Catalog 非法时服务启动失败，code 缺失时不静默降级；
7. 健康、法律、汽车和育儿 Profile 遵守各自风险边界；
8. 桌面和移动页面完整可用，无布局重叠或文本溢出；
9. 不修改 `pi` 目录；
10. 不覆盖、不回退或误提交用户现有的模型流式输出改动。
