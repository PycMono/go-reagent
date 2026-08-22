package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestRunnerLoadsRunsAndAppendsTurn(t *testing.T) {
	history := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("previous")}}
	input := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("next")}}
	answer := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{
		ID:             "conversation-pk-1",
		ConversationID: "conversation-1",
		UserID:         "user-1",
		Version:        7,
	}, history: messagesToDomain([]ai.Message{history}, "old-run")}
	runtime := &runnerRuntimeFake{result: pi.RunResult{
		NewMessages: []ai.Message{answer},
	}}
	runner := NewRunner(runtime, store, 100, pi.RunLimits{})

	result, err := runner.Run(context.Background(), RunRequest{
		UserID:         "user-1",
		ConversationID: "conversation-1",
		RunID:          "run-1",
		Input:          input,
		ResponsePolicy: "reply in the user's language",
	}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(result, runtime.result) {
		t.Fatalf("Run() result = %#v, want %#v", result, runtime.result)
	}
	if store.findCalls != 1 || store.foundUserID != "user-1" || store.foundID != "conversation-1" || store.messageLimit != 100 {
		t.Fatalf("FindByUserIDAndID calls/user/id/limit = %d, %q, %q, %d", store.findCalls, store.foundUserID, store.foundID, store.messageLimit)
	}
	wantHistory := []pi.Message{{
		ContentType: "text",
		Content:     "previous",
		SenderType:  "ai",
	}}
	if runtime.calls != 1 || !reflect.DeepEqual(runtime.request.History, wantHistory) ||
		runtime.request.Input.Content != "next\n\n<runtime_response_policy>\nreply in the user's language\n</runtime_response_policy>" ||
		runtime.request.Input.SenderType != "customer" {
		t.Fatalf("runtime call/request = %d, %#v", runtime.calls, runtime.request)
	}
	wantMessages := messagesToDomain([]ai.Message{input, answer}, "run-1")
	if store.appendCalls != 1 || store.appendedUserID != "user-1" || store.appendedID != "conversation-1" ||
		store.expectedVersion != 7 || !reflect.DeepEqual(store.appendedMessages, wantMessages) {
		t.Fatalf("AppendTurn calls/messages = %d, %#v, want %#v", store.appendCalls, store.appendedMessages, wantMessages)
	}
}

func TestRunnerCreatesConversationWhenNotFound(t *testing.T) {
	store := &runnerStoreFake{notFoundOnce: true}
	runtime := &runnerRuntimeFake{result: pi.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.findCalls != 2 || store.createCalls != 1 || store.created.ConversationID != "conversation" || store.created.UserID != "user" {
		t.Fatalf("find/create = %d/%d, created = %#v", store.findCalls, store.createCalls, store.created)
	}
}

func TestRunnerPersistsPartialMessagesOnRuntimeError(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	partial := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")}}
	invocation := pi.ModelInvocation{
		Sequence: 1,
		Phase:    pi.ModelInvocationPhaseAction,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user", Version: 2}}
	runtime := &runnerRuntimeFake{
		result: pi.RunResult{
			NewMessages: []ai.Message{partial},
			Invocations: []pi.ModelInvocation{invocation},
		},
		err: runtimeErr,
	}

	result, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Run() error = %v, want runtime error", err)
	}
	if !reflect.DeepEqual(result.NewMessages, []ai.Message{partial}) || store.appendCalls != 1 ||
		len(store.appendedMessages) != 2 || !reflect.DeepEqual(store.appendedInvocations, invocationsToDomain([]pi.ModelInvocation{invocation}, "run", "")) {
		t.Fatalf("result/messages/invocations = %#v, %#v, %#v", result, store.appendedMessages, store.appendedInvocations)
	}
}

func TestRunnerForwardsAndClonesUsageAndInvocations(t *testing.T) {
	answer := ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("answer")},
		Usage:   &ai.Usage{PlatformID: "test", Model: "model"},
	}
	invocations := []pi.ModelInvocation{{
		Sequence: 1,
		Phase:    pi.ModelInvocationPhaseThinking,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}, {
		Sequence: 2,
		Phase:    pi.ModelInvocationPhaseAction,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}}
	runtime := &runnerRuntimeFake{result: pi.RunResult{
		NewMessages: []ai.Message{answer},
		Invocations: invocations,
	}}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user", Version: 7}}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.appendedInvocations, invocationsToDomain(invocations, "run", "")) {
		t.Fatalf("Invocations = %#v, want %#v", store.appendedInvocations, invocations)
	}

	runtime.result.NewMessages[0].Usage.PlatformID = "mutated"
	runtime.result.Invocations[0].Usage.PlatformID = "mutated"
	if store.appendedInvocations[0].PlatformID != "test" {
		t.Fatalf("stored request aliases runtime result: %#v", store.appendedInvocations)
	}
}

