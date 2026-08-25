package middleware

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestTimeoutConvertsSlowToolToToolTimeout(t *testing.T) {
	tool := &stubTool{
		name: "exec",
		execute: func(ctx context.Context, _ json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
			select {
			case <-ctx.Done():
				return ai.ToolOutput{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("done")}}, nil
			}
		},
	}
	e := newExecution(tool)

	runChain(e, Timeout(20*time.Millisecond), ExecuteTool)

	if code := permissionErrorCode(t, e.Err); code != pierrors.ErrorCodeToolTimeout {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeToolTimeout)
	}
	// 返回后 e.Ctx 应还原为父 ctx（可被 Retry 安全重跑）。
	if e.Ctx.Err() != nil {
		t.Fatal("e.Ctx should be restored to the live parent ctx")
	}
}

func TestTimeoutPassesFastTool(t *testing.T) {
	e := newExecution(okTool("read"))

	runChain(e, Timeout(time.Minute), ExecuteTool)

	if e.Err != nil {
		t.Fatalf("Err = %v, want nil", e.Err)
	}
	if len(e.Output.Content) == 0 {
		t.Fatal("Output should pass through")
	}
}

// 父 ctx 的取消不能被 Timeout 覆盖为 tool_timeout——归一权属 ToolRuntime。
func TestTimeoutDoesNotOverrideParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tool := &stubTool{
		name: "exec",
		execute: func(ctx context.Context, _ json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
			<-ctx.Done()
			return ai.ToolOutput{}, ctx.Err()
		},
	}
	e := newExecution(tool)
	e.Ctx = ctx

	done := make(chan struct{})
	go func() {
		defer close(done)
		runChain(e, Timeout(time.Minute), ExecuteTool)
	}()
	cancel()
	<-done

	if code := pierrors.ErrorCodeOf(e.Err); code != pierrors.ErrorCodeCanceled {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeCanceled)
	}
}
