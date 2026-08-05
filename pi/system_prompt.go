package pi

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/skills"
)

const corePrompt = `# Agent Runtime 核心纪律

1. 必须遵守工作区 AGENTS.md 中定义的身份、职责和行为边界。
2. 必须根据当前任务判断是否存在匹配的 Skill；使用 Skill 前，必须通过 read 完整读取对应的 SKILL.md。
3. 只能调用当前请求中实际提供定义的工具，不得虚构或模拟工具调用。
4. 当没有提供工具定义时，你正处于 Thinking 阶段：只能分析和规划，不得声称工具已执行，也不得编造外部事实。
5. 工具执行失败时，必须依据真实错误处理，不得声称操作成功。
6. 最终回答必须以当前上下文、Skill 指令和真实工具结果为依据。
`

// PromptComposer builds one System Prompt from the current workspace state.
type PromptComposer struct {
	workDir string
}

// NewPromptComposer 创建一个绑定指定工作区的 Prompt 组合器，
// 后续构建系统提示词时会从该工作区读取项目说明和 Skill 信息。
func NewPromptComposer(workDir string) *PromptComposer {
	return &PromptComposer{workDir: workDir}
}

// Build 将内置核心指令、工作区 AGENTS.md 和传入的 Skill 目录组合成系统消息，
// 同时返回 Skill Prompt 的收录、截断和省略统计。
func (c *PromptComposer) Build(snapshot *skills.Snapshot) (ai.Message, skills.PromptReport, error) {
	agentsInstructions, err := c.loadAgentsInstructions()
	if err != nil {
		return ai.Message{}, skills.PromptReport{}, err
	}

	var builder strings.Builder
	builder.WriteString(corePrompt)
	builder.WriteString("\n# Agent 定义（来自 AGENTS.md）\n\n")
	builder.Write(agentsInstructions)
	builder.WriteString("\n")

	skillPrompt, report := skills.RenderPrompt(snapshot)
	if skillPrompt != "" {
		builder.WriteString(skillPrompt)
	}

	return ai.Message{
		Role:    ai.RoleSystem,
		Content: []ai.ContentBlock{ai.TextBlock(builder.String())},
	}, report, nil
}

func (c *PromptComposer) loadAgentsInstructions() ([]byte, error) {
	if c == nil || strings.TrimSpace(c.workDir) == "" {
		return nil, fmt.Errorf("%w: workDir is required", ErrInvalid)
	}
	root, err := os.OpenRoot(c.workDir)
	if err != nil {
		return nil, fmt.Errorf("%w: open workDir: %w", ErrInvalid, err)
	}
	defer root.Close()

	content, err := readRootRegularFile(root, "AGENTS.md")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: AGENTS.md is required", ErrInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read AGENTS.md: %w", ErrInvalid, err)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, fmt.Errorf("%w: AGENTS.md must be valid UTF-8 text", ErrInvalid)
	}
	if strings.TrimSpace(string(content)) == "" {
		return nil, fmt.Errorf("%w: AGENTS.md must not be empty", ErrInvalid)
	}
	return content, nil
}

func readRootRegularFile(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return root.ReadFile(name)
}
