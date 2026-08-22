package observability

import (
	"context"
	"errors"
	"sync"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/pi/ai"
	"go.opentelemetry.io/otel/trace"
)

// errStreamAbandoned 表示流在 Result 之前被放弃（§5 abandoned 路径）；
// 只用于错误分类，正文不进入 Span 或 Metrics。
var errStreamAbandoned = errors.New("observability: stream abandoned before result")

// streamTimingReader 是 CostTracker 暴露 TTFT Snapshot 的包内私有接口（§5）；
// 不修改公共 ai.Stream 接口。自定义 Provider 缺少该接口时 TracingProvider
// 为 Trace 本地兜底，但不得回写 Usage/Ledger。
type streamTimingReader interface {
	StreamTTFT() (time.Duration, bool)
}

// TracingProvider 为每次物理 Provider 请求创建 CLIENT Span（§4.5），
// 装饰顺序固定为 Loop → TracingProvider → CostTracker → Raw Provider（§5）。
// Span 创建经 go-context-sdk StartSpan（全局 Provider 未安装时 Noop），
// 指标经 go-observability-sdk 包级 API（默认 Manager 未安装时 Noop）。
type TracingProvider struct {
	next       ai.Provider
	provider   string // gen_ai.provider.name（协议名，如 openai/anthropic）
	platformID string // Metrics provider Label，与 Ledger Usage.PlatformID 一致
	model      string
	now        func() time.Time
}

// NewTracingProvider 包装 next。provider 取协议名，platformID 取平台 ID。
func NewTracingProvider(next ai.Provider, provider, platformID, model string) *TracingProvider {
	return &TracingProvider{
		next:       next,
		provider:   provider,
		platformID: platformID,
		model:      model,
		now:        time.Now,
	}
}

// Stream 创建 Provider Span；Span 持续到 Result 或 Close（§5），
// 不能在 Stream 返回时结束。
func (p *TracingProvider) Stream(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) ai.Stream {
	hint, hasHint := GenerationHintFrom(ctx)
	spanCtx, span := contexttracing.StartSpan(ctx, ChatSpanName(p.model),
		trace.WithSpanKind(trace.SpanKindClient))
	fields := []contexttracing.Field{
		contexttracing.OperationName("chat"),
		contexttracing.ProviderName(p.provider),
		contexttracing.RequestModel(p.model),
		contexttracing.KV(AttrGenerationPhase, hint.Phase),
		contexttracing.KV(AttrProviderAttempt, hint.Attempt),
	}
	if hasHint {
		fields = append(fields, contexttracing.KV(AttrProviderRequestIndex, int(hint.RequestIndex)))
	}
	contexttracing.WithKV(spanCtx, fields...)
	return &tracingStream{
		ctx:       spanCtx,
		span:      span,
		next:      p.next.Stream(spanCtx, messages, tools),
		provider:  p,
		phase:     GenerationPhase(hint.Phase),
		startedAt: p.now(),
	}
}

type tracingStream struct {
	ctx       context.Context
	span      trace.Span
	next      ai.Stream
	provider  *TracingProvider
	phase     GenerationPhase
	startedAt time.Time

	current     ai.StreamEvent
	chunks      int
	ttftWritten bool
	resolveOnce sync.Once
}

func (s *tracingStream) Next() bool {
	if !s.next.Next() {
		return false
	}
	s.current = s.next.Current()
	s.chunks++
	if !s.ttftWritten && s.current.Type == ai.StreamEventTextDelta && s.current.TextDelta != "" {
		s.writeTTFT()
	}
	return true
}

// writeTTFT 把 CostTracker 的同一 TTFT Snapshot 写入 Span 和 Histogram（§5）；
// 标准链路禁止第二套 TTFT 计时，仅在下游缺少 streamTimingReader 时本地兜底。
func (s *tracingStream) writeTTFT() {
	s.ttftWritten = true
	ttft, ok := ttftOf(s.next)
	if !ok {
		ttft = s.provider.now().Sub(s.startedAt)
	}
	contexttracing.WithKV(s.ctx, contexttracing.KV(AttrStreamTTFTMS, ttft.Milliseconds()))
	RecordModelTTFT(s.ctx, s.provider.platformID, s.provider.model, s.phase, ttft)
}

func ttftOf(stream ai.Stream) (time.Duration, bool) {
	reader, ok := stream.(streamTimingReader)
	if !ok {
		return 0, false
	}
	return reader.StreamTTFT()
}

func (s *tracingStream) Current() ai.StreamEvent { return s.current }

func (s *tracingStream) Result() (*ai.Message, error) {
	message, err := s.next.Result()
	s.resolveOnce.Do(func() { s.finish(message, err) })
	return message, err
}

// Close 始终关闭下层 Stream；未调用 Result 时标记 abandoned/canceled 并
// 结束 Span（§5）。sync.Once 保证正常完成、错误、取消、超时、提前 Close
// 和重复 Result 均只结束一次。
func (s *tracingStream) Close() error {
	err := s.next.Close()
	s.resolveOnce.Do(func() {
		abandonErr := s.ctx.Err()
		if abandonErr == nil {
			abandonErr = errStreamAbandoned
		}
		s.finish(nil, abandonErr)
	})
	return err
}

func (s *tracingStream) finish(message *ai.Message, err error) {
	defer s.span.End()
	p := s.provider
	contexttracing.WithKV(s.ctx, contexttracing.KV(AttrStreamChunkCount, s.chunks))
	// 每次物理请求恰好记录一次 requests 与 gen_ai 操作时延。
	RecordModelRequest(s.ctx, p.platformID, p.model, s.phase, err)
	RecordGenAIClientOperation(s.ctx, p.provider, p.model, p.now().Sub(s.startedAt), err)
	if err != nil {
		// 失败请求只保留 Span、耗时、Attempt、Request Index 和错误类型（§4.5）。
		SpanError(s.span, err)
		return
	}
	if message == nil || message.Usage == nil {
		return
	}
	// 只有可信 Usage 才写 Token 和成本。
	usage := message.Usage
	fields := []contexttracing.Field{
		contexttracing.InputTokens(int(usage.InputTokens)),
		contexttracing.OutputTokens(int(usage.OutputTokens)),
		contexttracing.KV(AttrInvocationCostUSD, usage.CostUSD),
		contexttracing.KV(AttrUsageCacheReadTokens, usage.CacheReadTokens),
		contexttracing.KV(AttrUsageCacheWriteTokens, usage.CacheWriteTokens),
		contexttracing.KV(AttrUsageReasoningTokens, usage.ReasoningTokens),
	}
	if message.FinishReason != "" {
		fields = append(fields, contexttracing.FinishReasons(string(message.FinishReason)))
	}
	contexttracing.WithKV(s.ctx, fields...)
	RecordGenAITokenUsage(s.ctx, p.provider, p.model, usage.InputTokens, usage.OutputTokens)
}
