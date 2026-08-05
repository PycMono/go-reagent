package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/internal/cli/app"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

// NewConfig loads the process configuration selected by CONFIG_PATH.
func NewConfig() (*config.Config, error) {
	return config.Load(configurationPath())
}

func NewPlatform(cfg *config.Config) (ai.PlatformConfig, error) {
	return cfg.Pi.Current()
}

// NewWorkDir resolves the current process directory as the Agent workspace.
func NewWorkDir() (pi.WorkDir, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}
	return pi.WorkDir(workDir), nil
}

// NewPrompt returns the process override or the default one-shot task.
func NewPrompt() app.Prompt {
	if prompt := os.Getenv("AGENT_PROMPT"); prompt != "" {
		return app.Prompt(prompt)
	}
	return app.Prompt(`我需要在当前目录下新建一个 ping.go，提供一个简单的 http ping 接口。 写完之后，帮我把代码用 git 提交一下。`)
}

func configurationPath() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		return "config.json"
	}
	return path
}
