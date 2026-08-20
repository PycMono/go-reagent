package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

const maxGenerateRetries = 2

const compactionSystemPrompt = `请总结所提供的早期对话，以便另一个 Agent 继续完成同一任务。
请保留用户目标、明确约束、已确认的决策、已完成工作、待完成工作、准确的文件路径、标识符、工具执行结果和稳定错误码。
不要回答用户，也不要继续执行任务。`

type generationResult struct {
	message *ai.Message
	context []ai.Message
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
	onText func(ai.ContentBlock),
) (*ai.Message, bool, error) {
	for attempt := 0; ; attempt++ {
		response, published, err := consumeStream(l.provider.Stream(ctx, messages, tools), onText)
		if err == nil {
			return response, published, nil
		}
		code := pierrors.ErrorCodeOf(err)
		if published || attempt >= maxGenerateRetries ||
			(code != pierrors.ErrorCodeAITransient && code != pierrors.ErrorCodeAIRateLimited) {
			return response, published, err
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
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
}

func consumeStream(stream ai.Stream, onText func(ai.ContentBlock)) (*ai.Message, bool, error) {
	defer stream.Close()
	published := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case ai.StreamEventTextDelta:
			if onText != nil && event.TextDelta != "" {
				published = true
				onText(ai.TextBlock(event.TextDelta))
			}
		case ai.StreamEventDone, ai.StreamEventError:
			message, err := stream.Result()
			return message, published, err
		}
	}
	message, err := stream.Result()
	return message, published, err
}

// generate 执行一次 Thinking 或 Action 调用。遇到 Context Overflow 时会先
// Compaction；onCompactionUsage 在摘要 Invocation 成功后、重试原请求之前
// 调用，用于记账和预算准入。预算达到时直接返回，不再重试原请求。
func (l *Loop) generate(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
	onCompactionUsage func(ai.Usage) error,
) (generationResult, error) {
	response, published, err := l.generateWithRetry(ctx, messages, tools, onText)
	if err == nil || published || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow {
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
	if onCompactionUsage != nil {
		if observeErr := onCompactionUsage(*usage); observeErr != nil {
			return generationResult{context: messages}, observeErr
		}
	}
	response, _, err = l.generateWithRetry(ctx, compacted, tools, onText)
	return generationResult{
		message: response,
		context: compacted,
	}, err
}

func (l *Loop) compact(ctx context.Context, plan harness.CompactionPlan) ([]ai.Message, *ai.Usage, error) {
	encoded, err := json.Marshal(plan.SummaryMessages)
	if err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", fmt.Errorf("encode summary input: %w", err))
	}
	response, _, err := l.generateWithRetry(ctx, []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock(compactionSystemPrompt)}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(string(encoded))}},
	}, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := response.ValidateThinking(); err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", err)
	}
	text, err := ai.TextContent(response.Content)
	if err != nil {
		return nil, nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "context compaction", err)
	}
	usage := *response.Usage
	return harness.ApplySummary(plan, strings.TrimSpace(text)), &usage, nil
}

func recoveryHint(code pierrors.ErrorCode) string {
	switch code {
	case pierrors.ErrorCodeToolEditNoMatch:
		return "先使用 read 获取文件最新内容，再使用精确的 oldText 重新编辑。"
	case pierrors.ErrorCodeToolEditNotUnique:
		return "增加 oldText 的相邻代码，确保只匹配一处后再编辑。"
	case pierrors.ErrorCodeToolResourceNotFound:
		return "不要继续猜测路径；先检查真实目录结构和文件名。"
	default:
		return ""
	}
}

func toolRecoveryMessage(message ai.Message, code pierrors.ErrorCode) ai.Message {
	hint := recoveryHint(code)
	if hint == "" {
		return message
	}
	enhanced := message
	enhanced.Content = append([]ai.ContentBlock(nil), message.Content...)
	enhanced.Content = append(enhanced.Content, ai.TextBlock("\n\n[Recovery Hint]\n"+hint))
	return enhanced
}
