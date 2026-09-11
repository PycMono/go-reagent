package agenttraining

import (
	"context"
	"github.com/PycMono/go-reagent/application/identity"
	agent "github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	"time"
)

type Scope struct{ TenantID, AgentID, TrainingID, AdminUserID string }
type Expected struct {
	RowVersion               uint64
	RequestID, RequestDigest string
}
type Repository interface {
	ActiveSession(context.Context, string, string) (training.Session, error)
	FindOwned(context.Context, identity.Principal, string) (training.Session, error)
	Find(context.Context, Scope) (training.Session, error)
	List(context.Context, identity.Principal, string, string, int) ([]training.Session, string, error)
	WithAgentTx(context.Context, string, string, func(Tx) error) error
	HasRun(context.Context, Scope, string) (bool, error)
	History(context.Context, Scope, uint64, int) ([]*conversation.Message, error)
}
type Tx interface {
	FindVersion(string) (agent.Version, error)
	CommitPublication(agent.Version, uint64) error
	NowUTC() (time.Time, error)
	AgentForUpdate() (agent.Agent, error)
	SessionForUpdate(Scope) (training.Session, error)
	CreateSession(training.Session, conversation.Conversation) error
	SaveSession(training.Session, uint64) error
	SetTrainingPointer(string, *string, uint64) error
	BeginTurn(Scope, string, conversation.Message) (uint64, error)
	FinishTurn(Scope, string, uint64, []*conversation.Message, []*conversation.ModelInvocation) error
}
