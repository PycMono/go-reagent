package context

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/schema"
)

// RunContext is the prepared message history and tool snapshot for one Agent run.
type RunContext struct {
	Messages []ai.Message
	Tools    []ai.ToolDefinition
	Metadata map[string]string
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
	request schema.RunRequest,
	definitions []ai.ToolDefinition,
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
	if err := validateRunRequest(request); err != nil {
		return RunContext{}, err
	}
	if !hasToolDefinition(definitions, "read") {
		return RunContext{}, errors.New("agent runtime: required tool read is not registered")
	}

	snapshot, err := f.skillLoader.Discover(DefaultSkillEnvironment())
	if err != nil {
		return RunContext{}, fmt.Errorf("发现 Agent Skills 失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return RunContext{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	logSkillDiagnostics(ctx, snapshot.Diagnostics())
	if len(snapshot.Skills()) == 0 {
		return RunContext{}, errors.New("agent workspace: at least one eligible Skill is required")
	}

	systemMessage, promptReport, err := f.composer.Build(snapshot)
	if err != nil {
		return RunContext{}, err
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
	contextBlocks := append([]schema.ContextBlock(nil), request.Context...)
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

	return RunContext{
		Messages: messages,
		Tools:    append([]ai.ToolDefinition(nil), definitions...),
		Metadata: cloneMetadata(request.Metadata),
	}, nil
}

func validateRunRequest(request schema.RunRequest) error {
	if request.Input.Role != ai.RoleUser {
		return fmt.Errorf("run context: input role must be user, got %q", request.Input.Role)
	}

	inputText, err := ai.TextContent(request.Input.Content)
	if err != nil {
		return fmt.Errorf("run context: input content: %w", err)
	}
	if strings.TrimSpace(inputText) == "" {
		return errors.New("run context: input content must not be empty")
	}

	if len(request.Input.ToolCalls) != 0 || request.Input.ToolCallID != "" ||
		request.Input.ToolName != "" || request.Input.IsError {
		return errors.New("run context: input must not contain tool fields")
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("run context: context block %d name must not be empty", index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("run context: context block %d content must not be empty", index)
		}
	}

	return nil
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
