package context

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/PycMono/go-reagent/internal/schema"
)

const corePrompt = `# 核心身份
你名叫 go-reagent，是一名经验丰富、注重事实与简洁表达的研发助手。你可以通过当前请求实际提供的工具定义读取、修改和检查工作区内容。

# 核心纪律 (CRITICAL)
1. 只能调用当前请求中实际提供定义的工具，不得虚构或模拟工具调用。
2. 当没有提供工具定义时，你正处于 Thinking 阶段：只能制定计划，不得声称工具已执行，也不得编造文件内容。
3. 修改文件前必须先读取并理解现有内容。
4. 工具执行失败时，应根据真实错误信息修正操作后重试。
5. 获得真实工具结果后，必须以这些 Observation 为依据完成面向用户的回答。
6. 始终使用中文回复，以便清晰传达进展和结论。
`

// PromptComposer builds one System Prompt from the current workspace state.
type PromptComposer struct {
	workDir     string
	skillLoader *SkillLoader
}

// NewPromptComposer creates a workspace-scoped Prompt Composer.
func NewPromptComposer(workDir string) *PromptComposer {
	return &PromptComposer{
		workDir:     workDir,
		skillLoader: NewSkillLoader(workDir),
	}
}

// Build composes the core instructions, AGENTS.md, and valid Agent Skills.
func (c *PromptComposer) Build() schema.Message {
	var builder strings.Builder
	builder.WriteString(corePrompt)

	agentsPath := filepath.Join(c.workDir, "AGENTS.md")
	if content, err := os.ReadFile(agentsPath); err == nil {
		builder.WriteString("\n# 项目专属指南 (来自 AGENTS.md)\n")
		builder.WriteString("以下是当前工作区特有的架构规范与注意事项，你的行为必须符合以下要求：\n")
		builder.WriteString("```markdown\n")
		_, _ = builder.Write(content)
		builder.WriteString("\n```\n")
	}

	if skills := c.skillLoader.LoadAll(); skills != "" {
		builder.WriteString(skills)
	}

	return schema.Message{
		Role:    schema.RoleSystem,
		Content: builder.String(),
	}
}
