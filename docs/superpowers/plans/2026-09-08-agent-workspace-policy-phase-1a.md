# Agent Workspace Policy Phase 1A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `pi` 提供显式 Workspace 写策略及与真实运行一致的只读检查接口，使执行脚本不再隐含整个工作区可写。

**Architecture:** 公共 `pi.WorkspacePolicy` 由 SDK 构造时规范化一次，底层文件工具、OS 沙箱和 MCP 使用同一内部策略对象。受限文件操作以可写子目录的 `os.Root` 为边界，Seatbelt/Bubblewrap 只授予声明目录写权限。`InspectWorkspace` 复用 PromptComposer 和 Skill discovery，不承担 Git 或发布业务。

**Tech Stack:** Go 1.26、`os.Root`、Go testing、macOS Seatbelt、Linux Bubblewrap、现有 fx/configor 配置；不增加第三方依赖。

**Spec:** [Agent 自训练、评测与微调设计](../specs/2026-09-04-agent-self-training-design.md)，设计提交 `58793ca`；本计划仅实现 §10.2、§10.3、§17.2 和 §24.1 的 Phase 1A 部分。

**Status:** 待评审；复选框表示未来执行步骤，本文件未执行任何功能开发或功能测试。

## Global Constraints

- `pi` 不 import application/domain/infrastructure；`pi.Runner`、`pi.RunRequest`、`pi.RunResult` 不增加训练业务字段。
- `WorkspaceWriteRestricted` 和 `WorkspaceWriteAll` 分别为 `restricted`、`all`；`AllowWrite` 只决定是否注册文件写工具。
- `WritablePrefixes` 只允许规范化后的 WorkDir 相对目录；不能配置绝对路径、`..` 或符号链接逃逸。
- `AllowWrite` 或 `AllowExec` 为 true 时，`WriteMode` 不能为空；stdio MCP 同样是可执行进程，不能绕过显式策略要求。
- `AllowExec=true + WorkspaceWriteRestricted` 在无法隔离的 Host 后端必须失败，不允许静默降级。
- Chat/Evaluation 的行为文件只读；进程临时目录为本 WorkDir 内 `.tmp/`，允许写入时必须显式列入前缀。
- 工作区校验与 ContextBuilder 使用相同 AGENTS/Skill 读取、解析和诊断，现有 SKILL.md 上限仍为 256 KiB。
- 本期不创建数据库表、Agent/Release/TrainingSession、Git Bundle Store、会话 Runtime Manager、管理 API 或训练页面。
- §8.1 的会话目录物化、租户隔离与缓存键由 Phase 1B 实现；本期仅提供可复用的目录写限制与底层沙箱契约。
- 不在本期增加网络策略字段；保持现有网络行为。训练业务的默认禁网及副作用工具替身由后续阶段落实。
- 原工作区未提交的 loopdetect/listener 改动不进入本分支、不自动提交或 stash；经评审后在隔离 worktree 执行。

## 文件与接口落位

| 文件 | 职责 / 任务 |
| --- | --- |
| `pi/internal/workspacepolicy/policy.go`、`policy_test.go`（新增） | 无上层依赖的策略规范化、路径边界与不可变内部对象，Task 1 |
| `pi/workspace_policy.go`、`workspace_policy_test.go`（新增） | 公共类型/常量和 SDK 入口行为，Tasks 1、6 |
| `pi/harness/tools/filesystem.go`、`filesystem_test.go` | 可写子根句柄及所有变更方法统一拦截，Task 2 |
| `pi/harness/sandbox/write_policy.go`、`write_policy_test.go`（新增） | 内部策略到后端规则、禁用进程的 Runner，Task 3 |
| `pi/harness/sandbox/runner.go`、`runner_test.go` | 生效写策略、TMPDIR 契约，Tasks 3、4 |
| `pi/harness/sandbox/platform.go`、`host.go`、`host_test.go` | 显式策略选择与 Host fail closed，Task 3 |
| `pi/harness/sandbox/seatbelt.go`、`bubblewrap.go`、`sandbox_test.go` | 只读根 + 可写子目录，Task 4 |
| `pi/harness/sandbox/write_policy_integration_test.go`（新增） | 真正执行两个 OS 后端，Task 4 |
| `pi/harness/tools/process_supervisor.go`、`process_supervisor_test.go` | 消除 Linux `/tmp` 与 MCP 分叉，Task 5 |
| `pi/mcp/sandbox.go`、`server.go`、`server_test.go`、`sandbox_test.go`（最后一个新增） | stdio 继承同一 Runner 及 TMPDIR，Task 5 |
| `pi/agent.go` | 显式策略 fail fast、统一构造与资源清理，Task 6 |
| `pi/harness/inspect.go`、`inspect_test.go`（新增） | 只读 WorkspaceReport，Task 7 |
| `pi/harness/context.go`、`context_test.go`、`prompt.go` | 检查与真实 Context 共用加载路径，Task 7 |
| `config/config.go`、`validate.go`、`server_options.go`、`workspace_policy_test.go`（新增） | 配置声明和纯翻译，Task 8 |
| `config.example.json` | 显式生产 restricted 示例，Task 8 |
| `cmd/server/app.go`、`app_test.go`（新增） | 过渡期单 Workspace 显式策略，Task 8 |
| `cmd/cli/main.go`、`main_test.go` | 保留显式 Coding all 语义，Task 6 |

与 §17.2 相比增加的 internal 包用于避免 `pi → harness → pi` 循环；ProcessSupervisor/CLI/Prompt 文件是已核验的现有调用链依赖，不引入后续业务功能。仓库只有根 `go.mod`，没有 `pi/go.mod`；所有命令从仓库根运行。

### Task 0: 固定执行基线并隔离已有改动

