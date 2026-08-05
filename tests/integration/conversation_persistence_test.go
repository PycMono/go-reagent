package integration_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
)

func TestConversationRunnerPersistsAndIsolatesHistory(t *testing.T) {
	store := newAcceptanceConversationStore()
	runtime := &acceptanceRuntime{}
	runner := conversation.NewRunner(runtime, store, 100)

	run := func(userID, conversationID, input string) {
		t.Helper()
		_, err := runner.Run(context.Background(), conversation.RunRequest{
			UserID: userID, ConversationID: conversationID,
			Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(input)}},
		}, nil)
		if err != nil {
			t.Fatalf("Run(%q, %q) error = %v", userID, conversationID, err)
		}
	}

	run("user-1", "conversation-1", "question-1")
	run("user-1", "conversation-1", "question-2")
	run("user-2", "conversation-1", "other-user")
	run("user-1", "conversation-2", "other-conversation")

	if len(runtime.histories) != 4 {
		t.Fatalf("runtime calls = %d", len(runtime.histories))
	}
	if len(runtime.histories[0]) != 0 {
		t.Fatalf("first history = %#v, want empty", runtime.histories[0])
	}
	wantSecond := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("question-1")}},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer-1")}},
	}
	if !reflect.DeepEqual(runtime.histories[1], wantSecond) {
		t.Fatalf("second history = %#v, want %#v", runtime.histories[1], wantSecond)
	}
	if len(runtime.histories[2]) != 0 || len(runtime.histories[3]) != 0 {
		t.Fatalf("isolated histories = %#v, %#v, want empty", runtime.histories[2], runtime.histories[3])
	}
}

type acceptanceRuntime struct {
	histories [][]ai.Message
}

func (r *acceptanceRuntime) Run(_ context.Context, request agent.RunRequest, _ agent.Reporter) (agent.RunResult, error) {
	r.histories = append(r.histories, cloneAcceptanceMessages(request.History))
	answer := fmt.Sprintf("answer-%d", len(r.histories))
	return agent.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock(answer)},
	}}}, nil
}

type acceptanceConversation struct {
	pk          uint64
	version     uint64
	messages    []ai.Message
	invocations []agent.ModelInvocation
}

type acceptanceConversationStore struct {
	mu     sync.Mutex
	nextPK uint64
	items  map[conversation.Key]*acceptanceConversation
}

func newAcceptanceConversationStore() *acceptanceConversationStore {
	return &acceptanceConversationStore{items: make(map[conversation.Key]*acceptanceConversation)}
}

func (s *acceptanceConversationStore) LoadOrCreate(_ context.Context, key conversation.Key, limit int) (conversation.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[key]
	if item == nil {
		s.nextPK++
		item = &acceptanceConversation{pk: s.nextPK}
		s.items[key] = item
	}
	messages := item.messages
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return conversation.Snapshot{
		ConversationPK: item.pk,
		Version:        item.version,
		Messages:       cloneAcceptanceMessages(messages),
	}, nil
}

func (s *acceptanceConversationStore) AppendTurn(_ context.Context, request conversation.AppendRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.pk != request.ConversationPK {
			continue
		}
		if item.version != request.ExpectedVersion {
			return conversation.ErrConflict
		}
		item.messages = append(item.messages, cloneAcceptanceMessages(request.Messages)...)
		item.invocations = append(item.invocations, cloneAcceptanceInvocations(request.Invocations)...)
		item.version++
		return nil
	}
	return conversation.ErrNotFound
}

func cloneAcceptanceInvocations(invocations []agent.ModelInvocation) []agent.ModelInvocation {
	return append([]agent.ModelInvocation(nil), invocations...)
}

func cloneAcceptanceMessages(messages []ai.Message) []ai.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]ai.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].Content = append([]ai.ContentBlock(nil), message.Content...)
		cloned[index].ToolCalls = append([]ai.ToolCall(nil), message.ToolCalls...)
	}
	return cloned
}

var _ conversation.Store = (*acceptanceConversationStore)(nil)
