package conversation

import (
	"context"
	"errors"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

var (
	ErrNotFound       = errors.New("conversation not found")
	ErrConflict       = errors.New("conversation version conflict")
	ErrCorruptMessage = errors.New("conversation message is corrupt")
)

type Key struct {
	UserID         string
	ConversationID string
}

type Snapshot struct {
	ConversationPK uint64
	Version        uint64
	Messages       []ai.Message
}

type AppendRequest struct {
	ConversationPK  uint64
	ExpectedVersion uint64
	RunID           string
	Messages        []ai.Message
}

type Store interface {
	LoadOrCreate(context.Context, Key, int) (Snapshot, error)
	AppendTurn(context.Context, AppendRequest) error
}

type RunRequest struct {
	UserID         string
	ConversationID string
	RunID          string
	Input          ai.Message
	Context        []schema.ContextBlock
	Metadata       map[string]string
}

type Runner interface {
	Run(context.Context, RunRequest, engine.Reporter) (schema.RunResult, error)
}
