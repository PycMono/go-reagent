package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
)

// NewChatCompactionConfig 把业务开关与平台模型容量组合为 Agent 压缩配置。
// pi 层不反向依赖 config 包，装配责任留在本层。
func NewChatCompactionConfig(cfg *config.Config, platform providers.Options) harness.CompactionConfig {
	compaction := harness.CompactionConfig{ContextWindowTokens: platform.ContextWindowTokens}
	if cfg != nil {
		compaction.EnablePrune = cfg.Agent.EnableContextPrune
	}
	return compaction
}

// NewChatWorkDir resolves the configured runtime Agent Workspace.
func NewChatWorkDir(cfg *config.Config) (pi.WorkDir, error) {
	if cfg == nil {
		return "", errors.New("Agent Workspace 配置不能为空")
	}

	path := strings.TrimSpace(cfg.Agent.WorkspaceDir)
	if path == "" {
		path = config.DefaultAgentWorkspaceDir
	}

	resolved, err := resolveDirectory(path)
	if err != nil {
		return "", fmt.Errorf("检查 Agent Workspace %q 失败: %w", path, err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("检查 Agent Workspace %q 失败: 获取进程当前目录: %w", path, err)
	}
	resolvedWorkingDir, err := resolveDirectory(workingDir)
	if err != nil {
		return "", fmt.Errorf("检查 Agent Workspace %q 失败: 解析进程当前目录: %w", path, err)
	}
	if resolved == resolvedWorkingDir {
		return "", fmt.Errorf("Agent Workspace %q 不能使用进程当前目录", path)
	}

	return pi.WorkDir(resolved), nil
}

func resolveDirectory(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析绝对路径: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("解析真实路径: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("必须是目录")
	}
	return filepath.Clean(resolved), nil
}
