package middleware

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

func permissionErrorCode(t *testing.T, err error) pierrors.ErrorCode {
	t.Helper()
	if err == nil {
		t.Fatal("Err should be set")
	}
	return pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", err))
}

// deny 规则命中：Block 且错误码为 tool_permission_denied，Tool 不执行。
func TestPermissionBlocksDeniedCall(t *testing.T) {
	executed := false
	tool := &stubTool{
		name: "exec",
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			executed = true
			return ai.ToolOutput{}, nil
		},
	}
	e := newExecution(tool)
	e.Call.Arguments = json.RawMessage(`{"command":"rm -rf /var/log"}`)
	rules := []PermissionRule{
		{Tool: "exec", Pattern: regexp.MustCompile(`rm\s+-rf`), Reason: "级联删除"},
	}

	runChain(e, Permission(rules), ExecuteTool)

	if code := permissionErrorCode(t, e.Err); code != pierrors.ErrorCodeToolPermissionDenied {
		t.Fatalf("error code = %v, want %v", code, pierrors.ErrorCodeToolPermissionDenied)
	}
	if executed {
		t.Fatal("denied tool should not execute")
	}
}

// 未命中放行：正则不匹配、工具名不匹配、规则为空三种情况都正常执行。
func TestPermissionPassesUnmatchedCalls(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		arguments string
		rules     []PermissionRule
	}{
		{
			name:      "pattern not matched",
			toolName:  "exec",
			arguments: `{"command":"ls -la"}`,
			rules:     []PermissionRule{{Tool: "exec", Pattern: regexp.MustCompile(`rm\s+-rf`)}},
		},
		{
			name:      "tool not matched",
			toolName:  "read",
			arguments: `{"path":"rm -rf.txt"}`,
			rules:     []PermissionRule{{Tool: "exec", Pattern: regexp.MustCompile(`rm\s+-rf`)}},
		},
		{
			name:      "empty rules",
			toolName:  "exec",
			arguments: `{"command":"rm -rf /"}`,
			rules:     nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executed := false
			tool := &stubTool{
				name: test.toolName,
				execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
					executed = true
					return ai.ToolOutput{}, nil
				},
			}
			e := newExecution(tool)
			e.Call.Arguments = json.RawMessage(test.arguments)

			runChain(e, Permission(test.rules), ExecuteTool)

			if e.Err != nil {
				t.Fatalf("Err = %v, want nil", e.Err)
			}
			if !executed {
				t.Fatal("unmatched call should execute")
			}
		})
	}
}
