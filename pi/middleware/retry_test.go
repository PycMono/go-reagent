package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// flakyTool 前 failures 次以 tool_runtime_failed 失败，之后成功。
func flakyTool(name string, failures int, calls *int) *stubTool {
	return &stubTool{
		name: name,
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			*calls++
			if *calls <= failures {
				return ai.ToolOutput{}, pierrors.Wrap(
					pierrors.ErrorCodeToolRuntime,
					"call tool",
					errors.New("connection reset"),
				)
			}
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
		},
	}
}

func TestRetrySucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	e := newExecution(flakyTool("mcp_search", 2, &calls))

	runChain(e, Retry(3, time.Millisecond, []string{"mcp_search"}), ExecuteTool)

	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if e.Err != nil {
		t.Fatalf("Err = %v, want nil", e.Err)
	}
	if len(e.Output.Content) == 0 {
		t.Fatal("Output should come from the successful attempt")
	}
}

func TestRetryGivesUpAfterAttempts(t *testing.T) {
	calls := 0
	e := newExecution(flakyTool("mcp_search", 99, &calls))

	runChain(e, Retry(3, time.Millisecond, []string{"mcp_search"}), ExecuteTool)

	if calls != 3 {
		t.Fatalf("calls = %d, want 3（含首次）", calls)
	}
	if code := permissionErrorCode(t, e.Err); code != pierrors.ErrorCodeToolRuntime {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeToolRuntime)
	}
}

func TestRetrySkipsNonWhitelistedTool(t *testing.T) {
	calls := 0
	e := newExecution(flakyTool("exec", 99, &calls))

	runChain(e, Retry(3, time.Millisecond, []string{"mcp_search"}), ExecuteTool)

	if calls != 1 {
		t.Fatalf("calls = %d, want 1（非白名单工具不重试）", calls)
	}
}

func TestRetrySkipsPermanentError(t *testing.T) {
	calls := 0
	tool := &stubTool{
		name: "mcp_search",
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			calls++
			return ai.ToolOutput{}, pierrors.Wrap(
				pierrors.ErrorCodeToolPermissionDenied,
				"tool permission",
				errors.New("denied"),
			)
		},
	}
	e := newExecution(tool)

	runChain(e, Retry(3, time.Millisecond, []string{"mcp_search"}), ExecuteTool)

	if calls != 1 {
		t.Fatalf("calls = %d, want 1（确定性错误不重试）", calls)
	}
}

func TestRetryDisabledBelowTwoAttempts(t *testing.T) {
	calls := 0
	e := newExecution(flakyTool("mcp_search", 99, &calls))

	runChain(e, Retry(1, time.Millisecond, []string{"mcp_search"}), ExecuteTool)

	if calls != 1 {
		t.Fatalf("calls = %d, want 1（attempts<=1 不重试）", calls)
	}
}

// 链序 Permission → Retry → Timeout 下，权限判定只执行一次，而每次
// 重试都获得新的执行期限（后缀 [Timeout, ExecuteTool] 被重跑）。
func TestRetryRerunsSuffixWithFreshTimeout(t *testing.T) {
	calls := 0
	e := newExecution(flakyTool("mcp_search", 1, &calls))
	permissionChecks := 0
	counting := func(e *Execution) {
		permissionChecks++
		e.Next()
	}

	runChain(e, counting, Retry(2, time.Millisecond, []string{"mcp_search"}), Timeout(time.Minute), ExecuteTool)

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if permissionChecks != 1 {
		t.Fatalf("permission checks = %d, want 1（Retry 只重跑后缀链）", permissionChecks)
	}
	if e.Err != nil {
		t.Fatalf("Err = %v, want nil", e.Err)
	}
}
