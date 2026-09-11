package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	agententity "github.com/PycMono/go-reagent/domain/entity/agent"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

type boundAgentRepo struct {
	agentrepo.Repository
	agent agententity.Agent
}

func (r *boundAgentRepo) Find(_ context.Context, tenant, id string) (agententity.Agent, error) {
	if r.agent.TenantID != tenant || r.agent.ID != id {
		return agententity.Agent{}, commonerrors.ErrNotFound
	}
	return r.agent, nil
}

type boundRepo struct {
	conversationrepo.IConversationManagementRepository
	record  *conversationentity.Conversation
	creates int
}

func (r *boundRepo) CreateBound(_ context.Context, c *conversationentity.Conversation) error {
	r.record = c
	r.creates++
	return nil
}
func (*boundRepo) CommitAgentVersion(context.Context, string, string, string, string) error {
	return nil
}

type boundIDs struct{ n int }

func (i *boundIDs) NextID() string { i.n++; return string(rune('a' + i.n)) }
func TestBoundConversationRequiresExplicitAgentAndTrustedOwner(t *testing.T) {
	version := "v"
	agents := &boundAgentRepo{agent: agententity.Agent{ID: "a", TenantID: "t", Status: "enabled", ActiveVersionID: &version}}
	repo := &boundRepo{}
	s := &Service{repository: repo, ids: &boundIDs{}, platform: &Platform{Agents: agents, Bindings: repo}}
	p := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleUser}
	ctx := identity.WithPrincipal(context.Background(), p)
	if _, err := s.CreateConversation(ctx, "u", dto.CreateConversationDTO{}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("missing agent: %v", err)
	}
	if _, err := s.CreateConversation(ctx, "other", dto.CreateConversationDTO{AgentID: "a"}); !errors.Is(err, commonerrors.ErrUnauthorized) {
		t.Fatalf("untrusted owner: %v", err)
	}
	if _, err := s.CreateConversation(ctx, "u", dto.CreateConversationDTO{AgentID: "missing"}); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("fallback Agent: %v", err)
	}
	if repo.creates != 0 {
		t.Fatal("created invalid conversation")
	}
	got, err := s.CreateConversation(ctx, "u", dto.CreateConversationDTO{AgentID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentID != "a" || repo.record.TenantID != "t" || repo.record.UserID != "u" || repo.record.AgentVersionID != "v" || !repo.record.FollowLatest || repo.record.ConversationType != "chat" {
		t.Fatalf("unbound conversation %+v", repo.record)
	}
}

type deletingRepo struct {
	conversationrepo.IConversationManagementRepository
	entered, proceed chan struct{}
}

func (r *deletingRepo) Delete(context.Context, string, string) error {
	close(r.entered)
	<-r.proceed
	return nil
}
func (*deletingRepo) FindByUserIDAndConversationID(context.Context, string, string) (*conversationentity.Conversation, bool, error) {
	return nil, false, nil
}
func TestDeleteBlocksNewRunUntilPersistenceFinishes(t *testing.T) {
	r := &deletingRepo{entered: make(chan struct{}), proceed: make(chan struct{})}
	s := &Service{repository: r, ids: &boundIDs{}, platform: &Platform{}, active: make(map[string]*activeRunEntry)}
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleUser})
	done := make(chan error, 1)
	go func() { done <- s.DeleteConversation(ctx, "u", "c") }()
	<-r.entered
	_, err := s.StartRun(ctx, "u", "c", dto.StartRunDTO{Content: "hello"})
	close(r.proceed)
	if deleteErr := <-done; deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("run admitted during deletion: %v", err)
	}
}
