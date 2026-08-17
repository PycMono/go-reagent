package pi

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestReadOnlyToolsRegisterExposesOnlyRead(t *testing.T) {
	got := resolveRegisteredToolNames(t, ReadOnlyToolsRegister)
	if want := []string{"read"}; !slices.Equal(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
}

func TestNewLoopDisablesThinkingForDirectChat(t *testing.T) {
	provider := &registerTestProvider{}
	runtime, err := NewToolRuntime(ToolRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reporter := &registerTestReporter{}
	loop := newLoop(provider, NewScheduler(runtime, 1), ThinkingEnabled(false))

	_, err = loop.Run(context.Background(), harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("你好")},
	}}}, reporter)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("Provider calls = %d, want 1", provider.calls)
	}
	wantEvents := []AgentEventType{AgentEventMessageStart, AgentEventMessageUpdate, AgentEventMessageEnd}
	if !slices.Equal(reporter.events, wantEvents) {
		t.Fatalf("events = %v, want %v", reporter.events, wantEvents)
	}
}

type registerTestProvider struct {
	calls int
}

func (p *registerTestProvider) Stream(context.Context, []ai.Message, []ai.ToolDefinition) ai.Stream {
	p.calls++
	message := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("你好")},
		Usage:   &ai.Usage{PlatformID: "test", Model: "test-model"},
	}
	return &registerTestStream{message: message}
}

type registerTestStream struct {
	step    int
	message *ai.Message
}

func (s *registerTestStream) Next() bool { s.step++; return s.step <= 3 }
func (s *registerTestStream) Current() ai.StreamEvent {
	switch s.step {
	case 1:
		return ai.StreamEvent{Type: ai.StreamEventStart}
	case 2:
		return ai.StreamEvent{Type: ai.StreamEventTextDelta, TextDelta: "你好"}
	default:
		return ai.StreamEvent{Type: ai.StreamEventDone}
	}
}
func (s *registerTestStream) Result() (*ai.Message, error) { return s.message, nil }
func (s *registerTestStream) Close() error                 { return nil }

type registerTestReporter struct {
	events []AgentEventType
}

func (r *registerTestReporter) Report(_ context.Context, event AgentEvent) {
	r.events = append(r.events, event.Type)
}

func TestCodingToolsRegisterPreservesCompleteDefaultSet(t *testing.T) {
	got := resolveRegisteredToolNames(t, CodingToolsRegister)
	want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	if !slices.Equal(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
}

func TestCoreRegisterAllowsEmptyToolGroup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		CoreRegister,
		fx.Supply(
			WorkDir(root),
			ThinkingEnabled(false),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	if got := runtime.Definitions(); len(got) != 0 {
		t.Fatalf("CoreRegister tools = %#v, want empty", got)
	}
}

func resolveRegisteredToolNames(t *testing.T, register fx.Option) []string {
	t.Helper()
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		register,
		fx.Provide(newToolRuntime),
		fx.Supply(WorkDir(t.TempDir())),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	definitions := runtime.Definitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	return names
}
