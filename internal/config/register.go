package config

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/fx"
)

// WorkDir is the Agent workspace path injected through Fx.
type WorkDir string

// Prompt is the one-shot task injected into the application runner.
type Prompt string

// Register provides process configuration values to the application graph.
var Register = fx.Options(
	fx.Provide(
		NewConfig,
		NewWorkDir,
		NewPrompt,
	),
)

// NewConfig loads the process configuration selected by CONFIG_PATH.
func NewConfig() (*Config, error) {
	return Load(configurationPath())
}

// NewWorkDir resolves the current process directory as the Agent workspace.
func NewWorkDir() (WorkDir, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}
	return WorkDir(workDir), nil
}

// NewPrompt returns the process override or the default one-shot task.
func NewPrompt() Prompt {
	if prompt := os.Getenv("AGENT_PROMPT"); prompt != "" {
		return Prompt(prompt)
	}
	return Prompt(`你好，老板，你看下明天天气怎么样？。`)
}

func configurationPath() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		return "config.json"
	}
	return path
}
