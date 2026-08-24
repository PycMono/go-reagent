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

func TestCoreRegisterInjectsCompactionConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var loop *Loop
	app := fxtest.New(
		t,
		CoreRegister,
		fx.Supply(
			WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
			harness.CompactionConfig{ContextWindowTokens: 128000, EnablePrune: true},
		),
		fx.Populate(&loop),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	if loop.compaction.ContextWindowTokens != 128000 || !loop.compaction.EnablePrune {
		t.Fatalf("loop compaction config = %+v, want the supplied value (fx optional injection)", loop.compaction)
	}
}

func TestCoreRegisterDefaultsZeroCompactionConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var loop *Loop
	app := fxtest.New(
		t,
		CoreRegister,
		fx.Supply(
			WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&loop),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	if loop.compaction != (harness.CompactionConfig{}) {
		t.Fatalf("loop compaction config = %+v, want zero value when not supplied", loop.compaction)
	}
}

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
	listener := &registerTestRunListener{}
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	loop := NewLoop(provider, NewScheduler(runtime, 1))
	agent := New(builder, loop, runtime)

	_, err = agent.Run(context.Background(), RunRequest{Input: Message{
		ContentType: "text",
		Content:     "你好",
		SenderType:  "customer",
	}}, listener)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("Provider calls = %d, want 1", provider.calls)
	}
	wantEvents := []AgentEventType{AgentEventMessageStart, AgentEventMessageUpdate, AgentEventMessageEnd}
	if !slices.Equal(listener.events, wantEvents) {
		t.Fatalf("events = %v, want %v", listener.events, wantEvents)
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

type registerTestRunListener struct {
	events []AgentEventType
}

func (r *registerTestRunListener) OnEvent(_ context.Context, event AgentEvent) {
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

func TestCoreRegisterAddsGroupedExtensionToolsBeforeUse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var events []string
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		CoreRegister,
		fx.Provide(fx.Annotate(
			func() Extension {
				return &extensionFake{name: "mcp:test", events: &events, tool: "remote_tool"}
			},
			fx.ResultTags(`group:"agent_extensions"`),
		)),
		fx.Supply(
			WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	definitions := runtime.Definitions()
	if len(definitions) != 1 || definitions[0].Name != "remote_tool" {
		t.Fatalf("CoreRegister extension tools = %#v", definitions)
	}
}

func resolveRegisteredToolNames(t *testing.T, register fx.Option) []string {
	t.Helper()
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		register,
		fx.Provide(newFXToolRegistry, newExtensionRuntime, newFXToolRuntime),
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

// subagentFXTestOptions 提供 fx 装配测试的公共依赖（stub web 工具入 group）。
func subagentFXTestOptions(root string) fx.Option {
	return fx.Options(
		CoreRegister,
		SubagentRegister,
		fx.Provide(
			fx.Annotate(func() ai.Tool { return &stubTool{name: "web_search_exa"} },
				fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
			fx.Annotate(func() ai.Tool { return &stubTool{name: "web_fetch_exa"} },
				fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		),
		fx.Supply(
			WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
	)
}

func findSubagentTools(tools []ai.Tool) []*SubagentTool {
	var found []*SubagentTool
	for _, tool := range tools {
		if subagent, ok := tool.(*SubagentTool); ok {
			found = append(found, subagent)
		}
	}
	return found
}

func TestSubagentRegisterBindsAfterFreeze(t *testing.T) {
	var tools []ai.Tool
	app := fxtest.New(
		t,
		subagentFXTestOptions(t.TempDir()),
		fx.Invoke(func(params struct {
			fx.In
			Tools []ai.Tool `group:"agent_tools"`
		}) {
			tools = params.Tools
		}),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	subagents := findSubagentTools(tools)
	if len(subagents) != 1 {
		t.Fatalf("subagent tools = %d, want 1（默认 research）", len(subagents))
	}
	// fx.Invoke 生效：启动期绑定完成（freeze 后，含 MCP 类工具）。
	if subagents[0].bound.Load() == nil {
		t.Fatal("subagent tool must be bound after app start")
	}
	pipeline := subagents[0].bound.Load()
	if len(pipeline.childTools) != 2 {
		t.Fatalf("child tools = %v, want [web_search_exa web_fetch_exa]", pipeline.childTools)
	}
}

func TestSubagentRegisterRejectsUnknownWhitelistTool(t *testing.T) {
	badTool := newResearchSubagentTool()
	badTool.tools = []string{"nonexistent_tool"}
	app := fx.New(
		fx.NopLogger,
		subagentFXTestOptions(t.TempDir()),
		fx.Provide(fx.Annotate(
			func() ai.Tool { return badTool },
			fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`))),
	)
	if err := app.Start(context.Background()); err == nil {
		t.Fatal("unknown whitelist tool must fail app start")
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
}
