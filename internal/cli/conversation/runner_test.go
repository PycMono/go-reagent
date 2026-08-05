package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func TestRunnerLoadsRunsAndAppendsTurn(t *testing.T) {
	history := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("previous")}}
	input := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("next")}}
	answer := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}}
	store := &runnerStoreFake{snapshot: Snapshot{
		ConversationPK: 42,
		Version:        7,
		Messages:       []ai.Message{history},
	}}
	runtime := &runnerRuntimeFake{result: agent.RunResult{
		RunID:       "run-1",
		NewMessages: []ai.Message{answer},
	}}
	runner := NewRunner(runtime, store, 100)

	result, err := runner.Run(context.Background(), RunRequest{
		UserID:         "user-1",
		ConversationID: "conversation-1",
		RunID:          "run-1",
		Input:          input,
	}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(result, runtime.result) {
		t.Fatalf("Run() result = %#v, want %#v", result, runtime.result)
	}
	if store.loadCalls != 1 || store.loadedKey != (Key{UserID: "user-1", ConversationID: "conversation-1"}) || store.loadedLimit != 100 {
		t.Fatalf("LoadOrCreate calls/key/limit = %d, %#v, %d", store.loadCalls, store.loadedKey, store.loadedLimit)
	}
	if runtime.calls != 1 || !reflect.DeepEqual(runtime.request.History, []ai.Message{history}) || !reflect.DeepEqual(runtime.request.Input, input) {
		t.Fatalf("runtime call/request = %d, %#v", runtime.calls, runtime.request)
	}
	wantAppend := AppendRequest{
		ConversationPK:  42,
		ExpectedVersion: 7,
		RunID:           "run-1",
		Messages:        []ai.Message{input, answer},
	}
	if store.appendCalls != 1 || !reflect.DeepEqual(store.appended, wantAppend) {
		t.Fatalf("AppendTurn calls/request = %d, %#v, want %#v", store.appendCalls, store.appended, wantAppend)
	}
}

func TestRunnerPersistsPartialMessagesOnRuntimeError(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	partial := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")}}
	invocation := agent.ModelInvocation{
		Sequence: 1,
		Phase:    agent.ModelInvocationPhaseAction,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}
	store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 1, Version: 2}}
	runtime := &runnerRuntimeFake{
		result: agent.RunResult{
			RunID:       "run",
			NewMessages: []ai.Message{partial},
			Invocations: []agent.ModelInvocation{invocation},
		},
		err: runtimeErr,
	}

	result, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Run() error = %v, want runtime error", err)
	}
	if !reflect.DeepEqual(result.NewMessages, []ai.Message{partial}) || store.appendCalls != 1 ||
		len(store.appended.Messages) != 2 || !reflect.DeepEqual(store.appended.Invocations, []agent.ModelInvocation{invocation}) {
		t.Fatalf("result/append = %#v, %#v", result, store.appended)
	}
}

func TestRunnerForwardsAndClonesUsageAndInvocations(t *testing.T) {
	answer := ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("answer")},
		Usage:   &ai.Usage{PlatformID: "test", Model: "model"},
	}
	invocations := []agent.ModelInvocation{{
		Sequence: 1,
		Phase:    agent.ModelInvocationPhaseThinking,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}, {
		Sequence: 2,
		Phase:    agent.ModelInvocationPhaseAction,
		Usage:    ai.Usage{PlatformID: "test", Model: "model"},
	}}
	runtime := &runnerRuntimeFake{result: agent.RunResult{
		NewMessages: []ai.Message{answer},
		Invocations: invocations,
	}}
	store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 42, Version: 7}}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.appended.Invocations, invocations) {
		t.Fatalf("Invocations = %#v, want %#v", store.appended.Invocations, invocations)
	}

	runtime.result.NewMessages[0].Usage.PlatformID = "mutated"
	runtime.result.Invocations[0].Usage.PlatformID = "mutated"
	if store.appended.Messages[1].Usage.PlatformID != "test" ||
		store.appended.Invocations[0].Usage.PlatformID != "test" {
		t.Fatalf("stored request aliases runtime result: %#v", store.appended)
	}
}

func TestRunnerSkipsAppendWhenRuntimeFailsWithoutMessages(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 1}}
	runtime := &runnerRuntimeFake{err: runtimeErr}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) || store.appendCalls != 0 {
		t.Fatalf("Run() error/append calls = %v, %d", err, store.appendCalls)
	}
}

func TestRunnerStopsAfterLoadError(t *testing.T) {
	loadErr := errors.New("load failed")
	store := &runnerStoreFake{loadErr: loadErr}
	runtime := &runnerRuntimeFake{}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, loadErr) || runtime.calls != 0 || store.appendCalls != 0 {
		t.Fatalf("Run() error/runtime/append = %v, %d, %d", err, runtime.calls, store.appendCalls)
	}
}