func TestRunnerPersistsInvocationsWithoutMessagesOnBudgetError(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	invocation := pi.ModelInvocation{
		Sequence: 1,
		Phase:    pi.ModelInvocationPhaseThinking,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user", Version: 3}}
	runtime := &runnerRuntimeFake{
		result: pi.RunResult{Invocations: []pi.ModelInvocation{invocation}},
		err:    runtimeErr,
	}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Run() error = %v, want runtime error", err)
	}
	if store.appendCalls != 1 || len(store.appendedMessages) != 1 ||
		!reflect.DeepEqual(store.appendedInvocations, invocationsToDomain([]pi.ModelInvocation{invocation}, "run", "")) {
		t.Fatalf("append/messages/invocations = %d, %#v, %#v", store.appendCalls, store.appendedMessages, store.appendedInvocations)
	}
}

func TestRunnerPassesConfiguredLimitsToRuntime(t *testing.T) {
	limits := pi.RunLimits{MaxTurns: 7, MaxCostUSD: 0.25, MaxTotalTokens: 1000}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user"}}
	runtime := &runnerRuntimeFake{result: pi.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}

	_, err := NewRunner(runtime, store, 100, limits).Run(context.Background(), validConversationRunRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.request.Limits != limits {
		t.Fatalf("runtime limits = %#v, want %#v", runtime.request.Limits, limits)
	}
}

func TestRunnerSkipsAppendWhenRuntimeFailsWithoutMessages(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user"}}
	runtime := &runnerRuntimeFake{err: runtimeErr}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) || store.appendCalls != 0 {
		t.Fatalf("Run() error/append calls = %v, %d", err, store.appendCalls)
	}
}

func TestRunnerStopsAfterLoadError(t *testing.T) {
	loadErr := errors.New("load failed")
	store := &runnerStoreFake{findErr: loadErr}
	runtime := &runnerRuntimeFake{}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, loadErr) || runtime.calls != 0 || store.appendCalls != 0 {
		t.Fatalf("Run() error/runtime/append = %v, %d, %d", err, runtime.calls, store.appendCalls)
	}
}

func TestRunnerJoinsRuntimeAndAppendErrors(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	appendErr := errors.New("append failed")
	partial := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")}}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user"}, appendErr: appendErr}
	runtime := &runnerRuntimeFake{
		result: pi.RunResult{NewMessages: []ai.Message{partial}},
		err:    runtimeErr,
	}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) || !errors.Is(err, appendErr) {
		t.Fatalf("Run() error = %v, want both errors", err)
	}
}

func TestRunnerReturnsConflictWithoutRetry(t *testing.T) {
	store := &runnerStoreFake{
		conversation: conversationentity.Conversation{ConversationID: "conversation", UserID: "user"},
		appendErr:    commonerrors.ErrConflict,
	}
	runtime := &runnerRuntimeFake{result: pi.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, commonerrors.ErrConflict) || store.findCalls != 1 || runtime.calls != 1 || store.appendCalls != 1 {
		t.Fatalf("Run() error/calls = %v, %d/%d/%d", err, store.findCalls, runtime.calls, store.appendCalls)
	}
}

func TestRunnerRejectsInvalidRequestsBeforeLoading(t *testing.T) {
	valid := validConversationRunRequest()
	tests := []struct {
		name   string
		mutate func(*RunRequest)
		want   string
	}{
		{name: "empty user ID", mutate: func(r *RunRequest) { r.UserID = " " }, want: "user ID"},
		{name: "empty conversation ID", mutate: func(r *RunRequest) { r.ConversationID = " " }, want: "conversation ID"},
		{name: "non-user input", mutate: func(r *RunRequest) { r.Input.Role = ai.RoleAssistant }, want: "input role"},
		{name: "empty input", mutate: func(r *RunRequest) { r.Input.Content = nil }, want: "input content"},
		{name: "unsupported content", mutate: func(r *RunRequest) {
			r.Input.Content = []ai.ContentBlock{{Type: "image", Text: "private content"}}
		}, want: "unsupported content type"},
		{name: "tool calls", mutate: func(r *RunRequest) {
			r.Input.ToolCalls = []ai.ToolCall{{ID: "call", Name: "read", Arguments: json.RawMessage(`{}`)}}
		}, want: "tool fields"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			request.Input = cloneMessage(valid.Input)
			tt.mutate(&request)
			store := &runnerStoreFake{}
			_, err := NewRunner(&runnerRuntimeFake{}, store, 100, pi.RunLimits{}).Run(context.Background(), request, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) || store.findCalls != 0 {
				t.Fatalf("Run() error/find calls = %v, %d", err, store.findCalls)
			}
			if strings.Contains(err.Error(), "private content") {
				t.Fatalf("Run() error leaks input content: %v", err)
			}
		})
	}
}

