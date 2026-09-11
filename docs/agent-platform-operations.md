# Agent 平台运行与维护

适用于单服务器进程、MySQL 8.4、持久化 Agent 数据目录。业务身份由宿主 `identity.Authenticator` 提供；`identity.mode=anonymous` 只提供明确固定租户的普通聊天，不开放管理功能。没有宿主登录集成时不能通过客户端传角色或租户来启用管理员。

## 运行入口

服务器继续使用 `go run ./cmd/server`。平台服务位于现有 application/service、infrastructure/driver、infrastructure/persistence 与 HTTP controller 分层；未替换 Fx、MySQL SDK、Gin SDK 或 pi 框架。

`agent_data_dir` 是持久资产根。启动持有其中的 `server.lock` 内核排他锁，第二个服务器必须失败。不能把同一目录用于多实例部署。先停止旧服务及其所有子进程，再启动新实例。

普通会话固定 Agent 身份，下一轮按 follow_latest 解析完整版本。训练使用独立候选。训练页左侧是当前候选的只读聊天测试，右侧是持久化的对话训练；试聊历史仅在当前页面内，既不进入正式聊天，也不进入训练作者的历史。试聊不提供业务写工具、exec 或 MCP，不把工具副作用当作测试结果。

训练/发布/恢复/取消共享 Agent 级操作准入；普通聊天只在自身准入时读取正式版本。模型 Runtime 与脚本校验共用容量限制；校验使用预留额度，不启动额外模型客户端。

## 迁移顺序

迁移前取得同批数据库和 Git 备份，停止写入并确认旧客户端退役安排。仓库 SQL 不由启动过程自动执行。

1. 备份后执行 `0007_agent_catalog.up.sql`，保留 nullable 会话归属。
2. 为明确租户初始化八个模板 Agent。先预检查，再显式写入：

   ```sh
   go run ./cmd/agent-migrate --mode bootstrap --tenant-id TENANT
   go run ./cmd/agent-migrate --mode bootstrap --tenant-id TENANT --dry-run=false
   ```

   bootstrap 依赖持久 bootstrap_key；失败重试复用原 Agent 草稿及准备记录，不新建另一个种子。
3. 确认存量会话归属。确实只有一个客户的历史，使用 `--confirmed-single-tenant-history --tenant-id TENANT`；否则提供宿主确认的映射文件。文件结构：

   ```json
   [{"conversation_id":"INTERNAL_CONVERSATION_ID","tenant_id":"TENANT"}]
   ```

   使用 Conversation 内部行 ID。未知字段、重复 ID、重复 JSON 键、未知会话及冲突归属均拒绝，不能从匿名 Cookie 推断组织。
4. 预检查所有行和 Git 版本，再显式回填：

   ```sh
   go run ./cmd/agent-migrate --mode backfill --mapping-file ownership.json
   go run ./cmd/agent-migrate --mode backfill --mapping-file ownership.json --dry-run=false
   go run ./cmd/agent-migrate --mode verify
   ```

   默认 dry-run。写入采用旧归属值 CAS；完整且一致的行跳过，不覆盖已有绑定。中途失败可在停写状态下重新预检查后重试。已存在的历史版本绑定保留，未绑定的旧 Profile 映射到本租户对应种子的初始完整版本。
5. 在停写窗口执行 `0008_agent_conversation_ownership.up.sql`，收紧 NOT NULL、复合外键和租户唯一键。客户端改用 agent_id；确认旧 profile_code 读写端退役后再执行 `0009_remove_legacy_profile.up.sql`。
6. 执行 `0010_agent_training_sessions.up.sql`，只新增训练会话表，并关联已有 Agent、版本、Conversation。

MySQL DDL 不能假定事务回滚。若中途退出，先 `SHOW CREATE TABLE` 核对已存在的列、索引和外键，再仅执行剩余语句，不整份盲目重跑。0008 down 首先尝试恢复旧 owner 唯一索引；跨租户重复 owner 会阻止回滚，不删除客户行。先撤销 0010 的依赖，再撤销 0008。0009 down 不能恢复新 Agent 从未拥有的旧 Profile 信息。

## 中断恢复

启动发现未完成的持久操作时拒绝接收请求，并给出训练 ID。运维必须确认旧服务与子进程全部停止，再使用：

```sh
go run ./cmd/server --recover-stopped-training
```

此选项是运维对旧写进程已停止的明确确认，不能用 TTL 或进程“不再响应”替代。接管前仍获取数据目录排他锁。

恢复以数据库 CandidateHead 为准重建 worktree，保留中断目录供诊断，保留全部 checkpoint refs。中断作者与校验不自动重跑，清空验证，操作标记 interrupted；过期/基线失效按当前数据库 UTC 时间和 Agent 指针结算。训练发布先查来源版本，已提交结果不回退；未提交且属于本操作的准备产物清理后才开放下一次修改。清理或一致性检查失败继续阻止写入。

发布与取消的结果以数据库为准。页面断线只查询状态、消息与差异，不重发 Run。运行、校验与发布均绑定剩余训练期限；停止完成前不释放活跃指针。数据库 SDK 的本地时区行为只在平台 UTC 日期读写边界转换，未改变旧 Conversation 的时区语义。

## 校验脚本

`agent_training.approved_interpreters` 默认空。带脚本的候选必须匹配服务端固定的解释器和依赖 SHA-256；不通过 PATH 选择候选解释器，不在运行时安装依赖。

当前 smoke 检查解释器启动与 `.sh` / `.py` 语法，不执行候选业务逻辑。macOS 使用 Seatbelt，Linux 使用 Bubblewrap；环境不继承服务端 Secret，禁止网络与候选写入，仅允许独立临时目录。原生沙箱不可用时失败，不降级到 Host。校验通过不代表回答质量通过；必须人工检查后发布。

## 同批备份与恢复演练

备份频率、保留期和异机位置由部署方配置。先协调停写并停止进程组，取得同批：

- MySQL 一致性快照（Agent、完整版本、训练状态和已有 Conversation/消息/计量）。
- 每个 bundle.git 的全部对象、refs/tags 和 checkpoint refs。
- 平台准备记录及实际持久化的附件。密钥由宿主独立备份；运行缓存无需备份。

恢复到隔离目录与隔离数据库，不在生产目录直接演练。先执行 `--mode verify`：核对全部版本的 Git 引用、文件摘要、联合配置摘要，并对各仓库执行 `git fsck --full --no-reflogs`。缺失/损坏对象必须报错，不能回退读取模板。按上述停止确认流程恢复中断训练，再通过应用物化聊天目录。保留备份批次、MySQL 版本、校验结果和缺失引用的 ID，日志不保存客户消息或密钥。

## 本地验证

```sh
go test ./... -count=1
node --test frontend/static/js/pages/*test.mjs
AGENT_NATIVE_SMOKE_TEST=1 RUN_WORKSPACE_SANDBOX_INTEGRATION=1 go test ./infrastructure/driver/agentsmoke ./pi/harness/sandbox -count=1
```

真实 MySQL 用例使用已有 `MYSQL_TEST_HOST/PORT/USER/PASSWORD`，通过 `go test -tags=integration ./infrastructure/persistence/agent -count=1` 运行，只创建并删除测试数据库。浏览器夹具额外设置 `AGENT_BROWSER_QA=1`；这是测试代码中的固定身份与可重复作者替身，不是生产认证或真实大模型质量验收。
