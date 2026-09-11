package agenttraining

import (
	"context"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
)

func (s *Service) ReconcileAgent(ctx context.Context, p identity.Principal, agentID string) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}
	if !identity.ValidID(agentID) {
		return ce.ErrInvalidParam
	}
	item, err := s.repository.ActiveSession(ctx, p.TenantID, agentID)
	if errors.Is(err, ce.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.reconcile(ctx, item)
	return err
}

func (s *Service) stop(ctx context.Context, id string) error {
	s.mu.Lock()
	live, ok := s.live[id]
	s.mu.Unlock()
	if !ok {
		return ce.ErrConflict
	}
	live.cancel()
	select {
	case <-live.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) reconcile(ctx context.Context, item training.Session) (training.Session, error) {
	if item.Terminal() {
		return item, nil
	}
	var expired, stale bool
	err := s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		item = current
		if item.Terminal() {
			return nil
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		a, err := tx.AgentForUpdate()
		if err != nil {
			return err
		}
		expired = item.IsExpired(now)
		stale = a.Status != "enabled" || a.ActiveVersionID == nil || *a.ActiveVersionID != item.BaseVersionID
		return nil
	})
	if err != nil || (!expired && !stale) {
		return item, err
	}
	if item.Running() {
		if err = s.stop(ctx, item.ID); err != nil {
			return item, err
		}
		item, err = s.repository.Find(ctx, scope(item))
		if err != nil || item.Terminal() {
			return item, err
		}
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, s.ids.NextID())
	if err != nil {
		return item, err
	}
	defer release()
	err = s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		item = current
		if item.Terminal() {
			return nil
		}
		if item.Running() {
			return ce.ErrConflict
		}
		a, err := tx.AgentForUpdate()
		if err != nil {
			return err
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		target := training.Stale
		if item.IsExpired(now) {
			target = training.Expired
		} else if a.Status == "enabled" && a.ActiveVersionID != nil && *a.ActiveVersionID == item.BaseVersionID {
			return nil
		}
		if err = item.Transition(target); err != nil {
			return err
		}
		if err = tx.SaveSession(item, item.RowVersion); err != nil {
			return err
		}
		item.RowVersion++
		return tx.SetTrainingPointer(item.ID, nil, a.RowVersion)
	})
	return item, err
}
func (s *Service) Cancel(ctx context.Context, p identity.Principal, id string, in dto.TrainingMutationDTO) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	if item.Terminal() {
		return sessionVO(item), nil
	}
	if !identity.ValidID(in.RequestID) {
		return sessionVO(item), ce.ErrInvalidParam
	}
	if item.RowVersion != in.ExpectedRowVersion {
		return sessionVO(item), ce.ErrConflict
	}
	if item.Running() {
		if err = s.stop(ctx, item.ID); err != nil {
			return sessionVO(item), err
		}
		item, err = s.repository.Find(ctx, scope(item))
		if err != nil {
			return sessionVO(item), err
		}
		if item.Terminal() {
			return sessionVO(item), nil
		}
		in.ExpectedRowVersion = item.RowVersion
	}
	digest, err := requestDigest("cancel", in)
	if err != nil {
		return sessionVO(item), err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RequestID)
	if err != nil {
		return sessionVO(item), err
	}
	defer release()
	item, replay, err := s.admit(ctx, item, in, "cancel", digest)
	if err != nil || replay {
		return sessionVO(item), err
	}
	item.Status = training.Cancelled
	item.Validation = nil
	final, cancel := finalizeContext()
	defer cancel()
	item, err = s.finish(final, item, nil)
	return sessionVO(item), err
}
