package application

import (
	"fmt"
	"os"

	"github.com/PycMono/go-reagent/pi"
)

// NewWorkDir resolves the process working directory used as the Pi resource root.
func NewWorkDir() (pi.WorkDir, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}
	return pi.WorkDir(workDir), nil
}

// NewPrompt returns the process override or the bundled one-shot task.
func NewPrompt() Prompt {
	if prompt := os.Getenv("AGENT_PROMPT"); prompt != "" {
		return Prompt(prompt)
	}
	return Prompt(`我需要在当前目录下新建一个 ping.go，提供一个简单的 http ping 接口。 写完之后，帮我把代码用 git 提交一下。`)
}
