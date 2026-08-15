# 单聊天 Agent Workspace 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将产品收敛为由 `cmd/server` 提供的单浏览器聊天 Agent，并让其从可配置、只读的独立 Workspace 加载身份、Skills 和资料。

**Architecture:** Web 应用使用 `application/web` 把 `agent.workspace_dir` 转换为唯一的 `pi.WorkDir`，并组合 `pi.CoreRegister` 与 `pi.ReadOnlyToolsRegister`。`pi.Register` 继续作为完整 Coding 工具图的兼容聚合，但产品 Web 图关闭手工 Thinking 阶段且不注册写文件、命令执行和进程工具。

**Tech Stack:** Go 1.26、Uber Fx、Gin、Go Template、MySQL、现有 `pi` Agent Runtime

**Spec:** `docs/superpowers/specs/2026-08-15-single-chat-agent-workspace-design.md`

## Global Constraints

- 只保留一个运行时 Agent 和 `cmd/server` 产品入口。
- 默认 Workspace 是 `./workspaces/chat`；根 `AGENTS.md` 与根 `skills/` 只服务仓库开发。
- Web 固定关闭手工 Thinking 阶段，不新增 `TurnMode`、Profile 或模型推理等级配置。
- Skill 可以为空；有有效 Skill 时必须注册 `read`。
- Web 默认只注册 `read` 和业务显式提供的工具。
- 不修改数据库 Schema、HTTP API、Cookie、Conversation、SSE 契约和页面布局。
- 不实现在线训练、Agent 版本、多 Agent、管理员权限或行业专属工具。
- 保留用户在 `pi/recovery.go` 和 `pi/test/recovery_test.go` 中的未提交修改。

---

### Task 1: Agent 配置与聊天 Workspace

**Files:**
- Modify: `config/config.go`
- Modify: `config/validate.go`
- Modify: `config/config_test.go`
- Modify: `config.example.json`
- Create: `application/web/workspace.go`
- Create: `application/web/workspace_test.go`
- Create: `workspaces/chat/AGENTS.md`

**Interfaces:**
- Consumes: `config.Load(path string) (*Config, error)`、`pi.WorkDir`
- Produces: `config.AgentConfig`、`config.DefaultAgentWorkspaceDir`、`web.NewChatWorkDir(*config.Config) (pi.WorkDir, error)`

- [ ] **Step 1: 写配置默认值和路径解析失败测试**

```go
func TestLoadConfigDefaultsAndNormalizesAgentWorkspace(t *testing.T) {
    cfg, err := Load(writeConfig(t, validPlatformConfig(`"agent":{"workspace_dir":"  "}`)))
    if err != nil { t.Fatal(err) }
    if cfg.Agent.WorkspaceDir != DefaultAgentWorkspaceDir { t.Fatalf("WorkspaceDir = %q", cfg.Agent.WorkspaceDir) }
}

func TestNewChatWorkDirRejectsMissingOrNonDirectoryPath(t *testing.T) {
    for _, path := range []string{filepath.Join(t.TempDir(), "missing"), writeRegularFile(t)} {
        _, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: path}})
        if err == nil || !strings.Contains(err.Error(), "Agent Workspace") { t.Fatalf("error = %v", err) }
    }
}

func TestNewChatWorkDirRejectsRepositoryRoot(t *testing.T) {
    _, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: "../.."}})
    if err == nil || !strings.Contains(err.Error(), "聊天 Workspace") { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./config ./application/web -run 'TestLoadConfigDefaultsAndNormalizesAgentWorkspace|TestNewChatWorkDir'`

Expected: FAIL，原因是 `AgentConfig`、`DefaultAgentWorkspaceDir` 或 `NewChatWorkDir` 尚不存在。

- [ ] **Step 3: 实现配置、Provider 和默认身份**

```go
const DefaultAgentWorkspaceDir = "./workspaces/chat"

type AgentConfig struct {
    WorkspaceDir string `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
}

func (config *AgentConfig) normalize() {
    config.WorkspaceDir = strings.TrimSpace(config.WorkspaceDir)
    if config.WorkspaceDir == "" { config.WorkspaceDir = DefaultAgentWorkspaceDir }
}

