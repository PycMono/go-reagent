package agenttraining

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/config"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"github.com/PycMono/go-reagent/pi"
	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"sync"
	"time"
)

type BundleStore interface {
	bundle.Store
	bundle.CandidateStore
	FreezeCandidate(context.Context, bundle.Candidate, string) (bundle.BundleRef, error)
	CandidatePath(context.Context, bundle.Candidate) (string, error)
	RecoverCandidate(context.Context, bundle.Candidate) error
	RecoverInitial(context.Context, string, string, string) (bundle.BundleRef, error)
	bundle.PreparationCleaner
}

// Author must stop all tool scheduling and confirm all writers dead before returning.
type Author interface {
	Run(context.Context, training.Session, dto.TrainingRunDTO, []*conversation.Message, pi.EventListener) (pi.RunResult, error)
}
type Admission interface {
	Reserve(context.Context, string, string, string) (func(), error)
}
type IDSource interface{ NextID() string }
type Validation interface {
	Validate(context.Context, string, string, bundle.BundleRef, string, agentversion.Snapshot) (agentversion.ValidationReport, error)
}
type liveOperation struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Service struct {
	repository repo.Repository
	agents     agentrepo.Repository
	bundles    BundleStore
	validator  Validation
	author     Author
	admission  Admission
	cfg        config.AgentTrainingConfig
	ids        IDSource
	capture    agentversion.SnapshotSource
	mu         sync.Mutex
	live       map[string]liveOperation
}

func NewService(r repo.Repository, a agentrepo.Repository, b BundleStore, v Validation, author Author, admission Admission, cfg config.AgentTrainingConfig, ids IDSource, capture agentversion.SnapshotSource) (*Service, error) {
	if r == nil || a == nil || b == nil || v == nil || author == nil || admission == nil || ids == nil || capture == nil || cfg.SessionTTLSeconds <= 0 {
		return nil, errors.New("training dependencies required")
	}
	return &Service{repository: r, agents: a, bundles: b, validator: v, author: author, admission: admission, cfg: cfg, ids: ids, capture: capture, live: map[string]liveOperation{}}, nil
}
func scope(s training.Session) repo.Scope {
	return repo.Scope{TenantID: s.TenantID, AgentID: s.AgentID, TrainingID: s.ID, AdminUserID: s.AdminUserID}
}
func candidate(s training.Session) bundle.Candidate {
	return bundle.Candidate{TenantID: s.TenantID, AgentID: s.AgentID, TrainingID: s.ID, Head: s.CandidateHead}
}
func sessionVO(s training.Session) vo.TrainingSessionVO {
	return vo.TrainingSessionVO{ID: s.ID, AgentID: s.AgentID, ConversationID: s.ConversationID, BaseVersionID: s.BaseVersionID, CandidateHead: s.CandidateHead, CandidatePartial: s.CandidatePartial, Status: s.Status, RowVersion: s.RowVersion, ExpiresAt: s.ExpiresAt, Validation: s.Validation, Operation: s.Operation, ResultVersionID: s.ResultVersionID}
}
func requestDigest(kind string, input any) (string, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	canonical, err := jsoncanonicalizer.Transform(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(kind+"\x00"), canonical...))
	return fmt.Sprintf("sha256:%x", sum), nil
}
func (s *Service) owned(ctx context.Context, p identity.Principal, id string) (training.Session, error) {
	if err := p.RequireAdmin(); err != nil {
		return training.Session{}, err
	}
	if !identity.ValidID(id) {
		return training.Session{}, ce.ErrInvalidParam
	}
	item, err := s.repository.FindOwned(ctx, p, id)
	if err != nil {
		return item, err
	}
	return s.reconcile(ctx, item)
}
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	return sessionVO(item), err
}
func (s *Service) List(ctx context.Context, p identity.Principal, agentID, cursor string, limit int) ([]vo.TrainingSessionVO, string, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, "", err
	}
	items, next, err := s.repository.List(ctx, p, agentID, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]vo.TrainingSessionVO, len(items))
	for i, item := range items {
		out[i] = sessionVO(item)
	}
	return out, next, nil
}
func (s *Service) Messages(ctx context.Context, p identity.Principal, id string, before uint64, limit int) ([]*conversation.Message, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return s.repository.History(ctx, scope(item), before, limit)
}
func (s *Service) Checkpoints(ctx context.Context, p identity.Principal, id, cursor string, limit int) ([]bundle.Checkpoint, string, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return nil, "", err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, s.ids.NextID())
	if err != nil {
		return nil, "", err
	}
	defer release()
	if item.Running() {
		return nil, "", ce.ErrConflict
	}
	return s.bundles.ListCheckpoints(ctx, candidate(item), cursor, limit)
}
func (s *Service) Diff(ctx context.Context, p identity.Principal, id string, maxBytes int) (bundle.Diff, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return bundle.Diff{}, err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, s.ids.NextID())
	if err != nil {
		return bundle.Diff{}, err
	}
	defer release()
	if item.Running() {
		return bundle.Diff{}, ce.ErrConflict
	}
	base, err := s.agents.FindVersion(ctx, item.TenantID, item.AgentID, item.BaseVersionID)
	if err != nil {
		return bundle.Diff{}, err
	}
	return s.bundles.Diff(ctx, candidate(item), base.BundleCommit, maxBytes)
}
func (s *Service) finish(ctx context.Context, item training.Session, opErr error) (training.Session, error) {
	err := s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		if current.RowVersion != item.RowVersion || current.Operation.ID != item.Operation.ID || !current.Running() {
			return ce.ErrConflict
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		item.Operation.FinishedAt = &now
		if opErr == nil {
			item.Operation.State = training.OperationCompleted
		} else {
			item.Operation.State = training.OperationFailed
			if len(item.Operation.Result) == 0 {
				item.Operation.Result = json.RawMessage(`{"error":"training operation failed"}`)
			}
		}
		if item.IsExpired(now) {
			item.Operation.State = training.OperationInterrupted
			item.Status = training.Expired
			item.Validation = nil
		}
		if item.Terminal() {
			a, err := tx.AgentForUpdate()
			if err != nil {
				return err
			}
			if err = tx.SetTrainingPointer(item.ID, nil, a.RowVersion); err != nil {
				return err
			}
		}
		if err = tx.SaveSession(item, item.RowVersion); err != nil {
			return err
		}
		item.RowVersion++
		return nil
	})
	return item, err
}
func finalizeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *Service) trackOperation(ctx context.Context, item training.Session, release func()) (context.Context, func()) {
	ctx, cancel := context.WithDeadline(ctx, item.ExpiresAt)
	done := make(chan struct{})
	s.mu.Lock()
	s.live[item.ID] = liveOperation{cancel, done}
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.live, item.ID)
		s.mu.Unlock()
		release()
		close(done)
	}
}