**Files:** 不修改实现文件。设计已单独提交；本计划评审后再执行下列步骤。

- [ ] 查看 `git status --short`、`git diff --cached --name-only` 和 `git log -3 --oneline`，确认设计和计划已提交，原工作区的无关改动没有被暂存。
- [ ] 使用 using-git-worktrees 技能建立隔离工作区；路径或分支已存在时先检查归属，禁止覆盖或删除：

```sh
git worktree add -b feat/phase1a-workspace-policy /tmp/go-reagent-phase1a HEAD
```

- [ ] 后续命令工作目录固定为 `/tmp/go-reagent-phase1a`；确认 `git status --short` 为空。执行一次基线检查，记录退出码及失败测试名：

```sh
GOCACHE=/tmp/go-reagent-phase1a-cache go test ./...
```

- [ ] 基线失败时先区分既有失败、缺少 OS 沙箱与实际回归；不搬入原工作区尚未提交的修复。记录平台限制后推进能独立验证的任务，但最终验收不得把跳过算通过。

### Task 1: 定义共享写策略，封闭路径规范化

**Files:** 新增 `pi/internal/workspacepolicy/policy.go`、`policy_test.go`、`pi/workspace_policy.go`。

**Interfaces:** internal 包不 import `pi`、tools 或 sandbox。对后续任务提供：

```go
type Mode string
const (
    Restricted Mode = "restricted"
    All Mode = "all"
)
type Policy struct {
    WriteMode Mode
    WritablePrefixes []string
}
func Normalize(root string, policy Policy) (*Normalized, error)
func (n *Normalized) Root() string
func (n *Normalized) Mode() Mode
func (n *Normalized) Prefixes() []string
func (n *Normalized) MatchWrite(path string) (prefix, relative string, err error)
```

`Normalized` 字段私有，Root 是 canonical 绝对目录，Prefixes 返回副本。公共 `pi` 类型采用别名，使底层不反向 import SDK 根：

```go
type WorkspaceWriteMode = workspacepolicy.Mode
type WorkspacePolicy = workspacepolicy.Policy
const (
    WorkspaceWriteRestricted = workspacepolicy.Restricted
    WorkspaceWriteAll = workspacepolicy.All
)
// 供 config 复用校验；只读，不创建目录，避免复制另一套规则。
func ValidateWorkspacePolicy(root string, policy WorkspacePolicy) error {
    _, err := workspacepolicy.Normalize(root, policy)
    return err
}
```

- [ ] 在 internal 包写失败测试，覆盖绝对路径、任意 `..` 分量、空前缀、`.`、NUL、反斜杠/volume、兄弟前缀、符号链接、非目录 root 和空 mode。起始测试如下（导入 `testing`）：

```go
func TestNormalizeRejectsUnsafePrefixes(t *testing.T) {
    for _, p := range []string{"", ".", "..", "../out", "a/../scratch", "/tmp", "C:/tmp", `a\b`, "a\x00b"} {
        t.Run(p, func(t *testing.T) {
            _, err := Normalize(t.TempDir(), Policy{WriteMode: Restricted, WritablePrefixes: []string{p}})
            if err == nil { t.Fatalf("accepted %q", p) }
        })
    }
}
func TestMatchWriteUsesPathComponents(t *testing.T) {
    n, err := Normalize(t.TempDir(), Policy{WriteMode: Restricted, WritablePrefixes: []string{"scratch"}})
    if err != nil { t.Fatal(err) }
    if _, _, err := n.MatchWrite("scratch-evil/file"); err == nil { t.Fatal("sibling matched") }
    prefix, relative, err := n.MatchWrite("scratch/result.txt")
    if err != nil || prefix != "scratch" || relative != "result.txt" {
        t.Fatalf("match: %q %q %v", prefix, relative, err)
    }
}
```

- [ ] 运行 RED：`go test ./pi/internal/workspacepolicy -run 'TestNormalize|TestMatchWrite' -count=1`，确认失败来自尚未提供的策略接口。
- [ ] 实现 Normalize：先验证 mode，再验证原始分量，再规范化；解析 root 真实路径，遍历已存在前缀祖先，拒绝 symlink/特殊文件。不存在的目录允许声明但不在 Normalize 中创建；重复前缀去重排序，父前缀已覆盖时去掉其子项。all 不允许附带非空 prefixes，restricted 的空列表表示无可写目录。MatchWrite 使用分量边界，前缀本身不能被 Remove 或替换。
- [ ] 增加输入/返回切片修改不会改变 n、相同输入输出稳定、根内 symlink 也不作为 writable prefix 的断言；复用 `os.Symlink` fixture，平台无权限创建链接时明确 skip 该单项。
- [ ] 运行 GREEN：`go test ./pi/internal/workspacepolicy ./pi -count=1`。
- [ ] 提交：`git add pi/internal/workspacepolicy pi/workspace_policy.go`，`git commit -m 'feat(pi): define explicit workspace write policy'`。

### Task 2: 文件工具按可写子根执行变更

**Files:** 修改 `pi/harness/tools/filesystem.go`、`filesystem_test.go`；沿用现有 `special_file_test.go` 的非普通文件验证。

**Interfaces:** 保留 `NewWorkspace(Root)` 作为低层 Coding/all 兼容入口；增加 `NewWorkspaceWithPolicy(Root, *workspacepolicy.Normalized) (*Workspace, error)`。SDK 在 Task 6 必须调用新入口，nil 或 root 不匹配时返回错误。内部增加 `writeTarget(path string) (*os.Root, string, error)`。

- [ ] 写行为失败测试（导入 `errors`、`io/fs`、`os`、`path/filepath`、`testing` 和 internal workspacepolicy 包）：

