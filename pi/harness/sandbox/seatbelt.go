package sandbox

import (
	"fmt"
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SeatbeltRunner 构造 sandbox-exec 包装的命令。
type SeatbeltRunner struct {
	sandboxExecPath string
	profile         string
	dArgs           []string // 固定只有 WORKSPACE_ROOT 参数
	workspaceRoot   string   // EvalSymlinks 后的真实路径
	tmpDir          string   // <workspaceRoot>/.tmp（§5.4，Workspace 启动时创建）
	normalized      *workspacepolicy.Normalized
	policy          Policy
}

func NewSeatbeltRunner(sandboxExecPath, workspaceRoot string) (*SeatbeltRunner, error) {
	symlinks, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root 解析失败: %w", err)
	}
	// 真实路径仍可能含特殊字符（目录名含引号/反斜杠），双保险校验（§6.2）。
	if strings.ContainsAny(symlinks, "\"\\\n") {
		return nil, fmt.Errorf("路径含 profile 注入字符: %q", symlinks)
	}
	tmpDir := filepath.Join(symlinks, ".tmp") // §5.4：单一共享 .tmp，构造期创建一次
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建工作区 .tmp 失败: %w", err)
	}
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("设置工作区 .tmp 权限失败: %w", err)
	}

	return &SeatbeltRunner{
		sandboxExecPath: sandboxExecPath,
		profile:         seatbeltProfile(),
		dArgs:           []string{"-D", "WORKSPACE_ROOT=" + symlinks},
		workspaceRoot:   symlinks,
		tmpDir:          tmpDir,
		policy:          Policy{Backend: "seatbelt", Network: "allow", TmpDir: tmpDir},
	}, nil
}

func (r *SeatbeltRunner) Policy() Policy { return clonePolicy(r.policy) }

func (r *SeatbeltRunner) BuildShell(commandStr string, spec CommandSpec) (*exec.Cmd, error) {
	workDir, err := r.validate(spec)
	if err != nil {
		return nil, err
	}
	// argv 顺序：sandbox-exec -D K=V... -p <profile> env -i <PayloadEnv...> /bin/sh -c <command>。
	// env -i 是 payload 环境的沙箱边界内侧注入（§5.3）；sandbox-exec 自身拿 WrapperEnv。
	argv := append([]string{}, r.dArgs...)
	argv = append(argv, "-p", r.profile, "env", "-i")
	argv = append(argv, spec.PayloadEnv...)
	argv = append(argv, "/bin/sh", "-c", commandStr)
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
	if r.normalized != nil {
		if err := prepareWritableDirectories(r.normalized); err != nil {
			return "", err
		}
	}
	workDir, err := ResolveWorkDir(spec.WorkDir, r.workspaceRoot)
	if err != nil {
		return "", err
	}
	if err := ValidateSandboxPayloadEnv(spec.PayloadEnv, r.workspaceRoot, r.tmpDir); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}

	return workDir, nil
}

// seatbeltProfile 返回固定 allow-network profile。workspaceRoot 经 -D 参数
// 注入，不与 Scheme 字符串拼接。
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

func NewSeatbeltRunnerWithPolicy(binary, root string, n *workspacepolicy.Normalized) (*SeatbeltRunner, error) {
	if err := validateNativePolicy(root, n); err != nil {
		return nil, err
	}
	r := &SeatbeltRunner{sandboxExecPath: binary, workspaceRoot: n.Root(), tmpDir: filepath.Join(n.Root(), ".tmp"), profile: seatbeltProfile(), dArgs: []string{"-D", "WORKSPACE_ROOT=" + n.Root()}, normalized: n}
	r.policy = policyFromNormalized("seatbelt", "allow", r.tmpDir, n)
	if n.Mode() == workspacepolicy.Restricted {
		rules := []string{}
		for i, p := range n.Prefixes() {
			key := fmt.Sprintf("WRITE_ROOT_%d", i)
			r.dArgs = append(r.dArgs, "-D", key+"="+filepath.Join(n.Root(), p))
			rules = append(rules, fmt.Sprintf(`(allow file-write* (subpath (param "%s")))
(deny file-write-unlink (literal (param "%s")))`, key, key))
		}
		r.profile = strings.Replace(r.profile, `(allow file-write* (subpath (param "WORKSPACE_ROOT")))`, strings.Join(rules, "\n"), 1)
	}
	return r, nil
}
