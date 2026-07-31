package context

import (
	"context"
	"errors"
	"fmt"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/schema"
)

// RunContext is the prepared message history and tool snapshot for one Agent run.
type RunContext struct {
	Messages []schema.Message
	Tools    []schema.ToolDefinition
}

// RunContextFactory prepares workspace-specific context for one Agent run.
type RunContextFactory struct {
	composer    *PromptComposer
	skillLoader *SkillLoader
}

// NewRunContextFactory creates a preparation boundary from workspace prompt and Skill components.
func NewRunContextFactory(composer *PromptComposer, skillLoader *SkillLoader) *RunContextFactory {
	return &RunContextFactory{composer: composer, skillLoader: skillLoader}
}

// Create discovers the current Skill snapshot and constructs initial system/user messages.
func (f *RunContextFactory) Create(
	ctx context.Context,
	userPrompt string,
	definitions []schema.ToolDefinition,
) (RunContext, error) {
	if ctx == nil {
		return RunContext{}, errors.New("run context: context is required")
	}
	if f == nil || f.composer == nil || f.skillLoader == nil {
		return RunContext{}, errors.New("run context: composer and skill loader are required")
	}
	if err := ctx.Err(); err != nil {
		return RunContext{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	snapshot, err := f.skillLoader.Discover(DefaultSkillEnvironment())
	if err != nil {
		return RunContext{}, fmt.Errorf("发现 Agent Skills 失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return RunContext{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}
	logSkillDiagnostics(ctx, snapshot.Diagnostics())
	if len(snapshot.Skills()) > 0 && !hasToolDefinition(definitions, "read") {
		return RunContext{}, errors.New("发现可用 Agent Skills，但 Registry 未挂载 read")
	}

	systemMessage, promptReport := f.composer.Build(snapshot)
	if promptReport.Truncated {
		logsdk.Warn(ctx, "[Context] Agent Skill Prompt 已截断",
			logsdk.Any("component", "context"),
			logsdk.Any("code", "skill_prompt_truncated"),
			logsdk.Any("included_skills", promptReport.IncludedSkills),
			logsdk.Any("omitted_skills", promptReport.OmittedSkills),
			logsdk.Any("shortened_descriptions", promptReport.ShortenedDescriptions),
		)
	}
	return RunContext{
		Messages: []schema.Message{
			systemMessage,
			{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock(userPrompt)}},
		},
		Tools: append([]schema.ToolDefinition(nil), definitions...),
	}, nil
}

func hasToolDefinition(definitions []schema.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func logSkillDiagnostics(ctx context.Context, diagnostics []SkillDiagnostic) {
	for _, diagnostic := range diagnostics {
		fields := []logsdk.Fields{
			logsdk.Any("component", "context"),
			logsdk.Any("code", diagnostic.Code),
			logsdk.Any("path", diagnostic.Path),
			logsdk.Any("severity", diagnostic.Severity),
			logsdk.Any("detail", diagnostic.Message),
		}
		switch diagnostic.Severity {
		case DiagnosticSeverityError:
			logsdk.Error(ctx, "[Context] Agent Skill 诊断", fields...)
		case DiagnosticSeverityWarning:
			logsdk.Warn(ctx, "[Context] Agent Skill 诊断", fields...)
		default:
			logsdk.Info(ctx, "[Context] Agent Skill 诊断", fields...)
		}
	}
}
