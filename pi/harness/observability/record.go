package observability

import (
	"context"
	"errors"
	"time"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// 本文件集中记录 Agent 领域指标（设计 §8；SDK 设计 §15：领域语义由产生
// 事实的模块集中定义）。所有记录经 go-observability-sdk 包级通用 Metrics
// API 写入当前默认 Manager——Runtime InstallGlobal 之前与 Shutdown 之后
// 默认 Manager 为 Noop，记录安全空转，因此 pi 不需要自建 Instrument 缓存、
// 门面对象或开关判断（SDK 设计 §6.4）。指标名称、Label、Bucket 与基数红线
// 等定义语义见 metrics.go。
//
// P0 与 reagent.model.ttft、reagent.tool.queue_duration 从阶段 2 起记录；
// 其余 P1 在阶段 5 启用记录。

// RecordAgentRun 记录一次 Run 结束（§8.1）。
func RecordAgentRun(ctx context.Context, terminationReason string, duration time.Duration) {
	labels := []sdkmetrics.Label{
		sdkmetrics.String(LabelAgent, AgentName),
		sdkmetrics.String(LabelTerminationReason, terminationReason),
	}
	sdkmetrics.Counter(ctx, MetricAgentRuns, 1, labels...)
	sdkmetrics.Timer(ctx, MetricAgentRunDuration, duration.Seconds(), labels...)
}

// RecordModelRequest 记录一次物理模型请求（§8.2）。
func RecordModelRequest(ctx context.Context, provider, model string, phase GenerationPhase, err error) {
	sdkmetrics.Counter(ctx, MetricModelRequests, 1,
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelOutcome, string(OutcomeOf(err))),
		sdkmetrics.String(LabelErrorCode, ErrorCodeLabel(err)),
	)
}

// RecordModelInvocation 记录一次可信 Usage 调用（§8.2）：invocations、
// cost、tokens 只在此处各累加一次。cache_read/cache_write/reasoning 是
// 子集口径（§9.1），只在非零时记录，不能与总量全部求和。
func RecordModelInvocation(ctx context.Context, provider, model string, phase GenerationPhase, acceptance Acceptance, costUSD float64, costQuality CostQuality, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens int64) {
	sdkmetrics.Counter(ctx, MetricModelInvocations, 1,
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelAcceptance, string(acceptance)),
	)
	sdkmetrics.Counter(ctx, MetricModelCost, costUSD,
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelCostQuality, string(costQuality)),
	)
	sdkmetrics.Counter(ctx, MetricModelTokens, float64(inputTokens),
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelTokenType, string(TokenTypeInputTotal)),
	)
	sdkmetrics.Counter(ctx, MetricModelTokens, float64(outputTokens),
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelTokenType, string(TokenTypeOutputTotal)),
	)
	for _, subset := range []struct {
		tokenType TokenType
		tokens    int64
	}{
		{TokenTypeCacheRead, cacheReadTokens},
		{TokenTypeCacheWrite, cacheWriteTokens},
		{TokenTypeReasoning, reasoningTokens},
	} {
		if subset.tokens <= 0 {
			continue
		}
		sdkmetrics.Counter(ctx, MetricModelTokens, float64(subset.tokens),
			sdkmetrics.String(LabelProvider, provider),
			sdkmetrics.String(LabelModel, model),
			sdkmetrics.String(LabelPhase, string(phase)),
			sdkmetrics.String(LabelTokenType, string(subset.tokenType)),
		)
	}
}

// RecordModelRetry 在 Retry 等待调度时累加一次（§4.8：仅 scheduled 累加）。
func RecordModelRetry(ctx context.Context, provider, model string, phase GenerationPhase, reason string) {
	sdkmetrics.Counter(ctx, MetricModelRetries, 1,
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
		sdkmetrics.String(LabelReason, reason),
	)
}

// RecordContextOverflow 记录一次 Context Overflow（§8.2）。
func RecordContextOverflow(ctx context.Context, provider, model string, phase GenerationPhase) {
	sdkmetrics.Counter(ctx, MetricModelContextOverflows, 1,
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
	)
}

// RecordGenAIClientOperation 记录 semconv 标准 client 操作时延（§8.2）；
// 失败请求附带稳定 error.type，不记录错误正文。
func RecordGenAIClientOperation(ctx context.Context, providerName, requestModel string, duration time.Duration, err error) {
	labels := []sdkmetrics.Label{
		sdkmetrics.String(AttrGenAIOperationName, "chat"),
		sdkmetrics.String(AttrGenAIProviderName, providerName),
		sdkmetrics.String(AttrGenAIRequestModel, requestModel),
	}
	if err != nil {
		labels = append(labels, sdkmetrics.String(AttrErrorType, ErrorCodeLabel(err)))
	}
	sdkmetrics.Timer(ctx, MetricGenAIClientOperationDuration, duration.Seconds(), labels...)
}

// RecordGenAITokenUsage 记录 semconv 标准单次请求 Token 用量。
func RecordGenAITokenUsage(ctx context.Context, providerName, requestModel string, inputTokens, outputTokens int64) {
	common := []sdkmetrics.Label{
		sdkmetrics.String(AttrGenAIOperationName, "chat"),
		sdkmetrics.String(AttrGenAIProviderName, providerName),
		sdkmetrics.String(AttrGenAIRequestModel, requestModel),
	}
	sdkmetrics.Histogram(ctx, MetricGenAIClientTokenUsage, float64(inputTokens),
		append(common, sdkmetrics.String("gen_ai.token.type", "input"))...)
	sdkmetrics.Histogram(ctx, MetricGenAIClientTokenUsage, float64(outputTokens),
		append(common, sdkmetrics.String("gen_ai.token.type", "output"))...)
}

