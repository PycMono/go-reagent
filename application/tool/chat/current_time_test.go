package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/domain/service"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestCurrentTimeToolUsesResolvedTimezone(t *testing.T) {
	resolver := &fakeResolver{locations: []service.Location{
		{Name: "Tokyo", Country: "Japan", CountryCode: "JP", Admin1: "Tokyo", Timezone: "Asia/Tokyo"},
	}}
	tool := newCurrentTimeTool(resolver, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Tokyo"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[currentTimeResult](t, output)
	if got.Status != "ok" || got.LocalTime != "2026-08-16T10:30:00+09:00" || got.Date != "2026-08-16" || got.Weekday != "Sunday" {
		t.Fatalf("result = %#v", got)
	}
	if got.Location.Name != "Tokyo" || got.Location.Timezone != "Asia/Tokyo" {
		t.Fatalf("location = %#v", got.Location)
	}
}

func TestCurrentTimeToolDefinitionIsStrictAndParallelSafe(t *testing.T) {
	definition := newCurrentTimeTool(&fakeResolver{}, fixedClock("2026-08-16T01:30:00Z")).Definition()
	if definition.Name != "get_current_time" || definition.Description == "" || !definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schema := definition.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	if schema["additionalProperties"] != false || len(properties) != 3 {
		t.Fatalf("schema = %#v", schema)
	}
}

func TestCurrentTimeToolReturnsLocationFailures(t *testing.T) {
	tests := []struct {
		name      string
		locations []service.Location
		want      string
	}{
		{name: "not found", want: "not_found"},
		{name: "ambiguous", want: "ambiguous", locations: []service.Location{
			{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Illinois", Timezone: "America/Chicago"},
			{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Missouri", Timezone: "America/Chicago"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool := newCurrentTimeTool(&fakeResolver{locations: test.locations}, fixedClock("2026-08-16T01:30:00Z"))
			output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Springfield"}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			got := decodeToolJSON[locationFailure](t, output)
			if got.Status != test.want {
				t.Fatalf("result = %#v", got)
			}
		})
	}
}

func TestCurrentTimeToolFiltersLocation(t *testing.T) {
	resolver := &fakeResolver{locations: []service.Location{
		{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Illinois", Timezone: "America/Chicago"},
		{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Missouri", Timezone: "America/Chicago"},
	}}
	tool := newCurrentTimeTool(resolver, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Springfield","country_code":"us","admin1":"missouri"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[currentTimeResult](t, output)
	if got.Status != "ok" || got.Location.Admin1 != "Missouri" {
		t.Fatalf("result = %#v", got)
	}
}

func TestCurrentTimeToolRejectsInvalidTimezoneAndArguments(t *testing.T) {
	location := service.Location{Name: "Nowhere", Country: "Nowhere", CountryCode: "NW", Timezone: "invalid/timezone"}
	tool := newCurrentTimeTool(&fakeResolver{locations: []service.Location{location}}, fixedClock("2026-08-16T01:30:00Z"))
	for _, arguments := range []string{
		`{"location":"Nowhere"}`,
		`{"location":"Nowhere","unexpected":true}`,
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
	tool := newCurrentTimeTool(&fakeResolver{}, fixedClock("2026-08-16T01:30:00Z"))
	_, err := tool.Execute(ctx, json.RawMessage(`{"location":"Tokyo"}`), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
