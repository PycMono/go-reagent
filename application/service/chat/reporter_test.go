package chat

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestReporterMapsPublicPiEvents(t *testing.T) {
	events := make(chan vo.RunEventVO, 8)
	reporter := newRunReporter("run-1", events)
	reporter.Report(context.Background(), pi.NewThinkingEvent())
	reporter.Report(context.Background(), pi.NewToolStartEvent(ai.ToolCall{
		ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
	}))
	reporter.Report(context.Background(), pi.NewToolUpdateEvent(ai.ToolCall{ID: "call-1", Name: "read"}, ai.ToolUpdate{
		Content: []ai.ContentBlock{ai.TextBlock("working")}, Details: "50%",
	}))
	reporter.Report(context.Background(), pi.NewToolEndEvent(ai.ToolCall{ID: "call-1", Name: "read"}, pi.ToolResult{
		ToolCallID: "call-1", ToolName: "read", Content: []ai.ContentBlock{ai.TextBlock("file")},
	}))
	reporter.Report(context.Background(), pi.NewMessageStartEvent())
	reporter.Report(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("do")))
	reporter.Report(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("ne")))
	reporter.Report(context.Background(), pi.NewMessageEndEvent(ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")},
	}))

	wants := []vo.RunEventType{
		vo.RunEventAgentThinking, vo.RunEventToolStarted, vo.RunEventToolUpdated,
		vo.RunEventToolCompleted, vo.RunEventMessageStarted, vo.RunEventMessageDelta,
		vo.RunEventMessageDelta, vo.RunEventMessageCompleted,
	}
	for _, want := range wants {
		select {
		case event := <-events:
			if event.Type != want || event.RunID != "run-1" {
				t.Fatalf("event = %#v, want %q", event, want)
			}
			if want == vo.RunEventToolStarted && (event.Tool == nil || string(event.Tool.Arguments) != `{"path":"README.md"}`) {
				t.Fatalf("tool started = %#v", event.Tool)
			}
			if want == vo.RunEventMessageCompleted && (event.Message == nil || event.Message.Content[0].Text != "done") {
				t.Fatalf("message completed = %#v", event.Message)
			}
			if want == vo.RunEventMessageDelta && (event.Delta == nil || (event.Delta.Text != "do" && event.Delta.Text != "ne")) {
				t.Fatalf("message delta = %#v", event.Delta)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing event %q", want)
		}
	}
}

func TestReporterDoesNotDropMessageDeltaWhenQueueIsFull(t *testing.T) {
	events := make(chan vo.RunEventVO, 1)
	reporter := newRunReporter("run-1", events)
	reporter.Report(context.Background(), pi.NewThinkingEvent())
	done := make(chan struct{})
	go func() {
		reporter.Report(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("chunk")))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("message delta was dropped while the queue was full")
	case <-time.After(20 * time.Millisecond):
	}
	<-events
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("message delta did not resume after queue capacity became available")
	}
	event := <-events
	if event.Type != vo.RunEventMessageDelta || event.Delta == nil || event.Delta.Text != "chunk" {
		t.Fatalf("event = %#v", event)
	}
}

func TestReporterMayDropToolUpdatesWhenQueueIsFull(t *testing.T) {
	events := make(chan vo.RunEventVO, 1)
	reporter := newRunReporter("run-1", events)
	reporter.Report(context.Background(), pi.NewThinkingEvent())
	done := make(chan struct{})
	go func() {
		reporter.Report(context.Background(), pi.NewToolUpdateEvent(
			ai.ToolCall{ID: "call"}, ai.ToolUpdate{Content: []ai.ContentBlock{ai.TextBlock("chunk")}},
		))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("droppable update blocked on a full queue")
	}
}
