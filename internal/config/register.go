package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

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
func NewWorkDir() (workspace.WorkDir, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}
	return workspace.WorkDir(workDir), nil
}

// NewPrompt returns the process override or the default one-shot task.
func NewPrompt() Prompt {
	if prompt := os.Getenv("AGENT_PROMPT"); prompt != "" {
		return Prompt(prompt)
	}
	return Prompt(`我需要在当前目录下新建一个 ping.go，提供一个简单的 http ping 接口。 写完之后，帮我把代码用 git 提交一下。`)
}

func configurationPath() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		return "config.json"
	}
	return path
}
