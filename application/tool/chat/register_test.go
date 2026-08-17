package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/domain/service"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type registeredTools struct {
	fx.In
	Tools []ai.Tool `group:"agent_tools"`
}

func TestRegisterProvidesThreeChatTools(t *testing.T) {
	resolver := &fakeResolver{}
	provider := &fakeWeatherProvider{}
	var tools []ai.Tool
	app := fxtest.New(t,
		Register,
		fx.Provide(
			func() service.LocationResolver { return resolver },
			func() service.WeatherProvider { return provider },
		),
		fx.Invoke(func(params registeredTools) { tools = params.Tools }),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Definition().Name
		if !tool.Definition().ParallelSafe {
			t.Fatalf("tool %q is not parallel safe", names[i])
		}
	}
	slices.Sort(names)
	want := []string{"calculate", "get_current_time", "get_weather"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestRegisteredWeatherToolRunsThroughDirectLoop(t *testing.T) {
	tests := []struct {
		name       string
		locations  []service.Location
		forecast   service.Forecast
		wantStatus string
	}{
		{
			name: "forecast", wantStatus: "ok",
			locations: []service.Location{{Name: "Beijing", Country: "China", CountryCode: "CN", Admin1: "Beijing", Timezone: "Asia/Shanghai"}},
			forecast:  forecastWithDays("2026-08-16", 1),
		},
		{
			name: "ambiguous", wantStatus: "ambiguous",
			locations: []service.Location{
				{Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Beijing", Timezone: "Asia/Shanghai"},
				{Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Liaoning", Timezone: "Asia/Shanghai"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			weather := newWeatherTool(
				&fakeResolver{locations: test.locations},
				&fakeWeatherProvider{forecast: test.forecast},
				fixedClock("2026-08-16T01:30:00Z"),
			)
			runtime, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{
				Tools: []ai.Tool{weather}, Middlewares: pi.DefaultMiddlewareRegistrations(),
			})
			if err != nil {
				t.Fatal(err)
			}
			provider := &loopWeatherProvider{location: test.locations[0].Name, wantStatus: test.wantStatus}
			reporter := &eventCollector{}
			loop := pi.NewLoop(provider, pi.NewScheduler(runtime, 4), false)
			messages, err := loop.Run(context.Background(), harness.Context{
				Messages: []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("weather")}}},
				Tools:    runtime.Definitions(),
			}, reporter)
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != 2 || len(messages) != 3 {
				t.Fatalf("provider calls = %d, messages = %#v", provider.calls, messages)
			}
			if messages[0].Role != ai.RoleAssistant || messages[1].Role != ai.RoleTool || messages[2].Role != ai.RoleAssistant {
				t.Fatalf("message roles = %q, %q, %q", messages[0].Role, messages[1].Role, messages[2].Role)
			}
			for _, event := range reporter.events {
				if event.Type == pi.AgentEventThinking {
					t.Fatalf("direct loop emitted Thinking event: %#v", reporter.events)
				}
			}
		})
	}
}

type loopWeatherProvider struct {
	location   string
	wantStatus string
	calls      int
}

func (p *loopWeatherProvider) Stream(_ context.Context, messages []ai.Message, tools []ai.ToolDefinition) ai.Stream {
	p.calls++
	switch p.calls {
	case 1:
		if len(tools) != 1 || tools[0].Name != "get_weather" {
			return newLoopWeatherStream(nil, fmt.Errorf("tools = %#v", tools))
		}
		arguments, _ := json.Marshal(map[string]any{"location": p.location})
		return newLoopWeatherStream(meteredMessage(ai.Message{
			Role:         ai.RoleAssistant,
			ToolCalls:    []ai.ToolCall{{ID: "weather-1", Name: "get_weather", Arguments: arguments}},
			FinishReason: ai.FinishReasonToolUse,
		}), nil)
	case 2:
		if len(messages) == 0 || messages[len(messages)-1].Role != ai.RoleTool {
			return newLoopWeatherStream(nil, errors.New("tool result was not appended to provider context"))
		}
		text, err := ai.TextContent(messages[len(messages)-1].Content)
		if err != nil {
			return newLoopWeatherStream(nil, err)
		}
		var payload struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			return newLoopWeatherStream(nil, fmt.Errorf("decode tool result: %w", err))
		}
		if payload.Status != p.wantStatus {
			return newLoopWeatherStream(nil, fmt.Errorf("tool status = %q, want %q", payload.Status, p.wantStatus))
		}
		return newLoopWeatherStream(meteredMessage(ai.Message{
			Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}, FinishReason: ai.FinishReasonStop,
		}), nil)
	default:
		return newLoopWeatherStream(nil, fmt.Errorf("unexpected provider call %d", p.calls))
	}
}

type loopWeatherStream struct {
	events  []ai.StreamEvent
	index   int
	message *ai.Message
	err     error
}

func newLoopWeatherStream(message *ai.Message, err error) ai.Stream {
	events := []ai.StreamEvent{{Type: ai.StreamEventStart}}
	if err != nil {
		events = append(events, ai.StreamEvent{Type: ai.StreamEventError})
	} else {
		if message != nil {
			for _, block := range message.Content {
				if block.Type == ai.ContentTypeText && block.Text != "" {
					events = append(events, ai.StreamEvent{Type: ai.StreamEventTextDelta, TextDelta: block.Text})
				}
			}
		}
		events = append(events, ai.StreamEvent{Type: ai.StreamEventDone})
	}
	return &loopWeatherStream{events: events, message: message, err: err}
}

func (s *loopWeatherStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return true
}

func (s *loopWeatherStream) Current() ai.StreamEvent      { return s.events[s.index-1] }
func (s *loopWeatherStream) Result() (*ai.Message, error) { return s.message, s.err }
func (s *loopWeatherStream) Close() error                 { return nil }

func meteredMessage(message ai.Message) *ai.Message {
	message.Usage = &ai.Usage{PlatformID: "test", Model: "test"}
	return &message
}

type eventCollector struct{ events []pi.AgentEvent }

func (r *eventCollector) Report(_ context.Context, event pi.AgentEvent) {
	r.events = append(r.events, event)
}
