package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BubblewrapRunner 用设计 §5.1 原型实测的参数组合构造 bwrap 命令。
type BubblewrapRunner struct {
	wrapPath      string
	workspaceRoot string // EvalSymlinks 后的真实路径
	policy        Policy
}

func NewBubblewrapRunner(bwrapPath, workspaceRoot string) (*BubblewrapRunner, error) {
	symlinks, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root 解析失败: %w", err)
	}
	return &BubblewrapRunner{
		wrapPath: bwrapPath, workspaceRoot: symlinks,
		policy: Policy{Backend: "bubblewrap", Network: "allow"},
	}, nil
}

func (r *BubblewrapRunner) Policy() Policy { return r.policy }

// bwrapWrapperEnv 是 bwrap 自身（PID 1）的环境，即 /proc/1/environ 的内容——
// 必须最小化（§5.1 实证：wrapper 环境含密钥时 payload 可经此读到）。
var bwrapWrapperEnv = []string{"PATH=" + SandboxPath}

func (r *BubblewrapRunner) BuildShell(commandStr string, spec CommandSpec) (*exec.Cmd, error) {
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	argv := r.baseArgv(spec, workDir)
	argv = append(argv, "--", "/bin/sh", "-c", commandStr)
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

// baseArgv 返回固定 allow-network 参数组合（必需挂载 + --chdir + setenv 注入），
// 顺序即文档顺序。--chdir 用 canonical WorkDir：宿主 cmd.Dir 不能替代沙箱内 cwd。
func (r *BubblewrapRunner) baseArgv(spec CommandSpec, workDir string) []string {
	argv := []string{
		"--die-with-parent",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--clearenv", // 必需：缺失时 payload 继承 wrapper 环境（原型实证）
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
	}
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
