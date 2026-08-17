# 本地 Web Chat

`cmd/server` 提供一个基于 Gin、Go Template 和原生 JavaScript 的本地聊天页面。浏览器通过 HttpOnly Cookie 获得匿名身份；会话、用户消息、模型回复、工具调用结果和模型调用账本均由现有会话 Runner 写入 MySQL。Web 层不会绕过或修改 `pi`。

## 1. 初始化数据库

创建数据库后，按顺序执行迁移，不能跳过中间版本：

```bash
mysql -uroot -p harness < migrations/0001_conversation_persistence.up.sql
mysql -uroot -p harness < migrations/0002_model_invocation_observability.up.sql
mysql -uroot -p harness < migrations/0003_web_chat.up.sql
mysql -uroot -p harness < migrations/0004_agent_profiles.up.sql
```

`0003_web_chat` 为 `agent_conversations` 增加 `name`；`0004_agent_profiles` 增加非空 `profile_code` 和用户/Profile/更新时间联合索引。既有会话通过字段默认值迁移为 `general`。会话列表里的 `message_total` 在查询时从 `agent_messages` 聚合，不持久化冗余的最后一条消息。

## 2. 配置

复制 `config.example.json` 为 `config.json`，填入有效模型平台、API Key 和定价，然后启用会话持久化。使用本地 `harness` MySQL 时可配置为：

```json
{
  "agent": {
    "workspace_dir": "./workspaces/chat"
  },
  "http": {
    "host": "127.0.0.1",
    "port": "8080",
    "read_timeout": 30,
    "write_timeout": 0,
    "secure_cookies": false
  },
  "conversation": {
    "enabled": true,
    "history_message_limit": 100
  },
  "redis": {
    "addr": ["127.0.0.1:6379"],
    "password": "",
    "db": 0,
    "pool_size": 5
  },
  "mysql": {
    "host": "127.0.0.1",
    "port": 3306,
    "database": "harness",
    "user": "root",
    "password": "123456",
    "max_open": 100,
    "max_idle": 10,
    "conn_lifetime": 3600,
    "conn_timeout": 3,
    "log_level": 3,
    "slow_threshold": 500
  }
}
```

`write_timeout` 必须保持为 `0`，否则长时间运行的 SSE 连接可能被服务器提前截断。生产配置仍需包含一个被 `currentPlatform` 选中的有效平台，其 `baseURL`、`apiKey`、`model` 和 `pricing` 都不能为空。

Redis 是必需依赖。服务启动时会连接配置的 Redis 并执行 `PING`；连接失败时 Fx 启动失败，HTTP Server 不会开始监听。示例密码保持为空，生产密码只写入已忽略的本地配置或安全的环境覆盖，不要提交到仓库。

`agent.workspace_dir` 选择唯一聊天 Agent 的 Workspace，空值默认使用 `./workspaces/chat`。相对路径以服务进程的当前目录为基准；路径必须存在、必须是目录，并且不能回退到进程当前目录。Workspace 中必须包含非空、UTF-8 的普通文件 `AGENTS.md`。

## 3. 启动

开发时可在仓库根目录运行：

```bash
CONFIG_PATH=./config.json go run ./cmd/server
```

打开 <http://127.0.0.1:8080>。健康检查地址为 <http://127.0.0.1:8080/health>。

Go Templates、CSS 和 JavaScript 通过 `go:embed` 编译进 server 二进制。部署已构建二进制时不需要携带 `frontend/` 目录，也不需要 Node 服务或前端构建步骤。Web Agent 默认注册 `calculate`、`get_current_time`、`get_weather` 和受 Workspace 边界保护的 `read`。天气数据来自 Open-Meteo，无需 API Key；重名地点会返回候选并先请用户确认，不会默认选择第一个。

Web 不提供网页搜索、提醒、长期记忆、在线训练或 Coding 工具，也不会获得 `write`、`edit`、`apply_patch`、`exec` 或 `process`。知识库、课程、订单等行业能力仍应在业务 Fx 图中显式注册对应的真实 `ai.Tool`。

## 4. 定制聊天 Agent

默认 Workspace 结构如下：

```text
workspaces/chat/
├── AGENTS.md                    # 所有助手共享的身份和基础规则
├── skills/                      # 天气、写作、决策和学习讲解等通用流程
└── profiles/
    ├── catalog.yaml             # 版本化 Profile 公共元数据
    ├── general/AGENTS.md
    ├── writing/AGENTS.md
    └── writing/skills/...       # 只注入写作 Profile 的专属 Skills
```

默认 Workspace 提供 `weather-assistance`、`writing-assistance`、`decision-support` 和 `learning-explanation`。普通问候、闲聊、翻译和用户已提供文本的简单总结不需要 Skill。存在匹配 Skill 时，模型必须先通过 `read` 完整读取对应 `SKILL.md`。

Workspace 可以没有 Skill，仅凭有效 `AGENTS.md` 正常聊天。修改 Workspace Skill 会在下一次 Run 生效；新增或修改 Go Tool 需要重新构建并重启服务。

页面提供 `general`、`writing`、`learning`、`health`、`legal`、`automotive`、`workplace` 和 `parenting` 八个 Agent Profile。用户发送第一条消息时才创建会话并持久化 `profile_code`；创建后没有修改入口，Run API 也不接受 Profile 参数。每轮 Run 由服务端读取会话 Profile，叠加对应 AGENTS 和专属 Skill 目录，避免历史上下文中途混用。

Profile Catalog 随仓库版本发布，不提供后台、在线训练或热更新。新增 Profile 时必须同时提交 `catalog.yaml` 条目、`profiles/<code>/AGENTS.md` 和可选的 `profiles/<code>/skills/`，再通过 Catalog 测试后重新构建。停用 Profile 时先设置 `selectable: false`：新会话不能选择，但既有会话仍可显示和运行。只有完成数据库引用迁移后才能删除 code；运行中的既有会话引用不存在的 code 会返回内部配置错误，不会静默切换为 `general`。

`AGENTS.md + Skills + Documents + 业务 Tools` 只配置模型上下文和真实工具能力，不会训练或修改模型权重。Profile 隔离是行为边界，不是文件系统权限边界；第一期所有 Profile 共用现有工具，不提供在线训练或 Profile 管理页面。

## 5. Profile API

查询公开 Profile Catalog：

```http
GET /api/v1/agent-profiles
```

创建会话必须显式提交一个存在且 `selectable=true` 的 code：

```http
POST /api/v1/conversations
Content-Type: application/json

{"profile_code":"writing"}
```

会话列表可以与 keyword、cursor 和 limit 组合做 Profile 精确筛选：

```http
GET /api/v1/conversations?profile_code=writing
```

Profile API 只返回名称、说明、图标、欢迎语和推荐问题，不返回 AGENTS 正文或 Skill 路径。

## 6. 身份与安全边界

- 每个浏览器 Cookie Jar 对应一个匿名用户，Cookie 名为 `reagent_visitor`，有效期一年。
- 清理或丢失 Cookie 会获得新用户身份，原会话仍在数据库中，但新身份无法读取它们。
- 所有会话查询、重命名、删除、消息详情和运行取消均按 Cookie 用户做所有权校验。
- 状态变更接口拒绝跨源浏览器请求；服务启动时也拒绝非回环监听地址。
- 当前实现只支持本机使用。不要通过反向代理、端口转发或修改校验的方式暴露到局域网或公网；该部署方式不在安全支持范围内。

若本机使用 HTTPS 终止，请把 `secure_cookies` 改为 `true`。纯 HTTP 的 `127.0.0.1` 开发环境保持 `false`。