```go
func TestWorkspaceRestrictedCannotTruncateBundle(t *testing.T) {
    root := t.TempDir()
    if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original"), 0600); err != nil { t.Fatal(err) }
    n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
        WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{"scratch"},
    })
    if err != nil { t.Fatal(err) }
    w, err := NewWorkspaceWithPolicy(Root(root), n)
    if err != nil { t.Fatal(err) }
    defer w.Close()
    f, err := w.OpenFile("AGENTS.md", os.O_WRONLY|os.O_TRUNC, 0600)
    if f != nil { f.Close() }
    if !errors.Is(err, fs.ErrPermission) { t.Fatalf("want permission error, got %v", err) }
    b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
    if err != nil || string(b) != "original" { t.Fatalf("bundle changed: %q %v", b, err) }
    if err := w.MkdirAll("scratch/nested", 0700); err != nil { t.Fatal(err) }
    f, err = w.OpenFile("scratch/nested/result", os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil { t.Fatal(err) }
    if err := f.Close(); err != nil { t.Fatal(err) }
}
```

- [ ] 运行 RED：`go test ./pi/harness/tools -run TestWorkspaceRestricted -count=1`。
- [ ] 构造期由可信平台创建声明的目录，再为每个非重叠可写前缀打开独立 `os.Root`。`writeTarget` 先做词法匹配，再返回该子根和相对路径。禁止检查后仍用整个 Workspace root 打开写句柄：`scratch/link → ../AGENTS.md` 必须由子根在系统调用时拦截。
- [ ] 接入全部变更方法；OpenFile 的变更判定必须覆盖所有写标志：

```go
mutating := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
if mutating {
    target, relative, err := w.writeTarget(path)
    if err != nil { return nil, err }
    return target.OpenFile(relative, flag, perm)
}
```

MkdirAll、Remove 同样走 writeTarget；拒绝对前缀自身的删除/改名、根外链接和非普通文件，保持现有特殊文件保护。读操作仍使用整个 Workspace 的守卫 root。Close 逆序关闭子根再关闭总根，失败构造立即关闭已打开句柄。
- [ ] 加入各 OpenFile flag、MkdirAll、Remove、`scratch-evil`、链接指向 Bundle、链接切换和 all 模式回归用例。`write/edit/apply_patch` 只复用 Workspace，不各写一套路径规则；验证拒绝前后 Bundle 字节与目录清单一致。
- [ ] 运行 GREEN：`go test ./pi/harness/tools -count=1`，再运行 `go test -race ./pi/harness/tools -run 'Workspace|Special' -count=1`。
- [ ] 提交：`git add pi/harness/tools/filesystem.go pi/harness/tools/filesystem_test.go`，`git commit -m 'feat(pi): confine file mutations to writable roots'`。

### Task 3: 传递后端策略并阻止 Host 绕过

**Files:** 新增 `pi/harness/sandbox/write_policy.go`、`write_policy_test.go`；修改 `runner.go`、`platform.go`、`host.go`、`host_test.go`。

**Interfaces:**

```go
func NewRunnerWithPolicy(root string, n *workspacepolicy.Normalized, needsProcess bool) (Runner, error)
func newRunnerForOSWithPolicy(goos, root string, lookup func(string) (string, error), n *workspacepolicy.Normalized, needsProcess bool) (Runner, error)
// 加到既有 Policy；返回切片须复制。
type Policy struct {
    Backend string
    Network string
    WriteMode string
    WritablePrefixes []string
    TmpDir string
}
var ErrWritePolicyUnsupported = errors.New("workspace_write_policy_unsupported")
var ErrProcessDisabled = errors.New("process_disabled")
```

旧 NewRunner/NewHostRunner 仅保留低层明确 all 兼容路径；新的 SDK 流程不走旧入口。`needsProcess=false` 返回 BuildShell/BuildArgv 均报 ErrProcessDisabled 的私有 Runner，不寻找 bwrap/sandbox-exec，也不创建 `.tmp`。无需进程时 Windows 的文件工具仍可以使用 Task 2 的 restricted 子根。

- [ ] 写 RED 测试（使用 `errors`、`testing` 和 internal 包），验证平台选择发生在任何启动动作前：

```go
func TestRestrictedHostFailsClosed(t *testing.T) {
    root := t.TempDir()
    n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
        WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{".tmp"},
    })
    if err != nil { t.Fatal(err) }
    lookup := func(string) (string, error) { t.Fatal("host must not look up binaries"); return "", nil }
    _, err = newRunnerForOSWithPolicy("windows", root, lookup, n, true)
    if !errors.Is(err, ErrWritePolicyUnsupported) { t.Fatalf("got %v", err) }
    r, err := newRunnerForOSWithPolicy("windows", root, lookup, n, false)
    if err != nil { t.Fatal(err) }
    if _, err := r.BuildArgv([]string{"ignored"}, CommandSpec{}); !errors.Is(err, ErrProcessDisabled) {
        t.Fatalf("disabled runner executed: %v", err)
    }
}
```

- [ ] 运行 `go test ./pi/harness/sandbox -run 'RestrictedHost|ProcessDisabled' -count=1`，确认 RED。
- [ ] 实现新 selector；needsProcess=true 且 restricted 时 `.tmp` 必须被明确允许，否则构造失败，禁止偷偷追加可写目录。Host 无法限制时直接返回 ErrWritePolicyUnsupported。平台支持但后端缺失仍失败；已创建 Runner 的 Policy 携带 n 的不可变副本。
- [ ] 增加 `TestProcessDisabledDoesNotCreateTmp`、`TestRestrictedProcessRequiresTmpPrefix`，断言失败不创建文件、不执行探针；all + Host 保持既有 shell/env 行为。Stage 3 中 macOS/Linux 受限入口在 Task 4 支持前明确拒绝，不返回假装受限的旧后端。
- [ ] 运行 GREEN：`go test ./pi/harness/sandbox -count=1`。
- [ ] 提交：`git add pi/harness/sandbox/write_policy.go pi/harness/sandbox/write_policy_test.go pi/harness/sandbox/runner.go pi/harness/sandbox/platform.go pi/harness/sandbox/host.go pi/harness/sandbox/host_test.go`，`git commit -m 'feat(pi): reject unsupported restricted process execution'`。

