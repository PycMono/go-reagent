// Package sandbox 提供统一的本地命令执行边界及 Host、Seatbelt、
// Bubble wrap 后端。
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Policy SandboxPolicy 是后端的生效策略，用于工具描述、CLI banner 与 Details。
type Policy struct {
	Backend string // host / seatbelt / bubble wrap
	Network string // allow / deny
}

// CommandSpec 描述一次命令执行的共享约束。
type CommandSpec struct {
	// WorkDir 是本次 cwd 的宿主绝对路径，必须位于 WorkspaceRoot 内。
	// 边界判断对 WorkDir 与 WorkspaceRoot 都用 filepath.EvalSymlinks 解析后的
	// 真实路径（与 Seatbelt profile 注入同一规则）；host 后端维持现状语义、
	// 不做此约束（由沙箱后端在 Build 时经 ResolveWorkDir 校验）。
	WorkDir string
	// PayloadEnv 是调用方准备好的最终 payload 环境（KEY=VALUE 列表）。
	// 沙箱后端在沙箱边界内侧注入；host 后端直接作为进程环境。
	PayloadEnv []string
}

// Build 层错误（策略拒绝，调用方转结构化启动错误）。
var (
	ErrEnvContractRejected = errors.New("env_contract_rejected")
	ErrWorkDirRejected     = errors.New("workdir_rejected")
)

// ProcessPipeWaitDelay 有界关闭遗留管道：直接子进程退出后，setsid 残留进程
// 若继承 stdout/stderr 管道，Wait() 会因等不到 EOF 永久阻塞。触发语义分两档：
// 直接子进程非零退出 → 正常 *exec.ExitError；成功退出但后代持管道 → exec.ErrWaitDelay。
const ProcessPipeWaitDelay = 3 * time.Second

// Runner 构造受限的、未启动的 *exec.Cmd。进程生命周期（管道、启动、
// 等待、进程组终止）由调用方持有，Runner 只负责构造。
type Runner interface {
	Policy() Policy
	// BuildShell 按后端 shell 契约构造 shell 命令进程。
	BuildShell(command string, spec CommandSpec) (*exec.Cmd, error)
	// BuildArgv 构造不经 shell 的直接执行进程（argv[0] 为可执行文件），
	// 用于 MCP stdio。空 argv 返回错误（不访问 argv[0]）。
	BuildArgv(argv []string, spec CommandSpec) (*exec.Cmd, error)
}

// BuildCommand 供各后端收尾：进程组 + WaitDelay + Dir。
func BuildCommand(path string, argv []string, dir string) *exec.Cmd {
	child := exec.Command(path, argv...)
	child.Dir = dir
	ConfigureProcessGroup(child)
	child.WaitDelay = ProcessPipeWaitDelay
	return child
}

// ResolveWorkDir 按 EvalSymlinks 真实路径校验 workDir 位于 workspaceRoot 内，
// 返回 canonical 路径——调用方必须把返回值用于 cmd.Dir 与沙箱内 --chdir，
// 避免"边界按真实路径判、执行却用原始路径"的符号链接错位。
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

// kvOK 变量名合法性（与原 processEnvironment 校验一致：非空、不含 = / NUL）。
func kvOK(key string) bool {
	return key != "" && !strings.ContainsAny(key, "=\x00")
}

// validateEnvList 统一环境校验：每项必须是 KEY=VALUE、名字合法、值不含
// NUL，且同名变量只能出现一次。
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
			return fmt.Errorf("环境变量 %s 重复", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// 契约变量与固定基础值。沙箱不继承宿主 locale 和终端设置。
const (
	SandboxPath = "/usr/bin:/bin"
	SandboxLang = "C.UTF-8"
	SandboxTerm = "dumb"
)

// Env 返回沙箱后端基础环境。
func Env(workspaceRoot, tmpDir string) []string {
	return []string{
		"HOME=" + workspaceRoot,
		"LANG=" + SandboxLang,
		"PATH=" + SandboxPath,
		"TERM=" + SandboxTerm,
		"TMPDIR=" + tmpDir,
	}
}

var contractKeys = map[string]struct{}{"PATH": {}, "HOME": {}, "TMPDIR": {}}

// BuildSandboxPayloadEnv 合并基础项与外部输入，契约变量禁止覆盖。
func BuildSandboxPayloadEnv(workspaceRoot, tmpDir string, extra []string) ([]string, error) {
	merged := map[string]string{}
	for _, kv := range Env(workspaceRoot, tmpDir) {
		key, value, _ := strings.Cut(kv, "=")
		merged[key] = value
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
	for key, value := range merged {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out, nil
}

// ValidateSandboxPayloadEnv 校验契约变量存在、唯一且值符合后端预期。
func ValidateSandboxPayloadEnv(env []string, workspaceRoot, tmpDir string) error {
	if err := validateEnvList(env); err != nil {
		return err
	}
	count := map[string]int{}
	value := map[string]string{}
	for _, kv := range env {
		key, itemValue, _ := strings.Cut(kv, "=")
		count[key]++
		value[key] = itemValue
	}
	want := map[string]string{"PATH": SandboxPath, "HOME": workspaceRoot, "TMPDIR": tmpDir}
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		if count[key] != 1 || value[key] != want[key] {
			return fmt.Errorf("%w: 契约变量 %s 缺失/重复/值不匹配", ErrEnvContractRejected, key)
		}
	}
	return nil
}