func TestRunnerJoinsRuntimeAndAppendErrors(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	appendErr := errors.New("append failed")
	partial := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")}}
	store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 1}, appendErr: appendErr}
	runtime := &runnerRuntimeFake{
		result: agent.RunResult{NewMessages: []ai.Message{partial}},
		err:    runtimeErr,
	}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, runtimeErr) || !errors.Is(err, appendErr) {
		t.Fatalf("Run() error = %v, want both errors", err)
	}
}

func TestRunnerReturnsConflictWithoutRetry(t *testing.T) {
	store := &runnerStoreFake{
		snapshot:  Snapshot{ConversationPK: 1},
		appendErr: ErrConflict,
	}
	runtime := &runnerRuntimeFake{result: agent.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
	if !errors.Is(err, ErrConflict) || store.loadCalls != 1 || runtime.calls != 1 || store.appendCalls != 1 {
		t.Fatalf("Run() error/calls = %v, %d/%d/%d", err, store.loadCalls, runtime.calls, store.appendCalls)
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
			_, err := NewRunner(&runnerRuntimeFake{}, store, 100).Run(context.Background(), request, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) || store.loadCalls != 0 {
				t.Fatalf("Run() error/load calls = %v, %d", err, store.loadCalls)
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
		{name: "invalid history limit", ctx: context.Background(), runner: NewRunner(&runnerRuntimeFake{}, &runnerStoreFake{}, 0)},
		{name: "canceled context", ctx: canceled, runner: NewRunner(&runnerRuntimeFake{}, &runnerStoreFake{}, 100), want: context.Canceled},
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
		Role:      ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{ID: "call", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}},
	}
	input := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("input")}}
	answer := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}}
	contextBlocks := []agent.ContextBlock{{Name: "profile", Content: "gold"}}
	metadata := map[string]string{"tenant": "one"}
	store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 1, Messages: []ai.Message{history}}}
	runtime := &runnerRuntimeFake{result: agent.RunResult{NewMessages: []ai.Message{answer}}}
	request := RunRequest{
		UserID: " user ", ConversationID: " conversation ", RunID: "run",
		Input: input, Context: contextBlocks, Metadata: metadata,
	}

	_, err := NewRunner(runtime, store, 100).Run(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.snapshot.Messages[0].ToolCalls[0].Arguments[0] = 'x'
	request.Input.Content[0].Text = "changed"
	contextBlocks[0].Content = "changed"
	metadata["tenant"] = "changed"
	runtime.result.NewMessages[0].Content[0].Text = "changed"

	if got := string(runtime.request.History[0].ToolCalls[0].Arguments); got != `{"path":"a"}` {
		t.Fatalf("runtime history mutated: %q", got)
	}
	if runtime.request.Input.Content[0].Text != "input" || runtime.request.Context[0].Content != "gold" || runtime.request.Metadata["tenant"] != "one" {
		t.Fatalf("runtime request mutated: %#v", runtime.request)
	}
	if store.appended.Messages[0].Content[0].Text != "input" || store.appended.Messages[1].Content[0].Text != "answer" {
		t.Fatalf("append request mutated: %#v", store.appended)
	}
	if store.loadedKey != (Key{UserID: "user", ConversationID: "conversation"}) {
		t.Fatalf("loaded key = %#v", store.loadedKey)
	}
}

func validConversationRunRequest() RunRequest {
	return RunRequest{
		UserID: "user", ConversationID: "conversation", RunID: "run",
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("input")}},
	}
}

type runnerRuntimeFake struct {
	result  agent.RunResult
	err     error
	calls   int
	request agent.RunRequest
	run     func(agent.RunRequest)
}

func (f *runnerRuntimeFake) Run(_ context.Context, request agent.RunRequest, _ agent.Reporter) (agent.RunResult, error) {
	f.calls++
	f.request = request
	if f.run != nil {
		f.run(request)
	}
	return f.result, f.err
}

type runnerStoreFake struct {
	snapshot    Snapshot
	loadErr     error
	appendErr   error
	loadCalls   int
	appendCalls int
	loadedKey   Key
	loadedLimit int
	appended    AppendRequest
}

func (f *runnerStoreFake) LoadOrCreate(_ context.Context, key Key, limit int) (Snapshot, error) {
	f.loadCalls++
	f.loadedKey = key
	f.loadedLimit = limit
	return f.snapshot, f.loadErr
}

func (f *runnerStoreFake) AppendTurn(_ context.Context, request AppendRequest) error {
	f.appendCalls++
	f.appended = request
	return f.appendErr
}
