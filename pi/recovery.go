package pi

import (
	"context"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/avast/retry-go/v4"
)

const maxGenerateRetries = 2

type generationResult struct {
	message *ai.Message
	context []ai.Message
	// attempts 是本次逻辑生成的物理 Provider 请求次数（不含 Compaction 自身）。
	attempts int
	// compactionTriggered 表示本次逻辑生成中触发了 L2 Compaction。
	compactionTriggered bool
	// requestIndex 是最终成功响应对应的物理请求序号。
	requestIndex uint32
}

// generateState 是一次逻辑生成（Thinking 或 Action）的共享状态：
// Attempt 在 Overflow 恢复后的重试中保持连续；
// RequestIndex 经 compactionRuntime 在 Run 内单调递增。
type generateState struct {
	phase    observability.GenerationPhase
	rt       *compactionRuntime
	attempts int
	// compactionTriggered 由 recoverOverflow 在触发 L2 Compaction 时置位。
	compactionTriggered bool
	// lastRequestIndex 是最近一次物理请求的 Run 内序号。
	lastRequestIndex uint32
}

// generateWithSpan 为一次逻辑生成创建 reagent.generate Span；
// Span 覆盖 Retry、Overflow 恢复与可能的 Compaction 子 Span，状态与
// 生命周期由 WithSpan 管理。
//
// messages 是 Durable 真实历史（允许写回 generationResult.context、参与
// L1/L2 Compaction 并进入后续 turn）；ephemeral 只在每次物理 Provider 请求
// 前复制附加，不属于返回 Context——循环护栏的 warning 提醒经此通道投递。
// 合并只发生在此入口（generateWithRetry 之上）：Compaction 摘要由
// tryCompactOnce 直接调用 generateWithRetry，不经过本通道，摘要请求永不
// 携带 ephemeral。
func (l *Loop) generateWithSpan(
	ctx context.Context,
	phase observability.GenerationPhase,
	messages []ai.Message,
	ephemeral []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
	onCompactionUsage invocationObserver,
	rt *compactionRuntime,
) (result generationResult, err error) {
	err = contexttracing.WithSpan(ctx, observability.SpanNameGenerate, func(ctx context.Context) error {
		contexttracing.WithKV(ctx, contexttracing.KV(observability.AttrGenerationPhase, string(phase)))

		state := &generateState{phase: phase, rt: rt}
		var genErr error
		result, genErr = l.generate(ctx, state, messages, ephemeral, tools, onText, onCompactionUsage)

		outcome := observability.GenerationOutcomeSucceeded
		if genErr != nil {
			outcome = observability.GenerationOutcomeFailed
			switch pierrors.ErrorCodeOf(genErr) {
			case pierrors.ErrorCodeCanceled:
				outcome = observability.GenerationOutcomeCanceled
			case pierrors.ErrorCodeDeadlineExceeded:
				outcome = observability.GenerationOutcomeDeadlineExceeded
			}
		}
		fields := []contexttracing.Field{
			contexttracing.KV(observability.AttrGenerationAttempts, result.attempts),
			contexttracing.KV(observability.AttrGenerationOutcome, string(outcome)),
			contexttracing.KV(observability.AttrCompactionTriggered, result.compactionTriggered),
		}
		fields = append(fields, observability.ErrorFields(genErr)...)
		contexttracing.WithKV(ctx, fields...)
		return genErr
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return result, err
}

func (l *Loop) generateWithRetry(
	ctx context.Context,
	state *generateState,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
) (*ai.Message, bool, error) {
	var response *ai.Message
	var published bool
	waitingForRetry := false
	var scheduledAt time.Time
	err := retry.Do(func() error {
		state.attempts++
		if state.attempts > 1 {
			// 上一次 Retry 等待的 Timer 正常到期。
			observability.RecordRetryCompleted(ctx, state.attempts, time.Since(scheduledAt))
		}
		waitingForRetry = false
		requestIndex, seqErr := state.rt.nextRequestIndex()
		if seqErr != nil {
			return seqErr
		}
		state.lastRequestIndex = requestIndex
		hintCtx := observability.WithGenerationHint(ctx, observability.GenerationHint{
			Phase:        string(state.phase),
			Attempt:      state.attempts,
			RequestIndex: state.lastRequestIndex,
		})
		var err error
		response, published, err = consumeStream(l.provider.Stream(hintCtx, messages, tools), onText)
		return err
	},
		retry.Attempts(maxGenerateRetries+1),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
		retry.RetryIf(func(err error) bool {
			code := pierrors.ErrorCodeOf(err)
			return !published && (code == pierrors.ErrorCodeAITransient || code == pierrors.ErrorCodeAIRateLimited)
		}),
		retry.DelayType(func(attempt uint, _ error, _ *retry.Config) time.Duration {
			return retryDelay(int(attempt - 1))
		}),
		retry.OnRetry(func(attempt uint, err error) {
			if attempt >= maxGenerateRetries {
				return
			}

			waitingForRetry = true
			scheduledAt = time.Now()
			delay := retryDelay(int(attempt))
			reason := string(pierrors.ErrorCodeOf(err))
			// Retry Counter 仅在 scheduled 时累加一次。
			observability.RecordRetryScheduled(ctx, state.attempts+1, delay, reason)
			observability.RecordModelRetry(ctx,
				labelOrUnknown(l.providerID), labelOrUnknown(l.model), state.phase, reason)
			logsdk.Warn(ctx, "model generation retry",
				logsdk.Any("component", "model_recovery"),
				logsdk.Any("error_code", pierrors.ErrorCodeOf(err)),
				logsdk.Any("retry", attempt+1),
				logsdk.Any("delay_ms", delay.Milliseconds()),
			)
		}),
	)
	if waitingForRetry && ctx.Err() != nil {
		observability.RecordRetryCanceled(ctx, state.attempts+1, time.Since(scheduledAt), ctx.Err())
		return nil, false, ctx.Err()
	}

	return response, published, err
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

// generate 执行一次 Thinking 或 Action 调用。遇到 Context Overflow 且尚未
// 发布内容时，转交 recoverOverflow 走 reactive 压缩兜底。
//
// durable/ephemeral 双通道：Provider 看到 mergeMessages(durable, ephemeral)
// 的复制切片；返回的 generationResult.context 始终是 durable。
func (l *Loop) generate(
	ctx context.Context,
	state *generateState,
	messages []ai.Message,
	ephemeral []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
	onCompactionUsage invocationObserver,
) (generationResult, error) {
	response, published, err := l.generateWithRetry(ctx, state, mergeMessages(messages, ephemeral), tools, onText)
	if err == nil || published || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow {
		return generationResult{
			message: response, context: messages,
			attempts: state.attempts, requestIndex: state.lastRequestIndex,
		}, err
	}
	observability.RecordContextOverflow(ctx,
		labelOrUnknown(l.providerID), labelOrUnknown(l.model), state.phase)
	result, recoverErr := l.recoverOverflow(ctx, state, response, err, messages, ephemeral, tools, onText, onCompactionUsage)
	result.attempts = state.attempts
	result.compactionTriggered = state.compactionTriggered
	result.requestIndex = state.lastRequestIndex
	return result, recoverErr
}

// mergeMessages 返回 clone(durable) + ephemeral 的新切片；不修改 durable
// 或调用方历史的底层数组。ephemeral 为空时直接返回 durable（只读使用）。
func mergeMessages(durable, ephemeral []ai.Message) []ai.Message {
	if len(ephemeral) == 0 {
		return durable
	}
	merged := make([]ai.Message, 0, len(durable)+len(ephemeral))
	merged = append(merged, durable...)
	merged = append(merged, ephemeral...)
	return merged
}

func retryDelay(retry int) time.Duration {
	if retry == 0 {
		return 500 * time.Millisecond
	}
	return time.Second
}
