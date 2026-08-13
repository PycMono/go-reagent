package pi

import (
	"context"
	"fmt"
	"strings"

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
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateRunRequest(request); err != nil {
		return result, err
	}

	runContext, err := a.prepareRunContext(ctx, request)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	loopResult, err := a.loop.runDetailed(ctx, runContext, reporter)
	result.NewMessages = loopResult.newMessages
	result.Invocations = append([]ModelInvocation(nil), loopResult.invocations...)
	return result, err
}

func (a *Agent) prepareRunContext(ctx context.Context, request RunRequest) (RunContext, error) {
	history, err := historyMessagesToAI(request.History)
	if err != nil {
		return RunContext{}, err
	}
	blocks := make([]harness.ContextBlock, len(request.Context))
	for index, block := range request.Context {
		blocks[index] = harness.ContextBlock{Name: block.Name, Content: block.Content, Priority: block.Priority}
	}
	prepared, err := a.builder.Build(ctx, harness.ContextRequest{
		History: history,
		Input: ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock(request.Input)},
		},
		Context: blocks,
	}, a.toolRuntime.Definitions())
	if err != nil {
		return RunContext{}, err
	}
	return RunContext{Messages: prepared.Messages, Tools: prepared.Tools}, nil
}

func validateRunRequest(request RunRequest) error {
	if strings.TrimSpace(request.Input) == "" {
		return fmt.Errorf("%w: input content must not be empty", pierrors.ErrRequestInvalid)
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("%w: context block %d name must not be empty", pierrors.ErrRequestInvalid, index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("%w: context block %d content must not be empty", pierrors.ErrRequestInvalid, index)
		}
	}
	return nil
}
