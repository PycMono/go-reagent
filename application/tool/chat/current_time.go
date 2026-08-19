package chat

import (
	"context"
	"encoding/json"
	"time"
	_ "time/tzdata"

	"github.com/PycMono/go-reagent/pi/ai"
)

type currentTimeTool struct {
	clock Clock
}

type currentTimeArgs struct {
	Timezone string `json:"timezone"`
}

type currentTimeResult struct {
	Timezone  string `json:"timezone"`
	LocalTime string `json:"local_time"`
	Date      string `json:"date"`
	Weekday   string `json:"weekday"`
}

func newCurrentTimeTool(clock Clock) *currentTimeTool {
	return &currentTimeTool{clock: clock}
}

func (t *currentTimeTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         "get_current_time",
		Description:  "根据 IANA 时区获取准确的当地日期、时间和星期，不进行网络查询。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timezone": map[string]any{
					"type": "string", "minLength": 1, "maxLength": 120,
					"description": "IANA 时区，例如 Asia/Shanghai 或 America/New_York",
				},
			},
			"required":             []string{"timezone"},
			"additionalProperties": false,
		},
	}
}

func (t *currentTimeTool) Execute(ctx context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolOutput{}, err
	}
	arguments, err := decodeArguments[currentTimeArgs](raw)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	zone, err := time.LoadLocation(arguments.Timezone)
	if err != nil {
		return ai.ToolOutput{}, invalidArguments("invalid IANA timezone", err)
	}
	now := t.clock().In(zone)
	return jsonOutput(currentTimeResult{
		Timezone: arguments.Timezone, LocalTime: now.Format(time.RFC3339),
		Date: now.Format("2006-01-02"), Weekday: now.Weekday().String(),
	})
}
