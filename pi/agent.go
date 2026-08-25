package pi

import (
	"context"
	"fmt"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Runner 定义无状态 Agent 的单次运行行为。
type Runner interface {
	Run(context.Context, RunRequest, EventListener) (RunResult, error)
}

// Agent 是可复用的无状态运行入口。
type Agent struct {
	builder     *harness.ContextBuilder
	loop        *Loop
	toolRuntime toolexec.Executor
	notifiers   []Notifier
}

// New 根据下层运行依赖创建 Agent；notifiers 为可选的外部通知通道
// （group:"agent_notifiers"），空切片表示无通知。
func New(builder *harness.ContextBuilder, loop *Loop, toolRuntime toolexec.Executor, notifiers ...Notifier) *Agent {
	return &Agent{builder: builder, loop: loop, toolRuntime: toolRuntime, notifiers: notifiers}
}

// Run 校验并执行一次相互隔离的请求。
//
// invoke_agent Span在本函数创建：经过 Chat 服务时是
// conversation.run 的子 Span；直接 SDK 调用时自然成为根 Span。
// Span 状态与生命周期由 WithSpan 管理。
func (a *Agent) Run(ctx context.Context, request RunRequest, listener EventListener) (result RunResult, err error) {
	startedAt := time.Now()
	err = contexttracing.WithSpan(ctx, observability.AgentSpanName(observability.AgentName), func(ctx context.Context) (runErr error) {
		defer func() {
			// 终止原因与 governor.Totals 无论成败都写入。
			reason := string(result.Termination.Reason)
			if reason == "" {
				reason = string(governor.TerminationError)
			}
			fields := []contexttracing.Field{
				contexttracing.OperationName("invoke_agent"),
				contexttracing.KV(observability.AttrGenAIAgentName, observability.AgentName),
				contexttracing.KV(observability.AttrTerminationReason, reason),
				contexttracing.KV(observability.AttrRunTurns, result.Termination.Totals.Turns),
				contexttracing.KV(observability.AttrRunInvocations, int(result.Termination.Totals.Invocations)),
				contexttracing.KV(observability.AttrRunTotalTokens, result.Termination.Totals.TotalTokens),
				contexttracing.KV(observability.AttrRunCostUSD, result.Termination.Totals.CostUSD),
			}
			fields = append(fields, observability.ErrorFields(runErr)...)
			contexttracing.WithKV(ctx, fields...)
			observability.RecordAgentRun(ctx, reason, time.Since(startedAt))
			observability.RecordAgentRunShape(ctx, result.Termination.Totals.Turns, int(result.Termination.Totals.Invocations))
		}()

		fail := func(failErr error) error {
			result.Termination = governor.TerminationFromError(failErr, governor.Totals{})
			return failErr
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := request.Validate(); err != nil {
			return fail(err)
		}

		var runContext harness.Context
		prepErr := contexttracing.WithSpan(ctx, observability.SpanNamePrepareContext, func(prepCtx context.Context) error {
			var err error
			runContext, err = a.prepareRunContext(prepCtx, request)
			if err != nil {
				contexttracing.WithKV(prepCtx, observability.ErrorFields(err)...)
			}
			return err
		}, contexttracing.WithErrorClassifier(observability.ClassifyError))
		if prepErr != nil {
			return fail(prepErr)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}

		gov := governor.New(request.Limits)
		if listener == nil {
			listener = nopListener{}
		}
		if len(a.notifiers) > 0 {
			// 告警监听排在调用方 EventListener 之后：SSE 等实时订阅优先，
			// 通道 panic 由 MultiEventListener/notifySafely 双层兜底。
			listener = NewMultiEventListener([]ListenerRegistration{
				{Name: "caller", Order: 0, Listener: listener},
				{Name: "alerts", Order: 100, Listener: &alertListener{notifiers: a.notifiers}},
			})
		}
		loopResult, runErr := a.loop.run(ctx, runContext, listener, gov)
		result.NewMessages = loopResult.newMessages
		result.Invocations = append([]governor.Invocation(nil), loopResult.invocations...)
		result.Termination = gov.Termination(runErr)
		return runErr
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	// 在 WithSpan 之外告警：覆盖 prepare 失败等 loop 之前的提前返回路径
	//（fail() 已正确设置 result.Termination）。
	a.notifyTermination(ctx, result.Termination)
	return result, err
}

// notifyTermination 把异常终止翻译为运行告警：错误/超时/预算终止告警，
// 正常完成与用户主动取消不告警。Summary 只含终止原因与累计用量，不含
// 消息正文或错误细节，告警通道可直接转发。
func (a *Agent) notifyTermination(ctx context.Context, termination governor.Termination) {
	if len(a.notifiers) == 0 {
		return
	}
	var notification Notification
	switch termination.Reason {
	case governor.TerminationError, governor.TerminationDeadline, governor.TerminationLoopDetected:
		notification = Notification{
			Kind:    NotificationRunError,
			Summary: fmt.Sprintf("Agent 运行异常终止（%s）", termination.Reason),
		}
	case governor.TerminationMaxTurns, governor.TerminationMaxCost, governor.TerminationMaxTotalTokens:
		notification = Notification{
			Kind: NotificationRunLimit,
			Summary: fmt.Sprintf("Agent 运行触发预算上限（%s）：%d 轮 / %d 次调用 / %d tokens / $%.6f",
				termination.Reason, termination.Totals.Turns, termination.Totals.Invocations,
				termination.Totals.TotalTokens, termination.Totals.CostUSD),
		}
	default:
		return
	}
	for _, notifier := range a.notifiers {
		notifySafely(ctx, notifier, notification)
	}
}

func (a *Agent) prepareRunContext(ctx context.Context, request RunRequest) (harness.Context, error) {
	history := make([]ai.Message, len(request.History))
	for index, message := range request.History {
		converted, err := message.Message2AI()
		if err != nil {
			return harness.Context{}, fmt.Errorf("history message %d: %w", index, err)
		}
		history[index] = converted
	}
	input, err := request.Input.Message2AI()
	if err != nil {
		return harness.Context{}, fmt.Errorf("input: %w", err)
	}

	if input.Role != ai.RoleUser {
		return harness.Context{}, fmt.Errorf("%w: input sender type must be customer", pierrors.ErrRequestInvalid)
	}
	blocks := make([]harness.ContextBlock, len(request.Context))
	for index, block := range request.Context {
		blocks[index] = harness.ContextBlock{Name: block.Name, Content: block.Content, Priority: block.Priority}
	}

	prepared, err := a.builder.Build(ctx, harness.ContextRequest{
		History: history,
		Input:   input,
		Context: blocks,
	}, a.toolRuntime.Definitions())
	if err != nil {
		return harness.Context{}, err
	}

	return prepared, nil
}