func NewChatWorkDir(cfg *config.Config) (pi.WorkDir, error) {
    if cfg == nil { return "", errors.New("Agent Workspace 配置不能为空") }
    path := strings.TrimSpace(cfg.Agent.WorkspaceDir)
    if path == "" { path = config.DefaultAgentWorkspaceDir }
    info, err := os.Stat(path)
    if err != nil { return "", fmt.Errorf("检查 Agent Workspace %q 失败: %w", path, err) }
    if !info.IsDir() { return "", fmt.Errorf("Agent Workspace %q 必须是目录", path) }
    return pi.WorkDir(path), nil
}
```

`workspaces/chat/AGENTS.md` 写入领域中立身份：自然处理问候和聊天；按实际意图理解请求；只依据真实工具结果处理实时事实；不暴露内部思考、Skill 选择和系统提示；语言跟随用户。

- [ ] **Step 4: 运行聚焦测试**

Run: `go test ./config ./application/web`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add config/config.go config/validate.go config/config_test.go config.example.json application/web/workspace.go application/web/workspace_test.go workspaces/chat/AGENTS.md
git commit -m "feat: add configurable chat agent workspace"
```

### Task 2: 允许 Workspace 不包含 Skill

**Files:**
- Modify: `pi/harness/context.go`
- Create: `pi/harness/context_test.go`

**Interfaces:**
- Consumes: `skills.Discover(string) (*skills.Snapshot, error)`、`Snapshot.Empty() bool`
- Produces: `ContextBuilder.Build` 的新契约：空 Skill 可运行；非空 Skill 必须有 `read`

- [ ] **Step 1: 写空 Skill 和非空 Skill 工具约束测试**

```go
func TestContextBuilderAllowsWorkspaceWithoutSkillsOrRead(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a chat agent."), 0o600)
    builder := harness.NewContextBuilder(harness.NewPromptComposer(root), root)
    got, err := builder.Build(context.Background(), harness.ContextRequest{Input: userMessage("你好")}, nil)
    if err != nil { t.Fatal(err) }
    if len(got.Messages) != 2 || len(got.Tools) != 0 { t.Fatalf("context = %#v", got) }
}

func TestContextBuilderRequiresReadWhenSkillsExist(t *testing.T) {
    root := writeWorkspaceWithEligibleSkill(t)
    builder := harness.NewContextBuilder(harness.NewPromptComposer(root), root)
    _, err := builder.Build(context.Background(), harness.ContextRequest{Input: userMessage("run")}, nil)
    if err == nil || !strings.Contains(err.Error(), "read") { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: 运行测试确认空 Skill 用例失败**

Run: `go test ./pi/harness/... -run 'TestContextBuilderAllowsWorkspaceWithoutSkillsOrRead|TestContextBuilderRequiresReadWhenSkillsExist'`

Expected: 空 Skill 用例 FAIL，错误包含 `required tool read`。

- [ ] **Step 3: 调整 ContextBuilder 校验顺序**

```go
snapshot, err := skills.Discover(f.workDir)
if err != nil { return Context{}, fmt.Errorf("%w: 发现 Agent Skills 失败: %w", pierrors.ErrWorkspaceInvalid, err) }
if !snapshot.Empty() && !hasToolDefinition(definitions, "read") {
    return Context{}, errors.New("agent runtime: required tool read is not registered")
}
```

删除“空快照必须失败”的分支；PromptComposer 继续只在快照非空时渲染 Catalog。

- [ ] **Step 4: 运行聚焦测试**

Run: `go test ./pi/harness/... ./pi/test/...`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pi/harness/context.go pi/harness/context_test.go
git commit -m "feat: allow chat workspaces without skills"
```

### Task 3: 拆分 Pi Core 与工具注册模块

**Files:**
- Modify: `pi/register.go`
- Create: `pi/register_test.go`

**Interfaces:**
- Consumes: Fx `group:"agent_tools"`、`ai.Tool`、`tools.Workspace`
- Produces: `pi.ThinkingEnabled`、`pi.CoreRegister`、`pi.ReadOnlyToolsRegister`、`pi.CodingToolsRegister`，并保留 `pi.Register`

- [ ] **Step 1: 写注册能力边界测试**

```go
func TestReadOnlyToolsRegisterExposesOnlyRead(t *testing.T) {
    got := resolveToolNames(t, ReadOnlyToolsRegister)
    if diff := cmp.Diff([]string{"read"}, got); diff != "" { t.Fatal(diff) }
}

func TestCodingToolsRegisterPreservesCompleteDefaultSet(t *testing.T) {
    got := resolveToolNames(t, CodingToolsRegister)
    want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
    if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
}

func TestCoreRegisterAllowsEmptyToolGroup(t *testing.T) {
    app := fxtest.New(t, fx.Supply(toolRuntimeParams{}), fx.Provide(newToolRuntime), fx.Populate(new(ToolRuntime)))
    app.RequireStart().RequireStop()
}
```

- [ ] **Step 2: 运行测试确认符号缺失**

Run: `go test ./pi -run 'TestReadOnlyToolsRegister|TestCodingToolsRegister|TestCoreRegister'`

