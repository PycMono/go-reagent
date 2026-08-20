package harness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/skills"
)

// ContextBlock is caller-provided context injected before conversation history.
type ContextBlock struct {
	Name     string
	Content  string
	Priority int
}

// ContextRequest contains the caller-owned values required to build one run context.
type ContextRequest struct {
	History []ai.Message
	Input   ai.Message
	Context []ContextBlock
}

// Context contains the prepared message and tool snapshot returned to Agent Core.
type Context struct {
	Messages []ai.Message
	Tools    ai.ToolDefinitions
	// CurrentInputIndex 是 request.Input 在 Messages 中的位置，
	// 供压缩识别本次真实用户输入（不能用 RoleUser 猜测）。
	CurrentInputIndex int
}

// ContextBuilder prepares workspace-specific context for one Agent run.
type ContextBuilder struct {
	composer *PromptComposer
	workDir  string
}

// NewContextBuilder creates a preparation boundary from a workspace prompt composer and directory.
func NewContextBuilder(composer *PromptComposer, workDir string) *ContextBuilder {
	return &ContextBuilder{composer: composer, workDir: workDir}
}

// Build discovers the current Skill snapshot and constructs initial system/user messages.
func (f *ContextBuilder) Build(
	ctx context.Context,
	request ContextRequest,
	definitions ai.ToolDefinitions,
) (Context, error) {
	snapshot, err := skills.Discover(f.workDir)
	if err != nil {
		return Context{}, fmt.Errorf("%w: 发现 Agent Skills 失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return Context{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	logSkillDiagnostics(ctx, snapshot.Diagnostics())
	if !snapshot.Empty() && !definitions.Has("read") {
		return Context{}, errors.New("agent runtime: required tool read is not registered")
	}

	systemMessage, promptReport, err := f.composer.Build(snapshot)
	if err != nil {
		return Context{}, err
	}

	if promptReport.Truncated {
		logsdk.Warn(ctx, "[Context] Agent Skill Prompt 已截断",
			logsdk.Any("component", "context"),
			logsdk.Any("code", "skill_prompt_truncated"),
			logsdk.Any("included_skills", promptReport.IncludedSkills),
			logsdk.Any("omitted_skills", promptReport.OmittedSkills),
			logsdk.Any("shortened_descriptions", promptReport.ShortenedDescriptions),
		)
	}

	messages := make([]ai.Message, 0, 2+len(request.Context)+len(request.History))
	messages = append(messages, systemMessage)
	contextBlocks := append([]ContextBlock(nil), request.Context...)
	sort.SliceStable(contextBlocks, func(i, j int) bool {
		return contextBlocks[i].Priority > contextBlocks[j].Priority
	})

	for _, block := range contextBlocks {
		messages = append(messages, ai.Message{
			Role: ai.RoleSystem,
			Content: []ai.ContentBlock{ai.TextBlock(
				"# Context: " + strings.TrimSpace(block.Name) + "\n" + block.Content,
			)},
		})
	}
	messages = append(messages, append([]ai.Message(nil), request.History...)...)
	messages = append(messages, request.Input)

	return Context{
		Messages:          messages,
		Tools:             append(ai.ToolDefinitions(nil), definitions...),
		CurrentInputIndex: len(messages) - 1,
	}, nil
}

func logSkillDiagnostics(ctx context.Context, diagnostics []skills.Diagnostic) {
	for _, diagnostic := range diagnostics {
		fields := []logsdk.Fields{
			logsdk.Any("component", "context"),
			logsdk.Any("code", diagnostic.Code),
			logsdk.Any("path", diagnostic.Path),
			logsdk.Any("severity", diagnostic.Severity),
			logsdk.Any("detail", diagnostic.Message),
		}
		switch diagnostic.Severity {
		case skills.SeverityWarning:
			logsdk.Warn(ctx, "[Context] Agent Skill 诊断", fields...)
		default:
			logsdk.Info(ctx, "[Context] Agent Skill 诊断", fields...)
		}
	}
}
