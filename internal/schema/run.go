package schema

import "github.com/PycMono/go-reagent/ai"

// ContextBlock is caller-provided context that the runtime injects before
// conversation history without interpreting its business meaning.
type ContextBlock struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority,omitempty"`
}

// RunRequest contains all caller-owned input required for one stateless run.
type RunRequest struct {
	RunID    string            `json:"run_id,omitempty"`
	History  []ai.Message      `json:"history,omitempty"`
	Input    ai.Message        `json:"input"`
	Context  []ContextBlock    `json:"context,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RunResult contains only messages created during the current run.
type RunResult struct {
	RunID       string       `json:"run_id,omitempty"`
	NewMessages []ai.Message `json:"new_messages,omitempty"`
}