### Task 4: Seatbelt/Bubblewrap 实际限制写入

**Files:** 修改 `seatbelt.go`、`bubblewrap.go`、`platform.go`、`write_policy.go`、`sandbox_test.go`、`runner_test.go`；新增 `write_policy_integration_test.go`，均位于 `pi/harness/sandbox/`。

**Interfaces:**

```go
func NewSeatbeltRunnerWithPolicy(binary, root string, n *workspacepolicy.Normalized) (*SeatbeltRunner, error)
func NewBubblewrapRunnerWithPolicy(binary, root string, n *workspacepolicy.Normalized) (*BubblewrapRunner, error)
func prepareWritableDirectories(n *workspacepolicy.Normalized) error
```

旧构造器保留原 all 行为和原 TMPDIR，避免本提交破坏未迁移调用方。新构造器的 Policy.TmpDir 在两个 OS 上都等于 canonical Root + `/.tmp`，新 selector 使用新构造器。prepareWritableDirectories 只创建显式批准目录，拒绝既有 symlink/特殊文件，`.tmp` 权限 0700；不改根目录或 Bundle 文件权限。

- [ ] 在 sandbox_test.go 增加新构造器的 argv/profile RED 测试。Seatbelt restricted profile 不得含全根 `(allow file-write* (subpath (param "WORKSPACE_ROOT")))`；每个前缀通过 `-D WRITE_ROOT_0=...` 等参数注入，不能把用户路径直接拼接进 Scheme。Bwrap restricted 必须先 `--ro-bind root root`，再逐个 `--bind prefix prefix`；all 必须保持整根 writable。
- [ ] 运行 `go test ./pi/harness/sandbox -run 'RestrictedSeatbelt|RestrictedBubblewrap' -count=1`，确认 RED。
- [ ] 实现 Seatbelt 的写规则生成；保留当前系统只读目录、环境白名单、进程与网络行为。根只读必须同时禁止 AGENTS 的 create/truncate/rename/unlink，`.tmp` 和 scratch 的替换不能改变写边界。保留 `/dev/null` 作为系统设备例外。
- [ ] 实现 Bwrap：保留隔离 namespace、最小 wrapper env、`--clearenv`、系统目录只读挂载；将进程 `/tmp` 绑定到本工作区 `.tmp` 作为同一临时存储的别名，TMPDIR 仍为根内 `.tmp`。挂载顺序先建立 `/tmp` 别名，再挂 Workspace 只读根，最后挂可写前缀，防止后面的父挂载覆盖子挂载。必须实测 WorkDir 自身位于 `/tmp` 下的情况；任何不能表达的布局在构造/探针阶段失败，不能退回全根可写。
- [ ] ProbeSeatbelt/ProbeBubblewrap 从 `runner.Policy().TmpDir` 取临时目录；探针使用完整最终规则。补充 native 测试（导入 `os`、`path/filepath`、`runtime`、`testing` 和 internal 包）：

```go
func TestNativeRestrictedWrites(t *testing.T) {
    if runtime.GOOS != "darwin" && runtime.GOOS != "linux" { t.Skip("requires native OS sandbox") }
    if os.Getenv("RUN_WORKSPACE_SANDBOX_INTEGRATION") != "1" { t.Skip("native suite not requested") }
    root := t.TempDir()
    if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original"), 0600); err != nil { t.Fatal(err) }
    n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
        WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{"scratch", ".tmp"},
    })
    if err != nil { t.Fatal(err) }
    r, err := NewRunnerWithPolicy(root, n, true)
    if err != nil { t.Fatal(err) }
    spec := CommandSpec{WorkDir: n.Root(), PayloadEnv: Env(n.Root(), r.Policy().TmpDir)}
    cmd, err := r.BuildShell("printf ok > scratch/result; printf tmp > \"$TMPDIR/result\"", spec)
    if err != nil { t.Fatal(err) }
    if out, err := cmd.CombinedOutput(); err != nil { t.Fatalf("allowed write: %v %s", err, out) }
    for path, want := range map[string]string{"scratch/result": "ok", ".tmp/result": "tmp"} {
        got, err := os.ReadFile(filepath.Join(root, path))
        if err != nil || string(got) != want { t.Fatalf("allowed output %s: %q %v", path, got, err) }
    }
    for _, attack := range []string{
        "printf changed > AGENTS.md", "rm AGENTS.md", "mv AGENTS.md scratch/moved",
        "mkdir scratch-evil", "printf changed > scratch/link",
    } {
        if attack == "printf changed > scratch/link" {
            if err := os.Symlink("../AGENTS.md", filepath.Join(root, "scratch/link")); err != nil { t.Fatal(err) }
        }
        cmd, err := r.BuildShell(attack, spec)
        if err != nil { t.Fatal(err) }
        if out, err := cmd.CombinedOutput(); err == nil { t.Fatalf("accepted %q: %s", attack, out) }
        b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
        if err != nil || string(b) != "original" { t.Fatalf("Bundle changed: %q %v", b, err) }
    }
}
```

- [ ] 增加 BuildArgv 直执同样拒绝 Bundle 写入、其他 WorkDir 文件不可读、hardlink 到只读 Bundle 不能用来写穿的用例；创建 root/scratch 时含空格的路径也要通过。这些是行为断言，不能只检查命令字符串。
- [ ] 运行 GREEN：`go test ./pi/harness/sandbox -count=1`。分别在原生 macOS 和 Linux 运行：