// RecordModelTTFT 记录 TTFT Histogram（秒），与 Span/Ledger 同源（§5）。
func RecordModelTTFT(ctx context.Context, provider, model string, phase GenerationPhase, ttft time.Duration) {
	sdkmetrics.Timer(ctx, MetricModelTTFT, ttft.Seconds(),
		sdkmetrics.String(LabelProvider, provider),
		sdkmetrics.String(LabelModel, model),
		sdkmetrics.String(LabelPhase, string(phase)),
	)
}

// RecordToolExecution 记录一次 Tool 执行（§8.3）。
func RecordToolExecution(ctx context.Context, tool string, err error, duration time.Duration) {
	outcome := OutcomeOf(err)
	sdkmetrics.Counter(ctx, MetricToolExecutions, 1,
		sdkmetrics.String(LabelTool, tool),
		sdkmetrics.String(LabelOutcome, string(outcome)),
		sdkmetrics.String(LabelErrorCode, ErrorCodeLabel(err)),
	)
	sdkmetrics.Timer(ctx, MetricToolDuration, duration.Seconds(),
		sdkmetrics.String(LabelTool, tool),
		sdkmetrics.String(LabelOutcome, string(outcome)),
	)
}

// RecordToolQueueDuration 记录 Tool 信号量排队时延（§8.3，阶段 2 交付）。
func RecordToolQueueDuration(ctx context.Context, tool string, mode ExecutionMode, queueErr error, wait time.Duration) {
	sdkmetrics.Timer(ctx, MetricToolQueueDuration, wait.Seconds(),
		sdkmetrics.String(LabelTool, tool),
		sdkmetrics.String(LabelExecutionMode, string(mode)),
		sdkmetrics.String(LabelOutcome, string(OutcomeOf(queueErr))),
	)
}

// RecordCompaction 记录一次上下文压缩（§8.4）。
func RecordCompaction(ctx context.Context, reason CompactionReason, err error) {
	sdkmetrics.Counter(ctx, MetricCompactions, 1,
		sdkmetrics.String(LabelReason, string(reason)),
		sdkmetrics.String(LabelOutcome, string(OutcomeOf(err))),
	)
}

// ---------- P1（阶段 5 启用，§8） ----------

// RecordAgentRunShape 记录每 Run 的 Turn/Invocation 分布（§8.1 P1）。
func RecordAgentRunShape(ctx context.Context, turns, invocations int) {
	sdkmetrics.Histogram(ctx, MetricAgentRunTurns, float64(turns),
		sdkmetrics.String(LabelAgent, AgentName))
	sdkmetrics.Histogram(ctx, MetricAgentRunInvocations, float64(invocations),
		sdkmetrics.String(LabelAgent, AgentName))
}

// RecordChatRun 由 Chat Service 记录一次业务 Run（§8.1 P1）。
func RecordChatRun(ctx context.Context, profile, transport, terminationReason string) {
	sdkmetrics.Counter(ctx, MetricChatRuns, 1,
		sdkmetrics.String(LabelProfile, profile),
		sdkmetrics.String(LabelTransport, transport),
		sdkmetrics.String(LabelTerminationReason, terminationReason),
	)
}

// RecordCompactionDetail 记录压缩时延与消息削减比例（§8.4 P1）。
// 削减比例只在 before > 0 时记录，并限制在 [0,1]。
func RecordCompactionDetail(ctx context.Context, reason CompactionReason, outcomeErr error, duration time.Duration, beforeMessageCount, afterMessageCount int) {
	sdkmetrics.Timer(ctx, MetricCompactionDuration, duration.Seconds(),
		sdkmetrics.String(LabelReason, string(reason)),
		sdkmetrics.String(LabelOutcome, string(OutcomeOf(outcomeErr))),
	)
	if beforeMessageCount > 0 {
		ratio := 1 - float64(afterMessageCount)/float64(beforeMessageCount)
		ratio = max(0, min(1, ratio))
		sdkmetrics.Histogram(ctx, MetricCompactionMessageReduction, ratio,
			sdkmetrics.String(LabelReason, string(reason)))
	}
}

// ---------- 错误分类 → Metrics Label ----------

// OutcomeOf 把一次操作结果归类为 §8.2 的 outcome Label；
// Context Cancel 与 Deadline 必须区分（§4.9）。
func OutcomeOf(err error) RequestOutcome {
	switch {
	case err == nil:
		return RequestOutcomeSuccess
	case errors.Is(err, context.Canceled):
		return RequestOutcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return RequestOutcomeDeadlineExceeded
	default:
		return RequestOutcomeError
	}
}

// ErrorCodeLabel 返回 Metrics 的 error_code Label：无错误时为 none，
// 其他情况取 pierrors 稳定错误码。
func ErrorCodeLabel(err error) string {
	if err == nil {
		return ErrorCodeNone
	}
	return string(pierrors.ErrorCodeOf(err))
}