func TestRunnerValidatesHistoryLimitAndCancellation(t *testing.T) {
	request := validConversationRunRequest()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name   string
		ctx    context.Context
		runner Runner
		want   error
	}{
		{name: "invalid history limit", ctx: context.Background(), runner: NewRunner(&runnerRuntimeFake{}, &runnerStoreFake{}, 0, pi.RunLimits{})},
		{name: "canceled context", ctx: canceled, runner: NewRunner(&runnerRuntimeFake{}, &runnerStoreFake{}, 100, pi.RunLimits{}), want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.runner.Run(tt.ctx, request, nil)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("Run() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRunnerClonesBoundaryValues(t *testing.T) {
	history := ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("history")},
	}
	input := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("input")}}
	answer := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}}
	contextBlocks := []pi.ContextBlock{{Name: "profile", Content: "gold"}}
	store := &runnerStoreFake{conversation: conversationentity.Conversation{ID: "conversation-pk", ConversationID: "conversation", UserID: "user"}, history: messagesToDomain([]ai.Message{history}, "old-run")}
	runtime := &runnerRuntimeFake{result: pi.RunResult{NewMessages: []ai.Message{answer}}}
	request := RunRequest{
		UserID: " user ", ConversationID: " conversation ", RunID: "run",
		Input: input, Context: contextBlocks,
	}

	_, err := NewRunner(runtime, store, 100, pi.RunLimits{}).Run(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.history[0].Payload.Content[0].Text = "changed"
	request.Input.Content[0].Text = "changed"
	contextBlocks[0].Content = "changed"
	runtime.result.NewMessages[0].Content[0].Text = "changed"

	if got := runtime.request.History[0].Content; got != "history" {
		t.Fatalf("runtime history mutated: %q", got)
	}
	if runtime.request.Input.Content != "input" || runtime.request.Input.SenderType != "customer" ||
		runtime.request.Context[0].Content != "gold" {
		t.Fatalf("runtime request mutated: %#v", runtime.request)
	}
	if store.appendedMessages[0].Payload.Content[0].Text != "input" || store.appendedMessages[1].Payload.Content[0].Text != "answer" {
		t.Fatalf("append request mutated: %#v", store.appendedMessages)
	}
	if store.foundUserID != "user" || store.foundID != "conversation" {
		t.Fatalf("found identity = %q, %q", store.foundUserID, store.foundID)
	}
}

func validConversationRunRequest() RunRequest {
	return RunRequest{
		UserID: "user", ConversationID: "conversation", RunID: "run",
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("input")}},
	}
}

type runnerRuntimeFake struct {
	result  pi.RunResult
	err     error
	calls   int
	request pi.RunRequest
	run     func(pi.RunRequest)
}

func (f *runnerRuntimeFake) Run(_ context.Context, request pi.RunRequest, _ pi.Reporter) (pi.RunResult, error) {
	f.calls++
	f.request = request
	if f.run != nil {
		f.run(request)
	}
	return f.result, f.err
}

type runnerStoreFake struct {
	conversation        conversationentity.Conversation
	history             []*conversationentity.Message
	created             conversationentity.Conversation
	appendedMessages    []*conversationentity.Message
	appendedInvocations []*conversationentity.ModelInvocation
	findErr             error
	createErr           error
	appendErr           error
	notFoundOnce        bool
	findCalls           int
	createCalls         int
	appendCalls         int
	appendCtxErr        error
	foundUserID         string
	foundID             string
	messageLimit        int
	appendedUserID      string
	appendedID          string
	expectedVersion     uint64
}

func (f *runnerStoreFake) FindByUserIDAndConversationID(_ context.Context, userID string, id string) (*conversationentity.Conversation, bool, error) {
	f.findCalls++
	f.foundUserID = userID
	f.foundID = id
	if f.findErr != nil {
		return nil, false, f.findErr
	}
	if f.notFoundOnce && f.findCalls == 1 {
		return nil, false, nil
	}
	conversation := f.conversation
	return &conversation, true, nil
}

func (f *runnerStoreFake) ListMessagesByConversationID(_ context.Context, _ string, messageLimit int) ([]*conversationentity.Message, error) {
	f.messageLimit = messageLimit
	return f.history, nil
}

func (f *runnerStoreFake) Create(_ context.Context, conversation *conversationentity.Conversation) error {
	f.createCalls++
	if conversation != nil {
		if conversation.ID == "" {
			conversation.ID = "generated-conversation-id"
		}
		f.created = *conversation
		f.conversation = *conversation
	}
	return f.createErr
}

func (f *runnerStoreFake) AppendTurn(ctx context.Context, userID string, id string, expectedVersion uint64, messages []*conversationentity.Message, invocations []*conversationentity.ModelInvocation) error {
	f.appendCalls++
	f.appendCtxErr = ctx.Err()
	f.appendedUserID = userID
	f.appendedID = id
	f.expectedVersion = expectedVersion
	f.appendedMessages = messages
	f.appendedInvocations = invocations
	return f.appendErr
}