```sh
RUN_WORKSPACE_SANDBOX_INTEGRATION=1 go test ./pi/harness/sandbox -run Native -count=1 -v
```

显式启用 native suite 后，缺 sandbox-exec/bwrap、内核禁止 user namespace、探针失败都必须 FAIL，不能 Skip。单台机器只覆盖其中一个 OS 时把另一平台验收留未勾选。
- [ ] 提交：`git add pi/harness/sandbox`，`git commit -m 'feat(pi): enforce restricted writes in OS sandboxes'`。

### Task 5: exec 与 MCP 使用同一临时目录和 Runner

**Files:** 修改 `pi/harness/tools/process_supervisor.go`、`process_supervisor_test.go`、`pi/mcp/sandbox.go`、`server.go`、`server_test.go`；新增 `pi/mcp/sandbox_test.go`。

**Interfaces:** 新 Runner 通过 Policy.TmpDir 发布确定的进程临时目录；保持 `pimcp.New([]ServerOptions, tools.Root, sandbox.Runner)` 和 `sandboxBuildCommand` 签名。新增 `sandbox.PayloadTmpDir(policy Policy, root string) (string, error)` 到 runner.go：优先用 Policy.TmpDir，仅旧 all Runner 缺字段时沿用旧后端 TMPDIR 约定，restricted 缺 TmpDir 直接拒绝。

- [ ] MCP 包添加失败测试，fixture 使用新 restricted Runner，直接调用真实 sandboxBuildCommand 并执行它返回的 *exec.Cmd；不需要远程 MCP 或模型 API。代码测试入口：

```go
func TestNativeMCPCommandCannotModifyBundle(t *testing.T) {
    if runtime.GOOS != "darwin" && runtime.GOOS != "linux" { t.Skip("requires OS sandbox") }
    if os.Getenv("RUN_WORKSPACE_SANDBOX_INTEGRATION") != "1" { t.Skip("native suite not requested") }
    root := t.TempDir()
    if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original"), 0600); err != nil { t.Fatal(err) }
    n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
        WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{".tmp", "scratch"},
    })
    if err != nil { t.Fatal(err) }
    r, err := sandbox.NewRunnerWithPolicy(root, n, true)
    if err != nil { t.Fatal(err) }
    build, err := sandboxBuildCommand(r, root, ServerOptions{
        Name: "write-probe", Transport: "stdio", Command: "/bin/sh",
        Args: []string{"-c", "printf changed > AGENTS.md"},
    })
    if err != nil { t.Fatal(err) }
    cmd, err := build()
    if err != nil { t.Fatal(err) }
    if out, err := cmd.CombinedOutput(); err == nil { t.Fatalf("MCP wrote Bundle: %s", out) }
    b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
    if err != nil || string(b) != "original" { t.Fatalf("changed: %q %v", b, err) }
}
```

导入 `os`、`path/filepath`、`runtime`、`testing`、sandbox 和 internal workspacepolicy 包。
- [ ] 为 ProcessSupervisor.payloadEnv 与 sandboxBuildCommand 补离线 RED 断言：两个新后端 TMPDIR 均为 n.Root()+`/.tmp`；禁止覆盖 HOME/PATH/TMPDIR、越界 CWD、未知后端。使用既有 process_supervisor_test.go 的 fake Runner，Policy 明确填写 WriteMode/TmpDir。

在 sandbox/runner_test.go 添加可独立失败的共用契约用例（使用现有 testing，补 filepath）：

```go
func TestPayloadTmpDirUsesDeclaredPolicy(t *testing.T) {
    root := t.TempDir()
    for _, backend := range []string{"seatbelt", "bubblewrap"} {
        want := filepath.Join(root, ".tmp")
        got, err := PayloadTmpDir(Policy{Backend: backend, WriteMode: "restricted", TmpDir: want}, root)
        if err != nil || got != want { t.Fatalf("%s: %q %v", backend, got, err) }
        if _, err := PayloadTmpDir(Policy{Backend: backend, WriteMode: "restricted"}, root); err == nil {
            t.Fatalf("%s accepted missing restricted TmpDir", backend)
        }
    }
}
```

- [ ] 运行 `go test ./pi/harness/tools ./pi/mcp ./pi/harness/sandbox -run 'PayloadEnv|Sandbox|TmpDir|Contract' -count=1`，确认真正覆盖新增失败用例。
- [ ] 两处删除自行 switch backend 的新流程，统一调用 `sandbox.PayloadTmpDir`。在 MCP Host 分支检查 `runner.Policy().WriteMode`，restricted 或 disabled-process 不能绕过 Runner 直接启动 StdioTransport；HTTP transport 行为不变。
- [ ] 运行 GREEN：`go test ./pi/harness/tools ./pi/mcp ./pi/harness/sandbox -count=1`；原生 OS 再运行 `RUN_WORKSPACE_SANDBOX_INTEGRATION=1 go test ./pi/mcp -run Native -count=1 -v`。
- [ ] 提交：`git add pi/harness/tools/process_supervisor.go pi/harness/tools/process_supervisor_test.go pi/mcp/sandbox.go pi/mcp/server.go pi/mcp/server_test.go pi/mcp/sandbox_test.go pi/harness/sandbox/runner.go pi/harness/sandbox/runner_test.go`，`git commit -m 'fix(pi): share process write policy with MCP stdio'`。

### Task 6: SDK fail fast 装配及现有 CLI 调用迁移

**Files:** 修改 `pi/agent.go`、`cmd/cli/main.go`、`cmd/cli/main_test.go`、`cmd/server/app.go`；新增 `pi/workspace_policy_test.go`。

