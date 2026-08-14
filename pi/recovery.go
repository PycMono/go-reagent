package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

const maxGenerateRetries = 2

const compactionSystemPrompt = `Summarize the supplied earlier conversation for another agent to continue the same task.
Preserve user goals, explicit constraints, accepted decisions, completed work, pending work, exact file paths, identifiers, tool results, and stable error codes.
Do not answer the user and do not continue the task.`

type generationResult struct {
	message         *ai.Message
	context         []ai.Message
	compactionUsage *ai.Usage
}

func retryDelay(retry int) time.Duration {
	if retry == 0 {
		return 500 * time.Millisecond
	}
	return time.Second
}

func (l *Loop) generateWithRetry(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) (*ai.Message, error) {
	for attempt := 0; ; attempt++ {
		response, err := l.provider.Generate(ctx, messages, tools)
		if err == nil {
			return response, nil
		}
		code := pierrors.ErrorCodeOf(err)
		if attempt >= maxGenerateRetries ||
			(code != pierrors.ErrorCodeAITransient && code != pierrors.ErrorCodeAIRateLimited) {
			return response, err
		}
		delay := retryDelay(attempt)
		logsdk.Warn(ctx, "model generation retry",
			logsdk.Any("component", "model_recovery"),
			logsdk.Any("error_code", code),
			logsdk.Any("retry", attempt+1),
			logsdk.Any("delay_ms", delay.Milliseconds()),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *Loop) generate(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) (generationResult, error) {
	response, err := l.generateWithRetry(ctx, messages, tools)
	if err == nil || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow {
		return generationResult{message: response, context: messages}, err
	}

	plan, planErr := harness.BuildCompactionPlan(messages)
	if planErr != nil {
		return generationResult{message: response, context: messages}, err
	}
	compacted, usage, compactErr := l.compact(ctx, plan)
	if compactErr != nil {
		return generationResult{context: messages}, compactErr
	}
	response, err = l.generateWithRetry(ctx, compacted, tools)
	return generationResult{
		message:         response,
		context:         compacted,
		compactionUsage: usage,
	}, err
}

func (l *Loop) compact(ctx context.Context, plan harness.CompactionPlan) ([]ai.Message, *ai.Usage, error) {
	encoded, err := json.Marshal(plan.SummaryMessages)
	if err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", fmt.Errorf("encode summary input: %w", err))
	}
	response, err := l.generateWithRetry(ctx, []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock(compactionSystemPrompt)}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(string(encoded))}},
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	if response == nil {
		return nil, nil, pierrors.Wrap(
			pierrors.ErrorCodeAIGeneration,
			"context compaction",
			errors.New("provider returned an empty summary response"),
		)
	}
	if err := validateThinkingResponse(response); err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", err)
	}
	if err := validateMeteredUsage(response.Usage); err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction usage", err)
	}
	text, err := ai.TextContent(response.Content)
	if err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", err)
	}
	usage := *response.Usage
	return harness.ApplySummary(plan, strings.TrimSpace(text)), &usage, nil
}
