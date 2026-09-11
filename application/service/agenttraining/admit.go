package agenttraining

import (
	"context"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
)

func (s *Service) admit(ctx context.Context, item training.Session, in dto.TrainingMutationDTO, kind, digest string, after ...func(repo.Tx, training.Session) error) (training.Session, bool, error) {
	if !identity.ValidID(in.RequestID) {
		return item, false, ce.ErrInvalidParam
	}
	replay := false
	err := s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		item = current
		if current.Operation.ID == in.RequestID {
			if current.Operation.Kind != kind || current.Operation.RequestDigest != digest {
				return ce.ErrConflict
			}
			replay = true
			return nil
		}
		if item.Terminal() || item.Running() || item.RowVersion != in.ExpectedRowVersion {
			return ce.ErrConflict
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		if item.IsExpired(now) {
			return ce.ErrConflict
		}
		a, err := tx.AgentForUpdate()
		if err != nil {
			return err
		}
		if a.Status != "enabled" || a.ActiveVersionID == nil || *a.ActiveVersionID != item.BaseVersionID || a.ActiveTrainingSessionID == nil || *a.ActiveTrainingSessionID != item.ID {
			return ce.ErrConflict
		}
		if kind == "publish" {
			if item.Status != training.Ready || item.CandidatePartial {
				return ce.ErrConflict
			}
		} else {
			if kind == "validate" && item.CandidatePartial {
				return ce.ErrConflict
			}
			item.Validation = nil
			if kind == "validate" {
				item.Status = training.Validating
			} else {
				item.Status = training.Active
			}
		}
		item.Operation = training.Operation{ID: in.RequestID, Kind: kind, RequestDigest: digest, State: training.OperationRunning, StartedAt: now}
		if err = tx.SaveSession(item, item.RowVersion); err != nil {
			return err
		}
		item.RowVersion++
		for _, fn := range after {
			if err := fn(tx, item); err != nil {
				return err
			}
		}
		return nil
	})
	return item, replay, err
}
