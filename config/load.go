package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/jinzhu/configor"
)

// Load decodes the existing flattened service configuration.
func Load(path string) (*Config, error) {
	var config Config
	if err := configor.Load(&config, path); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	if err := config.normalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	return &config, nil
}

// NewFromEnvironment loads CONFIG_PATH, defaulting to config.json.
func NewFromEnvironment() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		path = "config.json"
	}
	return Load(path)
}

// NewPlatform returns the selected model platform for Pi's Fx graph.
func NewPlatform(config *Config) (providers.Options, error) {
	return config.CurrentPlatformOptions()
}

// NewWorkDir 返回 Load 已解析并校验过的 Agent Workspace 绝对路径。
func NewWorkDir(config *Config) pi.WorkDir {
	return pi.WorkDir(config.Agent.WorkspaceDir)
}

// NewCompactionConfig 把业务开关与平台模型容量组合为 Agent 压缩配置。
func NewCompactionConfig(config *Config, platform providers.Options) harness.CompactionConfig {
	return harness.CompactionConfig{
		ContextWindowTokens: platform.ContextWindowTokens,
		EnablePrune:         config.Agent.EnableContextPrune,
	}
}
