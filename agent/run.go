package agent

import (
	"context"

	"github.com/PycMono/go-reagent/ai"
)

// RunRequest contains all caller-owned input required for one stateless run.
type RunRequest struct {
	RunID    string            `json:"run_id,omitempty"`
	History  []ai.Message      `json:"history,omitempty"`
	Input    ai.Message        `json:"input"`
	Context  []ContextBlock    `json:"context,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ContextBlock is caller-provided context that the runtime injects before
// conversation history without interpreting its business meaning.
type ContextBlock struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority,omitempty"`
}

// ModelInvocationPhase identifies why the Agent called the model.
type ModelInvocationPhase string

const (
	ModelInvocationPhaseThinking ModelInvocationPhase = "thinking"
	ModelInvocationPhaseAction   ModelInvocationPhase = "action"
)

// ModelInvocation records one successfully metered model call in run order.
type ModelInvocation struct {
	Sequence uint32               `json:"sequence"`
	Phase    ModelInvocationPhase `json:"phase"`
	Usage    ai.Usage             `json:"usage"`
}

// RunResult contains only messages created during the current run.
type RunResult struct {
	RunID       string            `json:"run_id,omitempty"`
	NewMessages []ai.Message      `json:"new_messages,omitempty"`
	Invocations []ModelInvocation `json:"invocations,omitempty"`
}

// RunContext is the prepared per-run message and tool snapshot.
type RunContext struct {
	Messages []ai.Message
	Tools    []ai.ToolDefinition
	Metadata map[string]string
}

// ContextFactory prepares product-specific context for one stateless Run.
type ContextFactory interface {
	Create(context.Context, RunRequest, []ai.ToolDefinition) (RunContext, error)
}

func cloneRequest(request RunRequest) RunRequest {
	request.History = cloneMessages(request.History)
	request.Input = cloneMessage(request.Input)
	request.Context = append([]ContextBlock(nil), request.Context...)
	request.Metadata = cloneMetadata(request.Metadata)
	return request
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
	message.Content = append([]ai.ContentBlock(nil), message.Content...)
	if message.ToolCalls != nil {
		calls := make([]ai.ToolCall, len(message.ToolCalls))
		for index, call := range message.ToolCalls {
			calls[index] = call
			calls[index].Arguments = append([]byte(nil), call.Arguments...)
		}
		message.ToolCalls = calls
	}
	return message
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
