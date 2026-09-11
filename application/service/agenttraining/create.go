package agenttraining

import (
	"context"
	"encoding/json"
	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"time"
)

func (s *Service) Create(ctx context.Context, p identity.Principal, agentID string, in dto.CreateTrainingDTO) (vo.TrainingSessionVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return vo.TrainingSessionVO{}, err
	}
	if !identity.ValidID(agentID) {
		return vo.TrainingSessionVO{}, ce.ErrInvalidParam
	}
	if err := s.ReconcileAgent(ctx, p, agentID); err != nil {
		return vo.TrainingSessionVO{}, err
	}
	id := s.ids.NextID()
	release, err := s.admission.Reserve(ctx, p.TenantID, agentID, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	defer release()
	var item training.Session
	var base bundle.BundleRef
	err = s.repository.WithAgentTx(ctx, p.TenantID, agentID, func(tx repo.Tx) error {
		a, err := tx.AgentForUpdate()
		if err != nil {
			return err
		}
		if a.RowVersion != in.ExpectedRowVersion || a.Status != "enabled" || a.ActiveVersionID == nil || a.ActiveTrainingSessionID != nil {
			return ce.ErrConflict
		}
		version, err := tx.FindVersion(*a.ActiveVersionID)
		if err != nil {
			return err
		}
		snapshot, err := agentversion.SnapshotFromVersion(version)
		if err != nil {
			return err
		}
		config, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		digest, _ := requestDigest("create", in)
		item = training.Session{ID: id, TenantID: p.TenantID, AgentID: agentID, AdminUserID: p.UserID, ConversationID: s.ids.NextID(), BaseVersionID: version.ID, CandidateHead: version.BundleCommit, CandidateConfig: config, Status: training.Active, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Duration(s.cfg.SessionTTLSeconds) * time.Second), Operation: training.Operation{ID: id, Kind: "initialize", RequestDigest: digest, State: training.OperationRunning, StartedAt: now}}
		c := conversation.Conversation{ID: item.ConversationID, TenantID: p.TenantID, UserID: p.UserID, ConversationID: item.ConversationID, ConversationType: "training", AgentID: agentID, AgentVersionID: version.ID, FollowLatest: false, Name: "Agent training", CreatedAt: now, UpdatedAt: now}
		if err = tx.CreateSession(item, c); err != nil {
			return err
		}
		if err = tx.SetTrainingPointer("", &id, a.RowVersion); err != nil {
			return err
		}
		base = bundle.BundleRef{Commit: version.BundleCommit, Tag: version.BundleTag, Digest: version.BundleDigest}
		return nil
	})
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	_, opErr := s.bundles.CreateCandidate(ctx, p.TenantID, agentID, id, base)
	if opErr != nil {
		item.Status = training.Cancelled
	}
	final, cancel := finalizeContext()
	defer cancel()
	item, err = s.finish(final, item, opErr)
	if err != nil {
		return sessionVO(item), err
	}
	return sessionVO(item), opErr
}
