# OpenClaw 风格本地工具闭环设计

## 背景与参考

当前 Registry 只提供 `read_file` 和 `edit_file`。Agent 可以读取和替换已有文本，但不能创建文件、执行 Git/Go 命令、应用多文件补丁或管理长时间运行的进程。

本设计参考 OpenClaw `main` 分支提交 `9979ee312d4348b84b9227235339120fbb2ae58a`（2026-07-31）：

- 工具总览：<https://github.com/openclaw/openclaw/blob/main/docs/tools/index.md>
- Exec：<https://github.com/openclaw/openclaw/blob/main/docs/tools/exec.md>
- Apply Patch：<https://github.com/openclaw/openclaw/blob/main/docs/tools/apply-patch.md>
- Write：<https://github.com/openclaw/openclaw/blob/main/src/agents/sessions/tools/write.ts>

## 范围

形成面向本地编码任务的完整工具面：

1. 保留 `read_file` 与 `edit_file`。
2. 新增 `write_file`，创建或完整覆盖 UTF-8 文本文件，并递归创建父目录。
3. 新增 `apply_patch`，支持 `*** Add File`、`*** Update File`、`*** Delete File` 和 `*** Move to`。
4. 新增 `exec`，在工作区内启动 shell 命令，支持超时、前台等待、自动后台化和有界输出。
5. 新增 `process`，列出、轮询、写入和终止当前 Agent 的后台命令。
6. 更新 Fx Registry、README 和工作区 `git-workflow` 技能。

不加入浏览器、网络、消息、定时任务、节点控制、PTY 或人工审批系统。它们属于独立子系统。

## 文件工具

### `write_file`

输入为必填的 `path` 与 `content`。路径必须是工作区相对路径；内容必须是合法 UTF-8 且不能包含 NUL。工具递归创建父目录，以 `0644` 创建新文件，完整覆盖已有普通文件；内容相同则返回未修改。所有访问通过 `os.Root`，拒绝路径逃逸、外部符号链接和非普通文件。

### `apply_patch`

输入为必填 `input`，必须由 `*** Begin Patch` 与 `*** End Patch` 包裹。一个补丁可以包含多个文件操作；所有路径必须是工作区相对路径。解析、预检全部成功后才执行写入，避免语法或上下文错误造成部分修改。

更新块按原始顺序匹配上下文与删除行；匹配必须唯一。新增目标必须不存在，删除目标必须存在，移动目标必须不存在。更新和新增内容必须为 UTF-8 文本且不能包含 NUL。

## 命令与进程工具

### `exec`

输入：

- `command`：必填 shell 命令；
- `workdir`：可选工作区相对目录，默认工作区根；
- `timeout_ms`：可选，默认 120000，范围 1..600000；
- `yield_ms`：可选，默认 10000，范围 0..30000；
- `background`：可选，默认 false；
- `env`：可选字符串键值环境覆盖，禁止覆盖 `PATH`。

非 Windows 使用 `$SHELL -lc`，没有 `$SHELL` 时使用 `/bin/sh -lc`；Windows 使用 `cmd.exe /d /s /c`。命令在独立进程组中运行，Context 取消、超时或 `process.kill` 必须终止进程树。

输出最多保留末尾 50 KiB。命令在 `yield_ms` 内结束时返回退出码和输出；否则返回 `session_id` 交给 `process`。非零退出码作为正常完成状态返回在 JSON 中，不让 Registry 丢失 stdout/stderr。

### `process`

输入 `action`：

- `list`：列出当前会话；
- `poll`：按 `session_id` 查询状态，可用 `wait_ms` 最多等待 30000 ms；
- `write`：向 stdin 写入 `data`，可用 `eof=true` 关闭 stdin；
- `kill`：终止进程树。

完成会话保留到 Registry 关闭；Registry 关闭时终止仍在运行的全部会话并释放资源。`exec` 与 `process` 都是独占工具，不参与并行安全波次。

## 安全边界

- 文件工具通过 `os.Root` 限制路径和符号链接逃逸。
- `exec.workdir` 必须位于工作区，但 shell 命令本身拥有 go-reagent 宿主进程的权限；cwd 限制不是系统调用沙箱。
- 限制命令时长、单次等待时间和保留输出，避免无限阻塞和内存膨胀。
- 不记录环境变量值或命令输出到结构化日志。
- 人工审批、命令 allowlist、容器/Seatbelt 沙箱和 elevated 模式留作后续安全层。

## 测试

- 每个工具验证定义、严格 JSON 参数、取消与关闭行为。
- 文件工具验证创建、覆盖、幂等、父目录、路径逃逸、符号链接、二进制内容与补丁原子预检。
- 命令工具验证成功、非零退出、stderr、超时、输出截断、工作目录、自动后台化、轮询、stdin、kill 和关闭清理。
- Registry 集成测试验证六个工具均可发现，并在 Fx Stop 后不可继续使用。
- 运行 `go test ./...`、`go test -race ./internal/tools ./internal/app`、`go vet ./...` 和 `git diff --check`。
