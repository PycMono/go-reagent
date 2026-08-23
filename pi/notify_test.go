package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

type recordingNotifier struct {
	notifications []Notification
}

func (n *recordingNotifier) Notify(_ context.Context, notification Notification) {
	n.notifications = append(n.notifications, notification)
}

type panicNotifier struct{}

func (panicNotifier) Notify(context.Context, Notification) { panic("notifier boom") }

func newNotifyingAgent(t *testing.T, provider ai.Provider, notifiers ...Notifier) *Agent {
	t.Helper()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := NewToolRuntime(ToolRuntimeOptions{Middlewares: DefaultMiddlewareRegistrations()})
	if err != nil {
		t.Fatal(err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	traced := observability.NewTracingProvider(provider, "openai", "test", "fake")
	loop := NewLoop(traced, NewScheduler(toolRuntime, 2), false, WithLoopProviderIdentity("test", "fake"))
	return New(builder, loop, toolRuntime, notifiers...)
}

func TestNotifierReceivesOnlyFinalAssistantText(t *testing.T) {
	final := actionMessage("最终回复")
	provider := &scriptedProvider{streams: []*scriptedStream{textDeltaStream(final)}}
	notifier := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, notifier)

	if _, err := agent.Run(context.Background(), runInput(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(notifier.notifications) != 1 || notifier.notifications[0].Text != "最终回复" {
		t.Fatalf("notifications = %#v, want one final text", notifier.notifications)
	}
}

func TestNotifierSkipsToolCallMessagesAndEmptyText(t *testing.T) {
	toolCall := ai.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)}
	notifier := &recordingNotifier{}

	bridge := &notifyBridge{notifiers: []Notifier{notifier}}
	bridge.Report(context.Background(), NewMessageEndEvent(*actionMessage("工具回合", toolCall)))
	bridge.Report(context.Background(), NewMessageEndEvent(*actionMessage("  ")))
	bridge.Report(context.Background(), NewThinkingEvent())
	bridge.Report(context.Background(), AgentEvent{Type: AgentEventMessageEnd})

	if len(notifier.notifications) != 0 {
		t.Fatalf("notifications = %#v, want none", notifier.notifications)
	}
}

func TestNotifierPanicDoesNotAffectRun(t *testing.T) {
	final := actionMessage("ok")
	provider := &scriptedProvider{streams: []*scriptedStream{textDeltaStream(final)}}
	recorder := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, panicNotifier{}, recorder)

	if _, err := agent.Run(context.Background(), runInput(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.notifications) != 1 || !strings.Contains(recorder.notifications[0].Text, "ok") {
		t.Fatalf("panic 不应影响后续 Notifier：notifications = %#v", recorder.notifications)
	}
}
