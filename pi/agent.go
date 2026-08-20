package pi

import (
	"context"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
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
func (a *Agent) Run(ctx context.Context, request RunRequest, reporter Reporter) (RunResult, error) {
	result := RunResult{}
	fail := func(err error) (RunResult, error) {
		result.Termination = terminationFromError(err, RunTotals{})
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := request.Validate(); err != nil {
		return fail(err)
	}

	runContext, err := a.prepareRunContext(ctx, request)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	governor := newRunGovernor(request.Limits)
	loopResult, err := a.loop.runDetailed(ctx, runContext, reporter, governor)
	result.NewMessages = loopResult.newMessages
	result.Invocations = append([]ModelInvocation(nil), loopResult.invocations...)
	result.Termination = governor.termination(err)
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
