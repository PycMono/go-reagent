package observability

// pi 的 Span 与 Retry Event 辅助函数。
//
// Span 创建统一走 go-context-sdk/tracing 的 StartSpan（SDK 迁移设计：
// StartSpan 是唯一 Span 创建 Helper，统一 instrumentation scope）；
// 属性补充统一走 KV/WithKV。全局 Provider 未安装时全部安全 Noop，
// 因此 pi 不需要自持有的门面、开关判断或 Fx 注入。

import (
	"context"
	"errors"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ClassifyError 是 contexttracing.WithSpan 的项目错误分类器（§4.9）：
// 返回 pierrors 稳定错误码，绝不返回错误正文。
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	return string(pierrors.ErrorCodeOf(err))
}

// ErrorFields 返回失败操作的 error.type / reagent.error.code 属性（§4.9）；
// 函数作用域 Span 在 fn 返回错误前经 WithKV 写入。
func ErrorFields(err error) []contexttracing.Field {
	if err == nil {
		return nil
	}
	code := pierrors.ErrorCodeOf(err)
	fields := []contexttracing.Field{contexttracing.KV(AttrErrorType, string(code))}
	if code != pierrors.ErrorCodeUnknown {
		fields = append(fields, contexttracing.KV(AttrReagentErrorCode, string(code)))
	}
	return fields
}

// SpanError 按 §4.9 结束失败 Span：Error Status 描述使用稳定错误码，
// error.type 记录 OTel 标准错误分类（本项目取 pierrors 稳定码），
// 可映射时同时记录 reagent.error.code；禁止写错误正文。
//
// 仅用于生命周期跨多次调用的流式 Span（TracingProvider）；函数作用域
// Span 统一使用 contexttracing.WithSpan + ClassifyError。
func SpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.SetStatus(codes.Error, ClassifyError(err))
	span.SetAttributes(ErrorFields(err)...)
}

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

// ---------- Retry Wait Event（§4.8） ----------
//
// Retry 等待在所属 Generate Span 上记录 Event，不创建 retry_sleep Span。

// RecordRetryScheduled 在等待启动前记录 reagent.retry.scheduled。
func RecordRetryScheduled(ctx context.Context, nextAttempt int, delay time.Duration, reason string) {
	trace.SpanFromContext(ctx).AddEvent(EventRetryScheduled,
		trace.WithAttributes(
			contexttracing.KV(AttrRetryNextAttempt, nextAttempt),
			contexttracing.KV(AttrRetryDelayMS, delay.Milliseconds()),
			contexttracing.KV(AttrRetryReason, reason),
		))
}

// RecordRetryCompleted 在 Timer 正常到期（下一次尝试开始）时记录。
func RecordRetryCompleted(ctx context.Context, nextAttempt int, actualDelay time.Duration) {
	trace.SpanFromContext(ctx).AddEvent(EventRetryCompleted,
		trace.WithAttributes(
			contexttracing.KV(AttrRetryNextAttempt, nextAttempt),
			contexttracing.KV(AttrRetryActualDelayMS, actualDelay.Milliseconds()),
		))
}

// RecordRetryCanceled 在等待被 Context 取消或超时时记录。
func RecordRetryCanceled(ctx context.Context, nextAttempt int, actualDelay time.Duration, err error) {
	reason := RetryCancelContextCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		reason = RetryCancelDeadlineExceeded
	}
	trace.SpanFromContext(ctx).AddEvent(EventRetryCanceled,
		trace.WithAttributes(
			contexttracing.KV(AttrRetryNextAttempt, nextAttempt),
			contexttracing.KV(AttrRetryActualDelayMS, actualDelay.Milliseconds()),
			contexttracing.KV(AttrRetryCancelReason, string(reason)),
		))
}