**Interfaces:** `pi.Options` 增加 `WorkspacePolicy WorkspacePolicy`。根包私有 `normalizeWorkspaceOptions(opts Options) (*workspacepolicy.Normalized, bool, error)` 返回规范化策略和 needsProcess；不增加新的公共 Normalized 类型。设计中的 NormalizeWorkspacePolicy 示意由此私有入口及 Task 1 internal.Normalize 落实。

- [ ] RED：测试缺省写策略在创建 Runner/Workspace/Provider 前拒绝，错误包含 `workspace policy`，失败不创建 `.tmp`；stdio 单独启用时也必须拒绝：

```go
func TestNewRequiresExplicitWritePolicy(t *testing.T) {
    cases := []Options{
        {AllowWrite: true},
        {AllowExec: true},
        {MCPServers: []pimcp.ServerOptions{{Name: "stdio", Transport: "stdio", Command: "ignored"}}},
    }
    for _, opts := range cases {
        opts.WorkDir = t.TempDir()
        _, err := New(opts)
        if err == nil || !strings.Contains(err.Error(), "workspace policy") { t.Fatalf("got %v", err) }
        if _, err := os.Stat(filepath.Join(opts.WorkDir, ".tmp")); !errors.Is(err, os.ErrNotExist) {
            t.Fatalf("failed construction changed filesystem: %v", err)
        }
    }
}
```

导入 `testing`、`strings`、`os`、`path/filepath`、`errors` 和别名 pimcp。
- [ ] 运行 `go test ./pi -run 'TestNew.*Policy' -count=1`，确认 RED。
- [ ] 实现模式判定：

```go
needsProcess := opts.AllowExec
for _, server := range opts.MCPServers {
    if server.Transport == "stdio" { needsProcess = true }
}
p := opts.WorkspacePolicy
if p.WriteMode == "" {
    if opts.AllowWrite || needsProcess {
        return nil, false, errors.New("pi: workspace policy must be explicit when write or process execution is enabled")
    }
    p.WriteMode = WorkspaceWriteRestricted
}
n, err := workspacepolicy.Normalize(opts.WorkDir, p)
return n, needsProcess, err
```

- [ ] pi.New 使用该 n 调用 `sandbox.NewRunnerWithPolicy(n.Root(), n, needsProcess)` 与 `tools.NewWorkspaceWithPolicy(tools.Root(n.Root()), n)`；PromptComposer、ContextBuilder、MCP 也用 n.Root()。AllowWrite=false 不注册 write/edit/apply_patch；AllowExec=false 不注册 exec/process，即使因 stdio 创建了 sandbox Runner 也不扩大工具集合。
- [ ] 添加失败路径清理：在 Workspace 创建成功后装配完成前使用 defer 关闭它，Supervisor/扩展创建失败同样释放已创建资源；成功后把所有权交给 Agent.Stop。新 no-process Runner 不跑 OS 探针。新增 ToolDefinitions 断言和 Start/Stop 生命周期断言，用 `providers.Options{ID:"test", Protocol:providers.ProtocolOpenAI, APIKey:"test", Model:"fake"}` 构造但不向外部发请求。
- [ ] CLI 的 buildAgent：未配置策略时显式设 all，保持 `-write/-exec/-yolo` 原授权档位；配置中已显式给 restricted 时保留配置，不能被 yolo 静默扩大。main_test.go 的直接 pi.New fixture 相应补 all。server 先显式采用 `restricted + [.tmp,scratch]` 且 AllowWrite=false，仍保留当前单 Runner；Task 8 再接入配置。不得仅为让测试通过给所有业务入口默认 all。
- [ ] 运行 GREEN：`go test ./pi ./cmd/cli ./cmd/server ./conversation -count=1`。原生 OS 下额外用 `AllowExec=false + stdio` 验证写 Bundle 仍被沙箱拒绝；Windows 验证该组合 restricted 构造失败。
- [ ] 提交：`git add pi/agent.go pi/workspace_policy_test.go cmd/cli/main.go cmd/cli/main_test.go cmd/server/app.go`，`git commit -m 'feat(pi): require explicit write policy at SDK construction'`。

### Task 7: InspectWorkspace 与真实上下文共用解析

**Files:** 新增 `pi/harness/inspect.go`、`inspect_test.go`；修改 `prompt.go`、`context.go`、`context_test.go`。

**Interfaces:**

```go
type WorkspaceReport struct {
    AgentInstructionsDigest string
    Skills []skills.Summary
    Diagnostics []skills.Diagnostic
}
func InspectWorkspace(ctx context.Context, workDir string) (WorkspaceReport, error)
type workspaceSnapshot struct {
    agents []byte
    skills *skills.Snapshot
}
func loadWorkspaceSnapshot(ctx context.Context, workDir string) (workspaceSnapshot, error)
func composePrompt(agents []byte, snapshot *skills.Snapshot) (ai.Message, skills.PromptReport)
```

保留 `NewPromptComposer`、`PromptComposer.Build`、`NewContextBuilder` 和 `ContextBuilder.Build` 的现有签名。loadWorkspaceSnapshot 复用 `PromptComposer.loadAgentsInstructions` 与 `skills.Discover`；composePrompt 从现有 Build 提取字符串组合部分，不复制 AGENTS 或 Skill 解析器。

- [ ] 写以下 RED 测试，导入 `context`、`crypto/sha256`、`fmt`、`os`、`path/filepath`、`strings`、`testing` 和 ai：

