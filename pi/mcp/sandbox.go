// 本文件承载沙箱后端拉起 MCP stdio server 的构造逻辑（设计 §5.4、§7）；
// host 直拉路径见 options.go。
package mcp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/harness/sandbox"
)

// sandboxBuildCommand 沙箱路径的进程构造回调：cwd 空→归一化为
// WorkspaceRoot，显式 cwd 必须位于工作区内；env 只包含固定基础项与
// server 显式配置，不继承宿主。
func sandboxBuildCommand(runner sandbox.Runner, workspaceRoot string, options ServerOptions) (func() (*exec.Cmd, error), error) {
	canonicalRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("解析工作区真实路径失败: %w", err)
	}
	cwd := strings.TrimSpace(options.CWD)
	if cwd == "" {
		cwd = canonicalRoot
	} else if cwd, err = sandbox.ResolveWorkDir(cwd, canonicalRoot); err != nil {
		return nil, fmt.Errorf("mcp server %q cwd 被拒: %w", options.Name, err)
	}

	tmpDir, err := sandboxTmpDir(runner.Policy().Backend, canonicalRoot)
	if err != nil {
		return nil, err
	}
	payload, err := sandbox.BuildSandboxPayloadEnv(canonicalRoot, tmpDir, environmentEntries(options.Env))
	if err != nil {
		return nil, fmt.Errorf("mcp server %q 环境构造被拒: %w", options.Name, err)
	}

	spec := sandbox.CommandSpec{WorkDir: cwd, PayloadEnv: payload}
	return func() (*exec.Cmd, error) {
		return runner.BuildArgv(append([]string{options.Command}, options.Args...), spec)
	}, nil
}

// environmentEntries 把环境 map 转为排序的 NAME=value 条目。
func environmentEntries(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]string, 0, len(values))
	for _, key := range keys {
		entries = append(entries, key+"="+values[key])
	}
	return entries
}

// sandboxTmpDir 返回所选后端的 TMPDIR 策略（§5.4）；host 不走此路径。
func sandboxTmpDir(backend, workspaceRoot string) (string, error) {
	switch backend {
	case "bubblewrap":
		return "/tmp", nil
	case "seatbelt":
		return filepath.Join(workspaceRoot, ".tmp"), nil
	default:
		return "", fmt.Errorf("未知沙箱后端 %q", backend)
	}
}
