package agenttraining

import (
	"context"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentadmission"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"testing"
	"time"
)

type recoveryRepo struct {
	repo.Repository
	repo.Tx
	item  training.Session
	saved int
}

func (r *recoveryRepo) WithAgentTx(_ context.Context, _, _ string, fn func(repo.Tx) error) error {
	return fn(r)
}
func (r *recoveryRepo) SessionForUpdate(repo.Scope) (training.Session, error) { return r.item, nil }
func (r *recoveryRepo) NowUTC() (time.Time, error)                            { return time.Now().UTC(), nil }
func (r *recoveryRepo) AgentForUpdate() (agent.Agent, error) {
	base := r.item.BaseVersionID
	id := r.item.ID
	return agent.Agent{Status: "enabled", ActiveVersionID: &base, ActiveTrainingSessionID: &id}, nil
}
func (r *recoveryRepo) SaveSession(s training.Session, expected uint64) error {
	if expected != r.item.RowVersion {
		panic("stale recovery")
	}
	s.RowVersion++
	r.item = s
	r.saved++
	return nil
}

type recoveryBundle struct {
	BundleStore
	restored bundle.Candidate
}

func (b *recoveryBundle) RecoverCandidate(_ context.Context, c bundle.Candidate) error {
	b.restored = c
	return nil
}

type recoveryIDs struct{}

func (recoveryIDs) NextID() string { return "recovery" }

func TestStoppedRunRecoveryClearsEvidenceWithoutReplaying(t *testing.T) {
	r := &recoveryRepo{item: training.Session{ID: "training", TenantID: "tenant", AgentID: "agent", AdminUserID: "admin", BaseVersionID: "v1", CandidateHead: "persisted", Status: training.Active, ExpiresAt: time.Now().Add(time.Hour), Validation: []byte(`{}`), Operation: training.Operation{ID: "run", Kind: "run", State: training.OperationRunning}}}
	b := &recoveryBundle{}
	s := &Service{repository: r, bundles: b, admission: agentadmission.New(), ids: recoveryIDs{}}
	if err := s.RecoverStopped(context.Background(), r.item); err != nil {
		t.Fatal(err)
	}
	if r.saved != 1 || r.item.Operation.State != training.OperationInterrupted || r.item.Status != training.Active || len(r.item.Validation) != 0 || b.restored.Head != "persisted" {
		t.Fatalf("incorrect recovery: %+v %+v", r.item, b.restored)
	}
}