```go
func TestInspectWorkspaceMatchesContext(t *testing.T) {
    root := t.TempDir()
    agents := []byte("You are a test Agent.\n")
    if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), agents, 0600); err != nil { t.Fatal(err) }
    dir := filepath.Join(root, "skills", "example")
    if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
    skill := []byte("---\nname: example\ndescription: Example workflow\n---\nUse this workflow.\n")
    if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), skill, 0600); err != nil { t.Fatal(err) }
    report, err := InspectWorkspace(context.Background(), root)
    if err != nil { t.Fatal(err) }
    wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(agents))
    if report.AgentInstructionsDigest != wantDigest || len(report.Skills) != 1 { t.Fatalf("report: %+v", report) }
    builder := NewContextBuilder(NewPromptComposer(root), root)
    built, err := builder.Build(context.Background(), ContextRequest{
        Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}},
    }, ai.ToolDefinitions{{Name: "read"}})
    if err != nil { t.Fatal(err) }
    text, err := built.Messages[0].Content.Text()
    if err != nil || !strings.Contains(text, report.Skills[0].Location) || !strings.Contains(text, string(agents)) {
        t.Fatalf("context differs: %s %v", text, err)
    }
    if _, err := os.Stat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) { t.Fatalf("inspection wrote files: %v", err) }
}
```

- [ ] 运行 `go test ./pi/harness -run TestInspectWorkspace -count=1`，确认 RED。
- [ ] 实现只读共用加载路径；ContextBuilder 用同一 snapshot 检查 `read` 工具要求、记录诊断、调用 composePrompt。InspectWorkspace 只生成摘要和复制出的列表，不生成 Runtime、不执行脚本、不要求平台 Provider 或 OS 进程后端：

```go
func InspectWorkspace(ctx context.Context, workDir string) (WorkspaceReport, error) {
    snapshot, err := loadWorkspaceSnapshot(ctx, workDir)
    if err != nil { return WorkspaceReport{}, err }
    digest := sha256.Sum256(snapshot.agents)
    return WorkspaceReport{
        AgentInstructionsDigest: fmt.Sprintf("sha256:%x", digest),
        Skills: snapshot.skills.Skills(),
        Diagnostics: snapshot.skills.Diagnostics(),
    }, nil
}
```

- [ ] loadWorkspaceSnapshot 在读前和读后检查 ctx.Err；AGENTS 错误保持 ErrWorkspaceInvalid 包装，digest 对原始有效 UTF-8 字节计算。Skill 诊断返回给调用者，不在 SDK 将所有 warning 强制转换为错误；Phase 1C 发布门槛自行决定拒绝。
- [ ] 加入缺 AGENTS、空白、NUL、无效 UTF-8、AGENTS symlink/目录、取消 Context、重复 Skill、缺少依赖、超过 256 KiB 的一致性矩阵：检查接口与 Context 加载都不能接受不合法 AGENTS；Skill 跳过和诊断与 `skills.Discover` 完全相同。对检查前后目录树及原文件摘要做比较，保证没有 `.tmp` 或其他输出生成。
- [ ] 运行 GREEN：`go test ./pi/harness ./pi/harness/skills -count=1`，再运行 `go test -race ./pi/harness -count=1`。
- [ ] 提交：`git add pi/harness/inspect.go pi/harness/inspect_test.go pi/harness/prompt.go pi/harness/context.go pi/harness/context_test.go`，`git commit -m 'feat(pi): inspect workspace through runtime parsers'`。

### Task 8: 配置、服务端与 CLI 使用明确策略

**Files:** 修改 `config/config.go`、`validate.go`、`server_options.go`、`config.example.json`、`cmd/server/app.go`、`cmd/cli/main.go`、`cmd/cli/main_test.go`；新增 `config/workspace_policy_test.go`、`cmd/server/app_test.go`。

**Interfaces:** AgentConfig 增加可空配置；nil 表示调用入口选择明确默认值，不代表 SDK 可以忽略 fail-fast：

```go
type WorkspacePolicyConfig struct {
    WriteMode string `json:"write_mode" yaml:"write_mode" toml:"write_mode"`
    WritablePrefixes []string `json:"writable_prefixes" yaml:"writable_prefixes" toml:"writable_prefixes"`
}
// 在 AgentConfig 中新增：
// WorkspacePolicy *WorkspacePolicyConfig `json:"workspace_policy" yaml:"workspace_policy" toml:"workspace_policy"`
func (p WorkspacePolicyConfig) PI() pi.WorkspacePolicy {
    return pi.WorkspacePolicy{
        WriteMode: pi.WorkspaceWriteMode(p.WriteMode),
        WritablePrefixes: append([]string(nil), p.WritablePrefixes...),
    }
}
```

`config.PIRuntimeOptions` 只复制显式配置，nil 保留零值；配置校验通过 Task 1 的 `pi.ValidateWorkspacePolicy` 共用规则。已经过配置校验的策略在 pi.New 中仍规范化一次供本实例复用，不能信任外部对象永不变化。

- [ ] 写 RED 配置测试（导入 `testing`、`reflect` 和 pi），先锁定纯翻译不会共享切片：

```go
func TestWorkspacePolicyConfigCopiesPrefixes(t *testing.T) {
    p := WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch"}}
    got := p.PI()
    p.WritablePrefixes[0] = "unsafe"
    if got.WriteMode != pi.WorkspaceWriteRestricted || !reflect.DeepEqual(got.WritablePrefixes, []string{".tmp", "scratch"}) {
        t.Fatalf("policy alias or mapping error: %+v", got)
    }
}
```

- [ ] 使用 config/config_test.go 的真实 LoadConfig 测试夹具添加 JSON/YAML/TOML 配置矩阵：显式 all、restricted、空 mode、绝对/父级前缀、未知 mode、all 携带前缀。验证所有错误在服务开始监听前报告；合法校验不创建临时目录。
- [ ] 运行 RED：`go test ./config -run WorkspacePolicy -count=1`。
- [ ] 接入 AgentConfig.normalizeAndValidate 已解析的 WorkspaceDir，对非 nil 的 p 调用 `pi.ValidateWorkspacePolicy(workDir,p.PI())`；PIRuntimeOptions 把 p.PI() 赋给 opts.WorkspacePolicy。禁止 config 直接 import `pi/internal/workspacepolicy`。
- [ ] 在 server app.go 提取 `serverAgentOptions(params appParams) (pi.Options,error)`，把既有纯装配代码移入此函数，再由 newApp 调用 pi.New。该函数非 nil 配置必须为 restricted 且仅允许 `.tmp`、`scratch` 的子路径，必须包含完整 `.tmp`；all 或包含行为目录返回错误。nil 使用以下明确值：

