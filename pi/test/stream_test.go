package test

import (
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

type contractStream struct {
	events  []ai.StreamEvent
	index   int
	message *ai.Message
	err     error
	closed  bool
}

func newTestStream(message *ai.Message, err error) ai.Stream {
	events := []ai.StreamEvent{{Type: ai.StreamEventStart}}
	if err != nil {
		events = append(events, ai.StreamEvent{Type: ai.StreamEventError})
		return &contractStream{events: events, message: message, err: err}
	}
	if message != nil {
		for _, block := range message.Content {
			if block.Type == ai.ContentTypeText && block.Text != "" {
				events = append(events, ai.StreamEvent{Type: ai.StreamEventTextDelta, TextDelta: block.Text})
			}
		}
	}
	events = append(events, ai.StreamEvent{Type: ai.StreamEventDone})
	return &contractStream{events: events, message: message}
}

func (s *contractStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return true
}

func (s *contractStream) Current() ai.StreamEvent { return s.events[s.index-1] }

func (s *contractStream) Result() (*ai.Message, error) { return s.message, s.err }

func (s *contractStream) Close() error {
	s.closed = true
	return nil
}

func TestModelStreamContractReturnsDeltasAndFinalMessage(t *testing.T) {
	want := &ai.Message{
		Role:         ai.RoleAssistant,
		Content:      []ai.ContentBlock{ai.TextBlock("hello")},
		FinishReason: ai.FinishReasonStop,
	}
	stream := &contractStream{
		events: []ai.StreamEvent{
			{Type: ai.StreamEventStart},
			{Type: ai.StreamEventTextDelta, TextDelta: "hel"},
			{Type: ai.StreamEventTextDelta, TextDelta: "lo"},
			{Type: ai.StreamEventDone},
		},
		message: want,
	}
	var got string
	for stream.Next() {
		event := stream.Current()
		if event.Type == ai.StreamEventTextDelta {
			got += event.TextDelta
		}
	}
	message, err := stream.Result()
	if err != nil || got != "hello" || message != want {
		t.Fatalf("stream result = %q, %#v, %v", got, message, err)
	}
	if err := stream.Close(); err != nil || !stream.closed {
		t.Fatalf("Close() = %v, closed=%v", err, stream.closed)
	}
}

func TestModelStreamContractReturnsTerminalError(t *testing.T) {
	wantErr := errors.New("stream failed")
	stream := &contractStream{
		events: []ai.StreamEvent{{Type: ai.StreamEventStart}, {Type: ai.StreamEventError}},
		err:    wantErr,
	}
	for stream.Next() {
	}
	message, err := stream.Result()
	if message != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Result() = %#v, %v", message, err)
	}
}
