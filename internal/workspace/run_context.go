package workspace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

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
	request agent.RunRequest,
	definitions []ai.ToolDefinition,
) (agent.RunContext, error) {
	if !hasToolDefinition(definitions, "read") {
		return agent.RunContext{}, errors.New("agent runtime: required tool read is not registered")
	}

	snapshot, err := f.skillLoader.Discover(DefaultSkillEnvironment())
	if err != nil {
		return agent.RunContext{}, fmt.Errorf("发现 Agent Skills 失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return agent.RunContext{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	logSkillDiagnostics(ctx, snapshot.Diagnostics())
	if len(snapshot.Skills()) == 0 {
		return agent.RunContext{}, fmt.Errorf("%w: at least one eligible Skill is required", ErrInvalid)
	}

	systemMessage, promptReport, err := f.composer.Build(snapshot)
	if err != nil {
		return agent.RunContext{}, err
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
	contextBlocks := append([]agent.ContextBlock(nil), request.Context...)
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

	return agent.RunContext{
		Messages: messages,
		Tools:    append([]ai.ToolDefinition(nil), definitions...),
		Metadata: cloneMetadata(request.Metadata),
	}, nil
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func hasToolDefinition(definitions []ai.ToolDefinition, name string) bool {
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