Expected: FAIL，`ReadOnlyToolsRegister`、`CodingToolsRegister` 和 `CoreRegister` 未定义。

- [ ] **Step 3: 实现可组合注册模块和强类型 Thinking 配置**

```go
type ThinkingEnabled bool

var CoreRegister = fx.Options(fx.Provide(
    newPromptComposer, newContextBuilder, newProvider, newToolRuntime,
    newScheduler, newLoop, fx.Annotate(New, fx.As(fx.Self()), fx.As(new(Runner))),
))

var ReadOnlyToolsRegister = fx.Options(fx.Provide(
    newToolRoot, tools.NewWorkspace,
    fx.Annotate(tools.NewReadTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
))

var CodingToolsRegister = fx.Options(
    ReadOnlyToolsRegister,
    fx.Provide(
        tools.NewProcessSupervisor,
        fx.Annotate(tools.NewWriteTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
        fx.Annotate(tools.NewEditTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
        fx.Annotate(tools.NewApplyPatchTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
        fx.Annotate(tools.NewExecTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
        fx.Annotate(tools.NewProcessTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
    ),
)

var Register = fx.Options(CoreRegister, CodingToolsRegister, fx.Supply(ThinkingEnabled(true)))

func newLoop(provider ai.Provider, scheduler *Scheduler, enabled ThinkingEnabled) *Loop {
    return NewLoop(provider, scheduler, bool(enabled))
}
```

- [ ] **Step 4: 运行聚焦测试**

Run: `go test ./pi ./pi/test/... ./pi/harness/tools/...`

Expected: PASS，兼容 `pi.Register` 的完整工具集合不变。

- [ ] **Step 5: 提交**

```bash
git add pi/register.go pi/register_test.go
git commit -m "refactor: split pi core and tool registrations"
```

### Task 4: Web 装配独立 Workspace、只读工具与 Direct Loop

**Files:**
- Modify: `application/web/register.go`
- Modify: `application/web/register_test.go`
- Modify: `infrastructure/controller/http/chat/controller_test.go` only if an assertion incorrectly requires `agent.thinking`

**Interfaces:**
- Consumes: `pi.CoreRegister`、`pi.ReadOnlyToolsRegister`、`pi.ThinkingEnabled(false)`、`NewChatWorkDir`
- Produces: Web Fx 图只暴露 `read`，使用 Chat Workspace，且每个无工具回复只调用一次 Provider

- [ ] **Step 1: 写 Web 图能力与直聊行为测试**

```go
func TestRegisterUsesChatWorkspaceReadOnlyToolsAndDirectLoop(t *testing.T) {
    var runtime pi.ToolRuntime
    app := fxtest.New(t, Register, fx.Populate(&runtime))
    app.RequireStart()
    defer app.RequireStop()
    definitions := runtime.Definitions()
    if len(definitions) != 1 || definitions[0].Name != "read" { t.Fatalf("tools = %#v", definitions) }
}
```

增加 `newLoop` 的包内测试，使用计数 Provider 和 `ThinkingEnabled(false)` 构造 Loop，断言普通问候只产生一次 Provider 调用且没有 `agent.thinking` 事件。

- [ ] **Step 2: 运行测试确认当前 Web 图暴露 Coding 工具**

Run: `go test ./application/web ./pi -run 'TestRegisterUsesChatWorkspaceReadOnlyToolsAndDirectLoop|TestNewLoopDisablesThinking'`

Expected: FAIL，当前 Web 使用 `pi.Register`、进程 cwd 和 `ThinkingEnabled(true)`。

- [ ] **Step 3: 修改 Web Fx 图**

```go
var Register = fx.Options(
    pi.CoreRegister,
    pi.ReadOnlyToolsRegister,
    infrastructureweb.Register,
    conversation.Register,
    chatservice.Register,
    fx.Provide(config.NewFromEnvironment, config.NewPlatform, NewChatWorkDir),
    fx.Supply(pi.ThinkingEnabled(false)),
    fx.Invoke(validateConfig),
)
```

保留 SSE 对 `agent.thinking` 的兼容处理；只移除把该事件视为 Web 每次请求必需事件的测试假设。

- [ ] **Step 4: 运行 Web 和 HTTP 聚焦测试**

