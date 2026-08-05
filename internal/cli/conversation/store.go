package conversation

import (
	"context"
	"errors"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
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
	Invocations     []agent.ModelInvocation
}

type Store interface {
	LoadOrCreate(context.Context, Key, int) (Snapshot, error)
	AppendTurn(context.Context, AppendRequest) error
}

type RunRequest struct {
	UserID         string               // 用户ID
	ConversationID string               // 会话ID
	RunID          string               // 运行ID
	Input          ai.Message           // 用户输入
	Context        []agent.ContextBlock //
	Metadata       map[string]string
}

type Runner interface {
	Run(context.Context, RunRequest, agent.Reporter) (agent.RunResult, error)
}
