package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// HostRunner 是默认后端：不构造 wrapper，命令经 ShellInvocation 现状契约
// 直接执行，行为与迁移前的 NewChildProcess 逐字等价。
type HostRunner struct{ policy Policy }

func NewHostRunner() *HostRunner {
	return &HostRunner{policy: Policy{Backend: "host", Network: "allow", WriteMode: "all"}}
}

func (r *HostRunner) Policy() Policy { return clonePolicy(r.policy) }

func (r *HostRunner) BuildShell(command string, spec CommandSpec) (*exec.Cmd, error) {
	if command == "" {
		return nil, errors.New("exec: 命令为空")
	}
	// host 不校验契约变量，只做统一的格式/NUL 校验；WorkDir 维持现状语义。
	if err := validateEnvList(spec.PayloadEnv); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvContractRejected, err)
	}
	shell, arguments := ShellInvocation(command)
	child := BuildCommand(shell, arguments, spec.WorkDir)
	child.Env = spec.PayloadEnv // host 无 wrapper：PayloadEnv 直接作为进程环境
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

// HostPayloadEnv 继承宿主环境并合并 overrides，最终按变量名去重、排序。
// 禁改 PATH，允许覆盖 HOME/TMPDIR。
func HostPayloadEnv(overrides map[string]string) ([]string, error) {
	effective := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && kvOK(key) && strings.IndexByte(entry, 0) < 0 {
			effective[key] = value
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("无效环境变量名: %q", key)
		}
		if strings.EqualFold(key, "PATH") {
			return nil, errors.New("禁止通过 exec 覆盖 PATH")
		}
		value := overrides[key]
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("环境变量 %s 包含 NUL", key)
		}
		effective[key] = value
	}
	keys = keys[:0]
	for key := range effective {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+effective[key])
	}
	return environment, nil
}

// ShellInvocation 返回 Host 后端执行 command 使用的 shell 与参数。
func ShellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-lc", command}
}