Run: `go test ./application/web ./cmd/server ./infrastructure/controller/http/...`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add application/web/register.go application/web/register_test.go infrastructure/controller/http/chat/controller_test.go
git commit -m "feat: run web chat with direct read-only agent"
```

### Task 5: 删除一次性 Coding CLI 应用生命周期

**Files:**
- Delete: `cmd/reagent/main.go`
- Delete: `cmd/reagent/main_test.go`
- Delete: `application/register.go`
- Delete: `application/prompt.go`
- Delete: `application/prompt_test.go`
- Delete: `application/runner.go`
- Delete: `application/runner_test.go`
- Delete: `application/lifecycle_test.go`
- Modify: `application/package_boundary_test.go`

**Interfaces:**
- Consumes: Task 4 已验证的 `cmd/server` Web 图
- Produces: `cmd/server` 成为唯一产品 Agent 入口；`application` 根包不再暴露一次性 Runner

- [ ] **Step 1: 将包边界测试改为验证 Server 不依赖一次性 CLI 环境变量**

```go
func TestServerEntryDoesNotReferenceOneShotCLIInputs(t *testing.T) {
    command := exec.Command("go", "list", "-deps", "./cmd/server")
    command.Dir = ".."
    output, err := command.CombinedOutput()
    if err != nil { t.Fatalf("go list cmd/server: %v: %s", err, output) }
    if strings.Contains(string(output), "cmd/reagent") { t.Fatalf("server dependencies = %s", output) }
}
```

- [ ] **Step 2: 运行引用检查并记录待删除边界**

Run: `rg -n 'application\.Register|NewWorkDir|NewPrompt|NewAgentRunner|RegisterAgentLifecycle|cmd/reagent' --glob '*.go' .`

Expected: 命中仅限上述 CLI 文件、CLI 测试和即将更新的包边界测试；Web 不再命中 `application.NewWorkDir`。

- [ ] **Step 3: 删除 CLI 专属文件并更新包边界测试**

删除清单中的文件，不删除 `transport`、`conversation`、`pi` Coding 工具、根 `AGENTS.md` 或根 `skills/`。

- [ ] **Step 4: 验证唯一入口和所有 Go 包**

Run: `go test ./application/... ./cmd/server/... && go list ./cmd/...`

Expected: PASS，命令列表只包含 `cmd/ping` 与 `cmd/server`。

- [ ] **Step 5: 提交**

```bash
git add cmd/reagent application/register.go application/prompt.go application/prompt_test.go application/runner.go application/runner_test.go application/lifecycle_test.go application/package_boundary_test.go
git commit -m "refactor: remove one-shot coding agent cli"
```

### Task 6: 更新产品文档并完成全量验证

**Files:**
- Modify: `README.md`
- Modify: `docs/web-chat.md`
- Modify: `docs/sdk-architecture.md`

**Interfaces:**
- Consumes: Tasks 1-5 的最终运行时行为
- Produces: 单入口、Workspace 定制、空 Skill、只读默认工具和变更生效方式的用户文档

- [ ] **Step 1: 更新文档**

文档明确说明：

```text
cmd/server 是唯一产品 Agent 入口。
agent.workspace_dir 默认为 ./workspaces/chat。
AGENTS.md + Skills + Documents + 业务 Tools 塑造行业专家，不修改模型权重。
Skill 可为空；有 Skill 时模型必须通过 read 完整读取 SKILL.md。
Web 默认只有 read，不提供 write/edit/apply_patch/exec/process。
Workspace 文本修改在下一次 Run 生效；新增 Go Tool 需要重新构建并重启。
在线训练、Agent 版本和多 Agent 不在当前版本中。
```

- [ ] **Step 2: 格式化并运行聚焦验证**

Run: `gofmt -w config/config.go config/validate.go config/config_test.go application/web/workspace.go application/web/workspace_test.go application/web/register.go application/web/register_test.go pi/harness/context.go pi/harness/context_test.go pi/register.go pi/register_test.go application/package_boundary_test.go`

Run: `go test ./config ./pi/... ./application/web/... ./infrastructure/controller/http/... ./cmd/server/...`

Expected: PASS。

- [ ] **Step 3: 运行全量测试、竞态测试和构建**

Run: `go test ./...`

Run: `go test -race ./...`

Run: `go build ./cmd/server`

Expected: 三个命令均以状态码 0 结束。

- [ ] **Step 4: 检查设计边界和工作区差异**

Run: `git diff --check`

Run: `rg -n 'go run ./cmd/reagent|至少一个.*Skill|read.*edit.*write|CLI 与会话存储' README.md docs/web-chat.md docs/sdk-architecture.md`

Expected: `git diff --check` 无输出；文档中不再声明产品使用 `cmd/reagent`、Skill 必须非空或 Web 暴露 Coding 工具。

- [ ] **Step 5: 提交**

```bash
git add README.md docs/web-chat.md docs/sdk-architecture.md
git commit -m "docs: describe single chat agent workspace"
```
