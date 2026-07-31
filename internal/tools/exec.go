package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/internal/schema"
)

const (
	defaultExecTimeoutMS = 120_000
	maxExecTimeoutMS     = 600_000
	defaultExecYieldMS   = 10_000
	maxExecYieldMS       = 30_000
)

type ExecTool struct {
	manager *ProcessManager
}

var _ Tool = (*ExecTool)(nil)

func NewExecTool(manager *ProcessManager) *ExecTool {
	return &ExecTool{manager: manager}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "在工作区中执行 shell 命令。支持超时、环境变量、前台等待和后台 session；命令拥有宿主进程权限，cwd 不是安全沙箱。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "要执行的 shell 命令"},
				"workdir":    map[string]any{"type": "string", "description": "可选的工作区相对目录"},
				"timeout_ms": map[string]any{"type": "integer", "minimum": 1, "maximum": maxExecTimeoutMS},
				"yield_ms":   map[string]any{"type": "integer", "minimum": 0, "maximum": maxExecYieldMS},
				"background": map[string]any{"type": "boolean"},
				"env":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage, _ UpdateEmitter) (schema.ToolOutput, error) {
	output, err := t.execute(ctx, args)
	return textToolOutput(output), err
}

func (t *ExecTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t == nil || t.manager == nil {
		return "", errors.New("exec 未初始化")
	}
	if ctx == nil {
		return "", errors.New("context 不能为空")
	}
	input, err := decodeExecArgs(args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Command) == "" {
		return "", errors.New("command 不能为空")
	}
	timeoutMS := defaultExecTimeoutMS
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	if timeoutMS < 1 || timeoutMS > maxExecTimeoutMS {
		return "", fmt.Errorf("timeout_ms 必须在 1..%d", maxExecTimeoutMS)
	}
	yieldMS := defaultExecYieldMS
	if input.YieldMS != nil {
		yieldMS = *input.YieldMS
	}
	if yieldMS < 0 || yieldMS > maxExecYieldMS {
		return "", fmt.Errorf("yield_ms 必须在 0..%d", maxExecYieldMS)
	}
	session, err := t.manager.start(ctx, input.Command, input.WorkDir, input.Env, time.Duration(timeoutMS)*time.Millisecond)
	if err != nil {
		return "", err
	}
	if input.Background {
		return marshalProcessSnapshot(session.snapshot())
	}
	timer := time.NewTimer(time.Duration(yieldMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-session.done:
		return marshalProcessSnapshot(session.snapshot())
	case <-timer.C:
		return marshalProcessSnapshot(session.snapshot())
	case <-ctx.Done():
		session.terminate("canceled")
		<-session.done
		return "", fmt.Errorf("命令执行已取消: %w", ctx.Err())
	}
}

type execArgs struct {
	Command    string            `json:"command"`
	WorkDir    string            `json:"workdir,omitempty"`
	TimeoutMS  *int              `json:"timeout_ms,omitempty"`
	YieldMS    *int              `json:"yield_ms,omitempty"`
	Background bool              `json:"background,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

func decodeExecArgs(args json.RawMessage) (execArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var input execArgs
	if err := decoder.Decode(&input); err != nil {
		return execArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return execArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return execArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}

func marshalProcessSnapshot(snapshot processSnapshot) (string, error) {
	output, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("序列化 process 结果失败: %w", err)
	}
	return string(output), nil
}
