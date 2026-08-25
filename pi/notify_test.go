package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/PycMono/go-reagent/pi/toolexec"
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
	toolRuntime, err := toolexec.NewExecutor(toolexec.ExecutorOptions{Middlewares: middleware.Defaults()})
	if err != nil {
		t.Fatal(err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	traced := observability.NewTracingProvider(provider, "openai", "test", "fake")
	loop := NewLoop(traced, toolexec.NewScheduler(toolRuntime, 2), WithLoopProviderIdentity("test", "fake"))
	return New(builder, loop, toolRuntime, notifiers...)
}

func TestNotifierSilentOnCompletedRun(t *testing.T) {
	provider := &scriptedProvider{streams: []*scriptedStream{textDeltaStream(actionMessage("正常回复"))}}
	notifier := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, notifier)

	if _, err := agent.Run(context.Background(), runInput(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(notifier.notifications) != 0 {
		t.Fatalf("正常完成的 run 不应告警：notifications = %#v", notifier.notifications)
	}
}

func TestNotifierAlertsOnRunError(t *testing.T) {
	provider := &scriptedProvider{streams: []*scriptedStream{{err: errors.New("provider down")}}}
	notifier := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, notifier)

	if _, err := agent.Run(context.Background(), runInput(), nil); err == nil {
		t.Fatal("Run() error = nil, want provider failure")
	}
	if len(notifier.notifications) != 1 || notifier.notifications[0].Kind != NotificationRunError {
		t.Fatalf("notifications = %#v, want one run_error", notifier.notifications)
	}
}

// prepare 阶段失败（如 workspace 缺少 AGENTS.md）走 loop 之前的提前返回
// 路径，同样必须告警。
func TestNotifierAlertsOnPrepareFailure(t *testing.T) {
	workDir := t.TempDir() // 不写 AGENTS.md，prepare 必然失败
	toolRuntime, err := toolexec.NewExecutor(toolexec.ExecutorOptions{Middlewares: middleware.Defaults()})
	if err != nil {
		t.Fatal(err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	provider := &scriptedProvider{}
	traced := observability.NewTracingProvider(provider, "openai", "test", "fake")
	loop := NewLoop(traced, toolexec.NewScheduler(toolRuntime, 2), WithLoopProviderIdentity("test", "fake"))
	notifier := &recordingNotifier{}
	agent := New(builder, loop, toolRuntime, notifier)

	if _, err := agent.Run(context.Background(), runInput(), nil); err == nil {
		t.Fatal("Run() error = nil, want prepare failure")
	}
	if provider.calls != 0 {
		t.Fatalf("prepare 失败不应触达 Provider，calls = %d", provider.calls)
	}
	if len(notifier.notifications) != 1 || notifier.notifications[0].Kind != NotificationRunError {
		t.Fatalf("notifications = %#v, want one run_error", notifier.notifications)
	}
}

func TestNotifierAlertsOnBudgetTermination(t *testing.T) {
	toolCall := ai.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)}
	looping := actionMessage("继续", toolCall)
	provider := &scriptedProvider{streams: []*scriptedStream{
		textDeltaStream(looping), textDeltaStream(looping),
	}}
	notifier := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, notifier)

	request := runInput()
	request.Limits = governor.Limits{MaxTurns: 1}
	if _, err := agent.Run(context.Background(), request, nil); err == nil {
		t.Fatal("Run() error = nil, want run limit exceeded")
	}
	if len(notifier.notifications) != 1 || notifier.notifications[0].Kind != NotificationRunLimit {
		t.Fatalf("notifications = %#v, want one run_limit", notifier.notifications)
	}
}

func TestNotifierAlertsOnToolError(t *testing.T) {
	listener := &alertListener{notifiers: []Notifier{&recordingNotifier{}}}
	recorder := listener.notifiers[0].(*recordingNotifier)

	listener.OnEvent(context.Background(), NewMessageEndEvent(*actionMessage("正常回复")))
	listener.OnEvent(context.Background(), NewAgentToolEvent(toolexec.NewEndEvent(
		ai.ToolCall{ID: "c1", Name: "read"},
		toolexec.Result{ToolCallID: "c1", ToolName: "read", IsError: false},
	)))
	if len(recorder.notifications) != 0 {
		t.Fatalf("非失败事件不应告警：%#v", recorder.notifications)
	}

	listener.OnEvent(context.Background(), NewAgentToolEvent(toolexec.NewEndEvent(
		ai.ToolCall{ID: "c2", Name: "read"},
		toolexec.Result{ToolCallID: "c2", ToolName: "read", IsError: true, ErrorCode: "tool_invalid_arguments"},
	)))
	if len(recorder.notifications) != 1 || recorder.notifications[0].Kind != NotificationToolError {
		t.Fatalf("notifications = %#v, want one tool_error", recorder.notifications)
	}
	if got := recorder.notifications[0].Summary; got == "" || !containsAll(got, "read", "tool_invalid_arguments") {
		t.Fatalf("Summary = %q, want tool name and error code", got)
	}
}

func TestNotifierPanicDoesNotAffectRun(t *testing.T) {
	provider := &scriptedProvider{streams: []*scriptedStream{{err: errors.New("provider down")}}}
	recorder := &recordingNotifier{}
	agent := newNotifyingAgent(t, provider, panicNotifier{}, recorder)

	if _, err := agent.Run(context.Background(), runInput(), nil); err == nil {
		t.Fatal("Run() error = nil")
	}
	if len(recorder.notifications) != 1 {
		t.Fatalf("panic 不应影响后续 Notifier：notifications = %#v", recorder.notifications)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
