package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/PycMono/go-reagent/domain/service"
	"github.com/PycMono/go-reagent/pi/ai"
)

type currentTimeTool struct {
	resolver service.LocationResolver
	clock    Clock
}

type currentTimeResult struct {
	Status    string       `json:"status"`
	Location  locationView `json:"location"`
	LocalTime string       `json:"local_time"`
	Date      string       `json:"date"`
	Weekday   string       `json:"weekday"`
}

func newCurrentTimeTool(resolver service.LocationResolver, clock Clock) *currentTimeTool {
	return &currentTimeTool{resolver: resolver, clock: clock}
}

func (t *currentTimeTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         "get_current_time",
		Description:  "按地点获取准确的当地日期、时间和星期；重名地点会返回候选供用户确认。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location":     map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				"country_code": map[string]any{"type": "string", "pattern": "^[A-Za-z]{2}$"},
				"admin1":       map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
			},
			"required":             []string{"location"},
			"additionalProperties": false,
		},
	}
}

func (t *currentTimeTool) Execute(ctx context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	arguments, err := decodeArguments[locationInput](raw)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	location, failure, err := resolveLocation(ctx, t.resolver, arguments)
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("resolve time location: %w", err)
	}
	if failure != nil {
		return jsonOutput(failure)
	}
	zone, err := time.LoadLocation(location.Timezone)
	if err != nil {
		return ai.ToolOutput{}, invalidArguments("invalid location timezone", err)
	}
	now := t.clock().In(zone)
	return jsonOutput(currentTimeResult{
		Status: "ok", Location: locationToView(location), LocalTime: now.Format(time.RFC3339),
		Date: now.Format("2006-01-02"), Weekday: now.Weekday().String(),
	})
}
