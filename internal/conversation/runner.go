package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

type runner struct {
	runtime      engine.AgentRuntime
	store        Store
	historyLimit int
}

func NewRunner(runtime engine.AgentRuntime, store Store, historyLimit int) Runner {
	return &runner{runtime: runtime, store: store, historyLimit: historyLimit}
}

func (r *runner) Run(ctx context.Context, request RunRequest, reporter engine.Reporter) (schema.RunResult, error) {
	result := schema.RunResult{RunID: request.RunID}
	if ctx == nil {
		return result, errors.New("conversation runner: context is required")
	}
	if r == nil || r.runtime == nil || r.store == nil {
		return result, errors.New("conversation runner: runtime and store are required")
	}
	if r.historyLimit < 1 {
		return result, errors.New("conversation runner: history limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("conversation runner: run canceled: %w", err)
	}
	key, err := validateRunRequest(request)
	if err != nil {
		return result, err
	}

	snapshot, err := r.store.LoadOrCreate(ctx, key, r.historyLimit)
	if err != nil {
		return result, err
	}
	runtimeResult, runErr := r.runtime.Run(ctx, schema.RunRequest{
		RunID:    request.RunID,
		History:  cloneMessages(snapshot.Messages),
		Input:    cloneMessage(request.Input),
		Context:  append([]schema.ContextBlock(nil), request.Context...),
		Metadata: cloneMetadata(request.Metadata),
	}, reporter)
	if runErr != nil && len(runtimeResult.NewMessages) == 0 {
		return runtimeResult, runErr
	}

	messages := make([]ai.Message, 0, 1+len(runtimeResult.NewMessages))
	messages = append(messages, cloneMessage(request.Input))
	messages = append(messages, cloneMessages(runtimeResult.NewMessages)...)
	persistErr := r.store.AppendTurn(ctx, AppendRequest{
		ConversationPK:  snapshot.ConversationPK,
		ExpectedVersion: snapshot.Version,
		RunID:           request.RunID,
		Messages:        messages,
	})
	return runtimeResult, errors.Join(runErr, persistErr)
}

func validateRunRequest(request RunRequest) (Key, error) {
	key := Key{
		UserID:         strings.TrimSpace(request.UserID),
		ConversationID: strings.TrimSpace(request.ConversationID),
	}
	switch {
	case key.UserID == "":
		return Key{}, errors.New("conversation runner: user ID is required")
	case key.ConversationID == "":
		return Key{}, errors.New("conversation runner: conversation ID is required")
	case request.Input.Role != ai.RoleUser:
		return Key{}, fmt.Errorf("conversation runner: input role must be user, got %q", request.Input.Role)
	}
	inputText, err := ai.TextContent(request.Input.Content)
	if err != nil {
		return Key{}, fmt.Errorf("conversation runner: input content: %w", err)
	}
	if strings.TrimSpace(inputText) == "" {
		return Key{}, errors.New("conversation runner: input content must not be empty")
	}
	if len(request.Input.ToolCalls) != 0 || request.Input.ToolCallID != "" ||
		request.Input.ToolName != "" || request.Input.IsError {
		return Key{}, errors.New("conversation runner: input must not contain tool fields")
	}
	return key, nil
}

func cloneMessages(messages []ai.Message) []ai.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]ai.Message, len(messages))
	for index := range messages {
		cloned[index] = cloneMessage(messages[index])
	}
	return cloned
}

func cloneMessage(message ai.Message) ai.Message {
	cloned := message
	cloned.Content = append([]ai.ContentBlock(nil), message.Content...)
	if message.ToolCalls != nil {
		cloned.ToolCalls = make([]ai.ToolCall, len(message.ToolCalls))
		for index, call := range message.ToolCalls {
			cloned.ToolCalls[index] = call
			cloned.ToolCalls[index].Arguments = append([]byte(nil), call.Arguments...)
		}
	}
	return cloned
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
