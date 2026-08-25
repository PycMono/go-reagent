package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// stubTool 是测试用的最小 ai.Tool 实现。
type stubTool struct {
	name    string
	execute func(ctx context.Context, args json.RawMessage, emit ai.UpdateEmitter) (ai.ToolOutput, error)
}

func (t *stubTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.name,
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t *stubTool) Execute(ctx context.Context, args json.RawMessage, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
	return t.execute(ctx, args, emit)
}

func newExecution(tool ai.Tool) *Execution {
	definition := tool.Definition()
	return &Execution{
		Ctx:        context.Background(),
		Call:       ai.ToolCall{ID: "call-1", Name: definition.Name, Arguments: json.RawMessage(`{}`)},
		Definition: definition,
		Tool:       tool,
	}
}

func okTool(name string) *stubTool {
	return &stubTool{
		name: name,
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
		},
	}
}

func runChain(e *Execution, handlers ...Handler) *Execution {
	e.Run(handlers)
	return e
}

// 中间件按切片顺序执行，且后置逻辑沿链折返（洋葱模型）。
func TestChainExecutesInSliceOrder(t *testing.T) {
	var order []string
	wrap := func(name string) Handler {
		return func(e *Execution) {
			order = append(order, name+":before")
			e.Next()
			order = append(order, name+":after")
		}
	}
	runChain(newExecution(okTool("t")), wrap("a"), wrap("b"), ExecuteTool)

	want := []string{"a:before", "b:before", "b:after", "a:after"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// Block 短路：后续 handler（含终端 ExecuteTool）不再执行。
func TestBlockSkipsRemainingHandlers(t *testing.T) {
	executed := false
	tool := &stubTool{
		name: "t",
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			executed = true
			return ai.ToolOutput{}, nil
		},
	}
	boom := errors.New("boom")
	blocking := func(e *Execution) {
		e.Block(boom)
	}
	reached := false
	after := func(e *Execution) {
		reached = true
		e.Next()
	}

	e := runChain(newExecution(tool), blocking, after, ExecuteTool)

	if !errors.Is(e.Err, boom) {
		t.Fatalf("Err = %v, want %v", e.Err, boom)
	}
	if reached {
		t.Fatal("handler after Block should not run")
	}
	if executed {
		t.Fatal("terminal ExecuteTool should not run after Block")
	}
}

// 语义锁定：handler 不调用 Next 直接返回，并不会阻止外层循环执行后续
// handler——短路必须显式 Block。
func TestReturnWithoutBlockDoesNotShortCircuit(t *testing.T) {
	reached := false
	silent := func(e *Execution) { /* 既不 Next 也不 Block */ }
	after := func(e *Execution) { reached = true }

	runChain(newExecution(okTool("t")), silent, after)

	if !reached {
		t.Fatal("handler returning without Block must not stop the outer Next loop")
	}
}

func TestPanicRecoveryConvertsPanicToError(t *testing.T) {
	panicking := func(e *Execution) { panic("kaboom") }
	reached := false
	after := func(e *Execution) { reached = true }

	e := runChain(newExecution(okTool("t")), PanicRecovery, panicking, after)

	if e.Err == nil {
		t.Fatal("Err should be set after panic")
	}
	if code := pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", e.Err)); code != pierrors.ErrorCodeToolPanic {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeToolPanic)
	}
	if reached {
		t.Fatal("handler after panicking handler should not run")
	}
}

func TestSchemaValidationBlocksOnInvalidArguments(t *testing.T) {
	executed := false
	tool := &stubTool{
		name: "t",
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			executed = true
			return ai.ToolOutput{}, nil
		},
	}
	e := newExecution(tool)
	e.ValidateArgs = func(json.RawMessage) error { return errors.New("bad args") }

	runChain(e, SchemaValidation, ExecuteTool)

	if e.Err == nil {
		t.Fatal("Err should be set on invalid arguments")
	}
	if code := pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", e.Err)); code != pierrors.ErrorCodeToolInvalidArguments {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeToolInvalidArguments)
	}
	if executed {
		t.Fatal("tool should not execute on invalid arguments")
	}
}

func TestEventForwardingNotifiesObserverAndEmit(t *testing.T) {
	tool := &stubTool{
		name: "t",
		execute: func(_ context.Context, _ json.RawMessage, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
			emit(ai.ToolUpdate{Content: []ai.ContentBlock{ai.TextBlock("progress")}})
			return ai.ToolOutput{}, nil
		},
	}
	e := newExecution(tool)
	var observed []ai.ToolUpdate
	e.Observer = func(_ context.Context, call ai.ToolCall, update ai.ToolUpdate) {
		if call.ID != "call-1" {
			t.Errorf("observer call ID = %v, want call-1", call.ID)
		}
		observed = append(observed, update)
	}
	var emitted []ai.ToolUpdate
	e.Emit = func(update ai.ToolUpdate) { emitted = append(emitted, update) }

	e.Run([]Handler{EventForwarding, ExecuteTool})

	if len(observed) != 1 || len(emitted) != 1 {
		t.Fatalf("observed = %d, emitted = %d, want 1/1", len(observed), len(emitted))
	}
}