```go
opts.WorkspacePolicy = pi.WorkspacePolicy{
    WriteMode: pi.WorkspaceWriteRestricted,
    WritablePrefixes: []string{".tmp", "scratch"},
}
opts.AllowWrite = false
opts.AllowExec = true
opts.BuiltinSubagent = true
```

显式配置必须保留其更小的前缀集合，不能被以上默认值覆盖；server 拒绝 all 是入口政策，SDK/CLI 仍支持 all。
- [ ] app_test.go 直接测试 serverAgentOptions，不连接 MySQL/Redis/模型；夹具 Config 使用 `CurrentPlatform:"test"`、一条 `providers.Options{ID:"test", Protocol:providers.ProtocolOpenAI, APIKey:"test", Model:"fake"}`。断言无 write 工具授权、显式更窄策略保留、all 被拒绝。CLI 测试分别验证 nil 配置→all、显式 restricted→保留、write/exec/yolo 只决定工具注册，不覆盖模式。
- [ ] 在 config.example.json 的 agent 节增加：

```json
"workspace_policy": {
  "write_mode": "restricted",
  "writable_prefixes": [".tmp", "scratch"]
}
```

该配置也用于 CLI 时会限制写入；Coding 用户可显式选择 all 且 prefixes 为空。不能在本期把服务端 WorkDir 改成租户/会话目录或新增 Runtime Manager。当前 Web 仍是单 Workspace，Phase 1A 验收只承诺行为资产写保护，不声称已完成多租户或会话文件隔离。
- [ ] 运行 GREEN：`go test ./config ./cmd/server ./cmd/cli -count=1`。
- [ ] 提交：`git add config/config.go config/validate.go config/server_options.go config/workspace_policy_test.go config.example.json cmd/server/app.go cmd/server/app_test.go cmd/cli/main.go cmd/cli/main_test.go`，`git commit -m 'feat(config): configure explicit server and CLI workspace policies'`。

### Task 9: 汇总验收与实现评审

**Files:** 不新增功能。若验收发现真实回归，回到对应 Task 的测试/修复步骤，不用批量更改断言掩盖问题。

- [ ] 运行变更范围回归和 race：

```sh
GOCACHE=/tmp/go-reagent-phase1a-cache go test ./pi/... ./config/... ./cmd/cli/... ./cmd/server/... -count=1
GOCACHE=/tmp/go-reagent-phase1a-cache go test -race ./pi/internal/workspacepolicy/... ./pi/harness/... ./pi/mcp/... -count=1
```

- [ ] 在 macOS 与 Linux 分别执行 native suites，记录 GOOS、Go 版本、后端及完整退出结果：

```sh
RUN_WORKSPACE_SANDBOX_INTEGRATION=1 GOCACHE=/tmp/go-reagent-phase1a-cache go test ./pi/harness/sandbox ./pi/mcp -run Native -count=1 -v
```

- [ ] 最后只运行一次全仓验证和 diff 检查：

```sh
GOCACHE=/tmp/go-reagent-phase1a-cache go test ./... -count=1
git diff --check
git status --short
```

- [ ] 在实现评审记录中区分实际通过、明确跳过、未具备环境三类结果。必须有 macOS 与 Linux 的原生拒写证据及 Host fail-closed 测试；缺少任一项不能宣称 Phase 1A 已完成。
- [ ] 对照下表审核，确认没有混入 Phase 1B/1C 的表、API、页面或 Runtime Manager；提交实现 PR 供评审，不自动进入 Phase 1B 或部署。

## 需求覆盖表

| 设计要求 | 对应任务及可核验证据 |
| --- | --- |
| §10.2 显式模式、空值 fail fast、相对前缀 | Tasks 1、6；TestNormalize、TestNewRequiresExplicitWritePolicy |
| §24.1 restricted + AllowWrite=false 不注册写工具 | Task 6 ToolDefinitions；Task 8 server 装配 |
| exec 不能写 Bundle，可写允许的临时区 | Tasks 3、4；两个 OS 的 TestNativeRestrictedWrites |
| Host 无法保证时启动失败 | Tasks 3、6；Windows selector 与 stdio-only 用例 |
| all + AllowWrite=true 保持 Coding 行为 | Tasks 2、6、8；工具/CLI 回归 |
| InspectWorkspace 与 ContextBuilder 一致 | Task 7；有效/无效 AGENTS 和 Skill 诊断矩阵 |
| symlink、路径逃逸、特殊文件拒绝 | Tasks 1、2、4；子根句柄和原生拒写验证 |
| MCP stdio 继承同一写边界 | Tasks 5、6；direct argv native 测试与 no-exec+stdio 入口检查 |
| config 和组合根显式选择策略 | Task 8；JSON/YAML/TOML、server/CLI 装配测试 |
| 不混入未提交改动及后续阶段 | Tasks 0、9；隔离 worktree 与最终 diff |

## 评审交接

本轮交付仅包含设计修订和本实施计划。评审重点是受限文件操作的子根边界、OS 实际拒写、
stdio-only 的启动限制，以及过渡期 Web 单 Workspace 的能力界限。评审通过后再按 Task 0
建立隔离工作区并执行 TDD；原工作区无需为了起草计划而 stash 或提交无关代码。
