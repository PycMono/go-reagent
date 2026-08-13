package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type runner struct {
	runtime      agent.Runner
	store        conversationrepo.Store
	historyLimit int
}

func NewRunner(runtime agent.Runner, store conversationrepo.Store, historyLimit int) Runner {
	return &runner{runtime: runtime, store: store, historyLimit: historyLimit}
}

func (r *runner) Run(ctx context.Context, request RunRequest, reporter agent.Reporter) (agent.RunResult, error) {
	result := agent.RunResult{RunID: request.RunID}
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
	runtimeResult, runErr := r.runtime.Run(ctx, agent.RunRequest{
		RunID:    request.RunID,
		History:  cloneMessages(snapshot.Messages),
		Input:    cloneMessage(request.Input),
		Context:  append([]agent.ContextBlock(nil), request.Context...),
		Metadata: cloneMetadata(request.Metadata),
	}, reporter)
	if runErr != nil && len(runtimeResult.NewMessages) == 0 {
		return runtimeResult, runErr
	}

	messages := make([]ai.Message, 0, 1+len(runtimeResult.NewMessages))
	messages = append(messages, cloneMessage(request.Input))
	messages = append(messages, cloneMessages(runtimeResult.NewMessages)...)
	persistErr := r.store.AppendTurn(ctx, conversationentity.AppendRequest{
		ConversationPK:  snapshot.ConversationPK,
		ExpectedVersion: snapshot.Version,
		RunID:           request.RunID,
		Messages:        messages,
		Invocations:     cloneInvocations(runtimeResult.Invocations),
	})
	return runtimeResult, errors.Join(runErr, persistErr)
}

func validateRunRequest(request RunRequest) (conversationentity.Key, error) {
	key := conversationentity.Key{
		UserID:         strings.TrimSpace(request.UserID),
		ConversationID: strings.TrimSpace(request.ConversationID),
	}
	switch {
	case key.UserID == "":
		return conversationentity.Key{}, errors.New("conversation runner: user ID is required")
	case key.ConversationID == "":
		return conversationentity.Key{}, errors.New("conversation runner: conversation ID is required")
	case request.Input.Role != ai.RoleUser:
		return conversationentity.Key{}, fmt.Errorf("conversation runner: input role must be user, got %q", request.Input.Role)
	}
	inputText, err := ai.TextContent(request.Input.Content)
	if err != nil {
		return conversationentity.Key{}, fmt.Errorf("conversation runner: input content: %w", err)
	}
	if strings.TrimSpace(inputText) == "" {
		return conversationentity.Key{}, errors.New("conversation runner: input content must not be empty")
	}
	if len(request.Input.ToolCalls) != 0 || request.Input.ToolCallID != "" ||
		request.Input.ToolName != "" || request.Input.IsError {
		return conversationentity.Key{}, errors.New("conversation runner: input must not contain tool fields")
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
	if message.Usage != nil {
		usage := *message.Usage
		cloned.Usage = &usage
	}
	if message.ToolCalls != nil {
		cloned.ToolCalls = make([]ai.ToolCall, len(message.ToolCalls))
		for index, call := range message.ToolCalls {
			cloned.ToolCalls[index] = call
			cloned.ToolCalls[index].Arguments = append([]byte(nil), call.Arguments...)
		}
	}
	return cloned
}

func cloneInvocations(invocations []agent.ModelInvocation) []agent.ModelInvocation {
	return append([]agent.ModelInvocation(nil), invocations...)
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
