package chat

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

func TestRunListenerMapsPublicPiEvents(t *testing.T) {
	events := make(chan vo.RunEventVO, 8)
	listener := newRunListener("run-1", events)
	listener.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewStartEvent(ai.ToolCall{
		ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
	})))
	listener.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewUpdateEvent(ai.ToolCall{ID: "call-1", Name: "read"}, ai.ToolUpdate{
		Content: []ai.ContentBlock{ai.TextBlock("working")}, Details: "50%",
	})))
	listener.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewEndEvent(
		ai.ToolCall{ID: "call-1", Name: "read"},
		ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("file")}}, false, "",
	)))
	listener.OnEvent(context.Background(), pi.NewMessageStartEvent())
	listener.OnEvent(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("do")))
	listener.OnEvent(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("ne")))
	listener.OnEvent(context.Background(), pi.NewMessageEndEvent(ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")},
	}))

	wants := []vo.RunEventType{
		vo.RunEventToolStarted, vo.RunEventToolUpdated,
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

func TestRunListenerDoesNotDropMessageDeltaWhenQueueIsFull(t *testing.T) {
	events := make(chan vo.RunEventVO, 1)
	listener := newRunListener("run-1", events)
	// 先用一条 important 事件占满容量为 1 的队列，制造"队列已满"前提。
	listener.OnEvent(context.Background(), pi.NewMessageStartEvent())
	done := make(chan struct{})
	go func() {
		listener.OnEvent(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock("chunk")))
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

func TestRunListenerMayDropToolUpdatesWhenQueueIsFull(t *testing.T) {
	events := make(chan vo.RunEventVO, 1)
	listener := newRunListener("run-1", events)
	done := make(chan struct{})
	go func() {
		listener.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewUpdateEvent(
			ai.ToolCall{ID: "call"}, ai.ToolUpdate{Content: []ai.ContentBlock{ai.TextBlock("chunk")}},
		)))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("droppable update blocked on a full queue")
	}
}

func TestSkillReadVisibilityNormalizesTheReadPath(t *testing.T) {
	for _, arguments := range []json.RawMessage{
		json.RawMessage(`{"path":" skills/writing/../review/SKILL.md "}`),
		json.RawMessage(`{"path":"profiles\\writing\\skills\\review\\SKILL.md"}`),
	} {
		if !isSkillRead("read", arguments) {
			t.Fatalf("isSkillRead(read, %s) = false", arguments)
		}
	}
	for _, arguments := range []json.RawMessage{
		json.RawMessage(`{"path":"docs/SKILL.md"}`),
		json.RawMessage(`{"path":"skills/review/examples.md"}`),
	} {
		if isSkillRead("read", arguments) {
			t.Fatalf("isSkillRead(read, %s) = true", arguments)
		}
	}
}

func TestRunListenerCompletesSkillReadNarrationAsAnInvisibleMessage(t *testing.T) {
	event, important, ok := mapRunEvent("run-1", pi.NewMessageEndEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("我先读取对应的 Skill。")},
		ToolCalls: []ai.ToolCall{{
			ID: "call-skill", Name: "read",
			Arguments: json.RawMessage(`{"path":"profiles/writing/skills/social-content/SKILL.md"}`),
		}},
	}))
	if !ok || !important || event.Type != vo.RunEventMessageCompleted {
		t.Fatalf("mapped event = %#v, important=%v, ok=%v", event, important, ok)
	}
	if event.Message != nil {
		t.Fatalf("skill read narration leaked as message = %#v", event.Message)
	}
}
