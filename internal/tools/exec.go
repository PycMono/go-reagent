package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

const (
	defaultExecTimeoutSeconds = 120
	maxExecTimeoutSeconds     = 600
	defaultExecYieldMS        = 10_000
	maxExecYieldMS            = 30_000
)

type StreamDetails struct {
	Stream string `json:"stream"`
	Bytes  int    `json:"bytes"`
}

type ExecDetails struct {
	Status    string `json:"status"`
	SessionID string `json:"sessionId,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Command   string `json:"command"`
	CWD       string `json:"cwd"`
	Truncated bool   `json:"truncated"`
}

type ExecTool struct {
	supervisor *ProcessSupervisor
}

type execStreamGate struct {
	open atomic.Bool
	mu   sync.Mutex
}

var _ agent.Tool = (*ExecTool)(nil)

func NewExecTool(supervisor *ProcessSupervisor) *ExecTool {
	return &ExecTool{supervisor: supervisor}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.Name(),
		Description: "在工作区中执行 shell 命令。前台输出按 stdout/stderr 流式返回，命令可在 yield 后转入后台；命令拥有宿主进程权限，cwd 不是安全沙箱。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "要执行的 shell 命令"},
				"workdir":    map[string]any{"type": "string", "description": "可选的工作区相对目录"},
				"env":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				"yieldMs":    map[string]any{"type": "integer", "minimum": 0, "maximum": maxExecYieldMS, "default": defaultExecYieldMS},
				"background": map[string]any{"type": "boolean", "default": false},
				"timeout":    map[string]any{"type": "integer", "minimum": 1, "maximum": maxExecTimeoutSeconds, "default": defaultExecTimeoutSeconds},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage, emit agent.UpdateEmitter) (agent.ToolOutput, error) {
	if t == nil || t.supervisor == nil {
		return agent.ToolOutput{}, errors.New("exec 未初始化")
	}
	if ctx == nil {
		return agent.ToolOutput{}, errors.New("context 不能为空")
	}
	input, err := decodeExecArgs(args)
	if err != nil {
		return agent.ToolOutput{}, err
	}
	if strings.TrimSpace(input.Command) == "" {
		return agent.ToolOutput{}, errors.New("command 不能为空")
	}
	timeoutSeconds := defaultExecTimeoutSeconds
	if input.Timeout != nil {
		timeoutSeconds = *input.Timeout
	}
	if timeoutSeconds < 1 || timeoutSeconds > maxExecTimeoutSeconds {
		return agent.ToolOutput{}, fmt.Errorf("timeout 必须在 1..%d 秒", maxExecTimeoutSeconds)
	}
	yieldMS := defaultExecYieldMS
	if input.YieldMS != nil {
		yieldMS = *input.YieldMS
	}
	if yieldMS < 0 || yieldMS > maxExecYieldMS {
		return agent.ToolOutput{}, fmt.Errorf("yieldMs 必须在 0..%d 毫秒", maxExecYieldMS)
	}

	gate := &execStreamGate{}
	gate.open.Store(!input.Background)
	onOutput := func(stream string, chunk []byte) {
		gate.emit(emit, stream, chunk)
	}
	session, err := t.supervisor.Start(ctx, ProcessStart{
		Command:  input.Command,
		WorkDir:  input.WorkDir,
		Env:      input.Env,
		Timeout:  time.Duration(timeoutSeconds) * time.Second,
		OnOutput: onOutput,
	})
	if err != nil {
		gate.close()
		return agent.ToolOutput{}, err
	}
	if input.Background {
		return execOutput(session.snapshot()), nil
	}

	timer := time.NewTimer(time.Duration(yieldMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-session.done:
		gate.close()
		return finishedExecOutput(ctx, session.snapshot())
	case <-timer.C:
		gate.close()
		snapshot := session.snapshot()
		if snapshot.Status != "running" {
			return finishedExecOutput(ctx, snapshot)
		}
		return execOutput(snapshot), nil
	case <-ctx.Done():
		gate.close()
		_ = session.terminate("canceled")
		<-session.done
		return execOutput(session.snapshot()), fmt.Errorf("命令执行已取消: %w", ctx.Err())
	}
}

func (g *execStreamGate) emit(emit agent.UpdateEmitter, stream string, chunk []byte) {
	if emit == nil || !g.open.Load() {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open.Load() {
		return
	}
	emit(agent.ToolUpdate{
		Content: []ai.ContentBlock{ai.TextBlock(string(chunk))},
		Details: StreamDetails{Stream: stream, Bytes: len(chunk)},
	})
}

func (g *execStreamGate) close() {
	g.open.Store(false)
	g.mu.Lock()
	g.mu.Unlock()
}

func execOutput(snapshot ProcessSnapshot) agent.ToolOutput {
	output := textToolOutput(snapshot.Output)
	output.Details = ExecDetails{
		Status:    snapshot.Status,
		SessionID: snapshot.SessionID,
		ExitCode:  snapshot.ExitCode,
		Command:   snapshot.Command,
		CWD:       snapshot.CWD,
		Truncated: snapshot.Truncated,
	}
	return output
}

func finishedExecOutput(ctx context.Context, snapshot ProcessSnapshot) (agent.ToolOutput, error) {
	output := execOutput(snapshot)
	if err := ctx.Err(); err != nil {
		return output, fmt.Errorf("命令执行已取消: %w", err)
	}
	switch snapshot.Status {
	case "timed_out":
		return output, errors.New("命令执行超时")
	case "canceled":
		return output, errors.New("命令执行已取消")
	case "failed":
		return output, errors.New("命令执行失败")
	}
	if snapshot.ExitCode != nil && *snapshot.ExitCode != 0 {
		return output, fmt.Errorf("命令退出码为 %d", *snapshot.ExitCode)
	}
	return output, nil
}

type execArgs struct {
	Command    string            `json:"command"`
	WorkDir    string            `json:"workdir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	YieldMS    *int              `json:"yieldMs,omitempty"`
	Background bool              `json:"background,omitempty"`
	Timeout    *int              `json:"timeout,omitempty"`
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
