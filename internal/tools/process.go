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

const maxProcessWaitMS = 30_000

type ProcessTool struct {
	supervisor *ProcessSupervisor
}

var _ Tool = (*ProcessTool)(nil)

func NewProcessTool(supervisor *ProcessSupervisor) *ProcessTool {
	return &ProcessTool{supervisor: supervisor}
}

func (t *ProcessTool) Name() string {
	return "process"
}

func (t *ProcessTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "管理 exec 创建的后台命令。支持 list、poll、write 和 kill。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":     map[string]any{"type": "string", "enum": []string{"list", "poll", "write", "kill"}},
				"session_id": map[string]any{"type": "string"},
				"wait_ms":    map[string]any{"type": "integer", "minimum": 0, "maximum": maxProcessWaitMS},
				"data":       map[string]any{"type": "string"},
				"eof":        map[string]any{"type": "boolean"},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	}
}

func (t *ProcessTool) Execute(ctx context.Context, args json.RawMessage, _ UpdateEmitter) (schema.ToolOutput, error) {
	output, err := t.execute(ctx, args)
	return textToolOutput(output), err
}

func (t *ProcessTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t == nil || t.supervisor == nil {
		return "", errors.New("process 未初始化")
	}
	if ctx == nil {
		return "", errors.New("context 不能为空")
	}
	if err := t.supervisor.ensureOpen(); err != nil {
		return "", err
	}
	input, err := decodeProcessArgs(args)
	if err != nil {
		return "", err
	}
	input.Action = strings.TrimSpace(input.Action)
	if input.Action == "list" {
		snapshots := t.supervisor.List()
		sessions := make([]any, 0, len(snapshots))
		for _, snapshot := range snapshots {
			sessions = append(sessions, legacyProcessSnapshot(snapshot))
		}
		output, err := json.Marshal(map[string]any{"sessions": sessions})
		if err != nil {
			return "", fmt.Errorf("序列化 process 列表失败: %w", err)
		}
		return string(output), nil
	}
	if input.Action != "poll" && input.Action != "write" && input.Action != "kill" {
		return "", errors.New("action 必须是 list、poll、write 或 kill")
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.SessionID == "" {
		return "", errors.New("session_id 不能为空")
	}
	waitMS := 0
	if input.WaitMS != nil {
		waitMS = *input.WaitMS
	}
	if waitMS < 0 || waitMS > maxProcessWaitMS {
		return "", fmt.Errorf("wait_ms 必须在 0..%d", maxProcessWaitMS)
	}
	var snapshot ProcessSnapshot
	switch input.Action {
	case "poll":
		snapshot, err = t.supervisor.Poll(ctx, input.SessionID, time.Duration(waitMS)*time.Millisecond)

	case "write":
		if input.Data == nil && !input.EOF {
			return "", errors.New("write action 需要 data 或 eof=true")
		}
		snapshot, err = t.supervisor.Write(ctx, input.SessionID, input.Data, input.EOF)

	case "kill":
		snapshot, err = t.supervisor.Kill(ctx, input.SessionID)
	}
	if err != nil {
		return "", err
	}
	return marshalProcessSnapshot(snapshot)
}

type processArgs struct {
	Action    string  `json:"action"`
	SessionID string  `json:"session_id,omitempty"`
	WaitMS    *int    `json:"wait_ms,omitempty"`
	Data      *string `json:"data,omitempty"`
	EOF       bool    `json:"eof,omitempty"`
}

func decodeProcessArgs(args json.RawMessage) (processArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var input processArgs
	if err := decoder.Decode(&input); err != nil {
		return processArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return processArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return processArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}
