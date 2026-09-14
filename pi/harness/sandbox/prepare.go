package sandbox

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// PrepareCommandSpec prepares working directory and environment for either
// BuildShell or BuildArgv. Callers supply only execution inputs; backend-specific
// defaults and environment inheritance are handled here. It starts no process.
func PrepareCommandSpec(runner Runner, workspaceRoot, workDir string, overrides map[string]string) (CommandSpec, error) {
	policy := runner.Policy()
	if policy.Backend == "host" && policy.WriteMode == "all" {
		env, err := HostPayloadEnv(overrides)
		return CommandSpec{WorkDir: workDir, PayloadEnv: env}, err
	}
	root, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return CommandSpec{}, fmt.Errorf("解析工作区真实路径失败: %w", err)
	}
	cwd := strings.TrimSpace(workDir)
	if cwd == "" {
		cwd = root
	} else if cwd, err = ResolveWorkDir(cwd, root); err != nil {
		return CommandSpec{}, err
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		if !kvOK(key) {
			return CommandSpec{}, fmt.Errorf("无效环境变量名: %q", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	extras := make([]string, 0, len(keys))
	for _, key := range keys {
		extras = append(extras, key+"="+overrides[key])
	}
	tmpDir, err := PayloadTmpDir(policy, root)
	if err != nil {
		return CommandSpec{}, err
	}
	env, err := BuildSandboxPayloadEnv(root, tmpDir, extras)
	if err != nil {
		return CommandSpec{}, err
	}
	return CommandSpec{WorkDir: cwd, PayloadEnv: env}, nil
}
