package pi

import (
	"context"
	"fmt"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

// Runner 定义无状态 Agent 的单次运行行为。
type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}

// Agent 是可复用的无状态运行入口。
type Agent struct {
	builder     *harness.ContextBuilder
	loop        *Loop
	toolRuntime ToolRuntime
}

// New 根据下层运行依赖创建 Agent。
func New(builder *harness.ContextBuilder, loop *Loop, toolRuntime ToolRuntime) *Agent {
	return &Agent{builder: builder, loop: loop, toolRuntime: toolRuntime}
}

// Run 校验并执行一次相互隔离的请求。
//
// invoke_agent Span（§4.2）在本函数创建：经过 Chat 服务时是
// conversation.run 的子 Span；直接 SDK 调用时自然成为根 Span。
// Span 状态与生命周期由 WithSpan 管理。
func (a *Agent) Run(ctx context.Context, request RunRequest, reporter Reporter) (result RunResult, err error) {
	startedAt := time.Now()
	err = contexttracing.WithSpan(ctx, observability.AgentSpanName(observability.AgentName), func(ctx context.Context) (runErr error) {
		defer func() {
			// 终止原因与 RunTotals 无论成败都写入（§4.2）。
			reason := string(result.Termination.Reason)
			if reason == "" {
				reason = string(RunTerminationError)
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
			result.Termination = terminationFromError(failErr, RunTotals{})
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

		governor := newRunGovernor(request.Limits)
		if reporter == nil {
			reporter = nopReporter{}
		}
		loopResult, runErr := a.loop.runDetailed(ctx, runContext, reporter, governor)
		result.NewMessages = loopResult.newMessages
		result.Invocations = append([]ModelInvocation(nil), loopResult.invocations...)
		result.Termination = governor.termination(runErr)
		return runErr
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return result, err
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
