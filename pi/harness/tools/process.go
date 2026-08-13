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

	"github.com/PycMono/go-reagent/pi/ai"
)

const (
	maxProcessWaitMS  = 30_000
	maxProcessLogSize = defaultProcessOutputBytes
)

type ProcessDetails struct {
	Action     string            `json:"action"`
	SessionID  string            `json:"sessionId,omitempty"`
	Status     string            `json:"status,omitempty"`
	ExitCode   *int              `json:"exitCode,omitempty"`
	Offset     int64             `json:"offset,omitempty"`
	NextOffset int64             `json:"nextOffset,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
	Removed    int               `json:"removed,omitempty"`
	Sessions   []ProcessSnapshot `json:"sessions,omitempty"`
}

type ProcessTool struct {
	supervisor *ProcessSupervisor
}

func NewProcessTool(supervisor *ProcessSupervisor) *ProcessTool {
	return &ProcessTool{supervisor: supervisor}
}

func (t *ProcessTool) Name() string {
	return "process"
}

func (t *ProcessTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.Name(),
		Description: "管理 exec 创建的后台命令。支持 list、poll、log、write、kill、clear 和 remove。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":    map[string]any{"type": "string", "enum": []string{"list", "poll", "log", "write", "kill", "clear", "remove"}},
				"sessionId": map[string]any{"type": "string"},
				"timeout":   map[string]any{"type": "integer", "minimum": 0, "maximum": maxProcessWaitMS},
				"offset":    map[string]any{"type": "integer", "minimum": 0},
				"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": maxProcessLogSize, "default": maxProcessLogSize},
				"data":      map[string]any{"type": "string"},
				"eof":       map[string]any{"type": "boolean"},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	}
}

func (t *ProcessTool) Execute(ctx context.Context, args json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	if err := t.supervisor.ensureOpen(); err != nil {
		return ai.ToolOutput{}, err
	}
	input, err := decodeProcessArgs(args)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	input.Action = strings.TrimSpace(input.Action)
	switch input.Action {
	case "list":
		sessions := t.supervisor.List()
		return processOutput(fmt.Sprintf("%d sessions", len(sessions)), ProcessDetails{
			Action:   input.Action,
			Sessions: sessions,
		}), nil

	case "poll":
		if err := requireProcessSessionID(input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		timeoutMS := 0
		if input.Timeout != nil {
			timeoutMS = *input.Timeout
		}
		if timeoutMS < 0 || timeoutMS > maxProcessWaitMS {
			return ai.ToolOutput{}, fmt.Errorf("timeout 必须在 0..%d 毫秒", maxProcessWaitMS)
		}
		snapshot, err := t.supervisor.Poll(ctx, input.SessionID, time.Duration(timeoutMS)*time.Millisecond)
		if err != nil {
			return ai.ToolOutput{}, err
		}
		return processSnapshotOutput(input.Action, snapshot, fmt.Sprintf("session %s is %s", snapshot.SessionID, snapshot.Status)), nil

	case "log":
		if err := requireProcessSessionID(input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		offset := int64(0)
		if input.Offset != nil {
			offset = *input.Offset
		}
		limit := maxProcessLogSize
		if input.Limit != nil {
			limit = *input.Limit
		}
		if offset < 0 {
			return ai.ToolOutput{}, errors.New("offset 不能小于 0")
		}
		if limit < 1 || limit > maxProcessLogSize {
			return ai.ToolOutput{}, fmt.Errorf("limit 必须在 1..%d", maxProcessLogSize)
		}
		page, err := t.supervisor.Log(input.SessionID, offset, limit)
		if err != nil {
			return ai.ToolOutput{}, err
		}
		content := page.Content
		if content == "" {
			content = "(no output)"
		}
		return processOutput(content, ProcessDetails{
			Action:     input.Action,
			SessionID:  input.SessionID,
			Offset:     page.Offset,
			NextOffset: page.NextOffset,
			Truncated:  page.Truncated,
		}), nil

	case "write":
		if err := requireProcessSessionID(input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		if input.Data == nil && !input.EOF {
			return ai.ToolOutput{}, errors.New("write action 需要 data 或 eof=true")
		}
		snapshot, err := t.supervisor.Write(ctx, input.SessionID, input.Data, input.EOF)
		if err != nil {
			return ai.ToolOutput{}, err
		}
		return processSnapshotOutput(input.Action, snapshot, fmt.Sprintf("wrote to session %s", snapshot.SessionID)), nil

	case "kill":
		if err := requireProcessSessionID(input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		snapshot, err := t.supervisor.Kill(ctx, input.SessionID)
		if err != nil {
			return ai.ToolOutput{}, err
		}
		return processSnapshotOutput(input.Action, snapshot, fmt.Sprintf("killed session %s", snapshot.SessionID)), nil

	case "clear":
		removed := t.supervisor.Clear()
		return processOutput(fmt.Sprintf("cleared %d sessions", removed), ProcessDetails{
			Action:  input.Action,
			Removed: removed,
		}), nil

	case "remove":
		if err := requireProcessSessionID(input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		if err := t.supervisor.Remove(ctx, input.SessionID); err != nil {
			return ai.ToolOutput{}, err
		}
		return processOutput(fmt.Sprintf("removed session %s", input.SessionID), ProcessDetails{
			Action:    input.Action,
			SessionID: input.SessionID,
			Status:    "removed",
			Removed:   1,
		}), nil

	default:
		return ai.ToolOutput{}, errors.New("action 必须是 list、poll、log、write、kill、clear 或 remove")
	}
}

func processSnapshotOutput(action string, snapshot ProcessSnapshot, content string) ai.ToolOutput {
	return processOutput(content, ProcessDetails{
		Action:    action,
		SessionID: snapshot.SessionID,
		Status:    snapshot.Status,
		ExitCode:  snapshot.ExitCode,
		Truncated: snapshot.Truncated,
	})
}

func processOutput(content string, details ProcessDetails) ai.ToolOutput {
	return ai.ToolOutput{
		Content: []ai.ContentBlock{ai.TextBlock(content)},
		Details: details,
	}
}

func requireProcessSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("sessionId 不能为空")
	}
	return nil
}

type processArgs struct {
	Action    string  `json:"action"`
	SessionID string  `json:"sessionId,omitempty"`
	Timeout   *int    `json:"timeout,omitempty"`
	Offset    *int64  `json:"offset,omitempty"`
	Limit     *int    `json:"limit,omitempty"`
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
