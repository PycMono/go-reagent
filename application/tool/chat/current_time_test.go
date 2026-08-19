package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestCurrentTimeToolUsesIANAZone(t *testing.T) {
	tool := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Asia/Tokyo"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[currentTimeResult](t, output)
	if got.Timezone != "Asia/Tokyo" || got.LocalTime != "2026-08-16T10:30:00+09:00" ||
		got.Date != "2026-08-16" || got.Weekday != "Sunday" {
		t.Fatalf("result = %#v", got)
	}
}

func TestCurrentTimeToolDefinitionAcceptsOnlyTimezone(t *testing.T) {
	definition := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z")).Definition()
	if definition.Name != "get_current_time" || definition.Description == "" || !definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schema := definition.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	if schema["additionalProperties"] != false || len(properties) != 1 {
		t.Fatalf("schema = %#v", schema)
	}
	if _, ok := properties["timezone"]; !ok {
		t.Fatalf("properties = %#v", properties)
	}
}

func TestCurrentTimeToolRejectsInvalidTimezoneAndExtraArguments(t *testing.T) {
	tool := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z"))
	for _, arguments := range []string{
		`{"timezone":"Mars/Olympus"}`,
		`{"timezone":"Asia/Tokyo","location":"Tokyo"}`,
	} {
		_, err := tool.Execute(context.Background(), json.RawMessage(arguments), nil)
		if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
			t.Fatalf("arguments = %s, error = %v", arguments, err)
		}
	}
}

func TestCurrentTimeToolHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z"))
	_, err := tool.Execute(ctx, json.RawMessage(`{"timezone":"Asia/Tokyo"}`), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
