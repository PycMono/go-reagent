package agenttraining

import (
	"context"
	"errors"
	"fmt"
	"os"

	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
)

func (s *Service) reconcilePreparation(ctx context.Context, item training.Session) (*agent.Version, error) {
	p := item.Operation.PreparedVersion
	if p == nil {
		return nil, nil
	}
	v, err := s.agents.FindVersion(ctx, item.TenantID, item.AgentID, p.ID)
	if err == nil {
		if v.SourceTrainingSessionID == nil || *v.SourceTrainingSessionID != item.ID || v.BundleCommit != item.CandidateHead || v.BundleTag != p.Tag || v.BundleDigest != p.BundleDigest || v.SpecDigest != p.SpecDigest {
			return nil, ce.ErrConflict
		}
		return &v, nil
	}
	if !errors.Is(err, ce.ErrNotFound) {
		return nil, err
	}
	ref, err := s.bundles.RecoverInitial(ctx, item.TenantID, item.AgentID, p.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && (ref.Commit != item.CandidateHead || ref.Tag != p.Tag || ref.Digest != p.BundleDigest) {
		return nil, ce.ErrConflict
	}
	if errors.Is(err, os.ErrNotExist) {
		ref = bundle.BundleRef{}
	}
	if err := s.bundles.CleanupPreparation(ctx, bundle.PreparationArtifacts{TenantID: item.TenantID, AgentID: item.AgentID, VersionID: p.ID, Ref: ref}); err != nil {
		return nil, fmt.Errorf("publication recovery: %w", err)
	}
	return nil, nil
}

// RecoverStopped is called before accepting requests, under the data-directory
// lock and an operator confirmation that the old process group has stopped.
func (s *Service) RecoverStopped(ctx context.Context, item training.Session) error {
	if item.Terminal() {
		return nil
	}
	if item.Running() {
		if item.Operation.PreparedVersion != nil {
			v, err := s.reconcilePreparation(ctx, item)
			if err != nil {
				return err
			}
			if v != nil {
				return s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
					current, err := tx.SessionForUpdate(scope(item))
					if err != nil {
						return err
					}
					if current.RowVersion != item.RowVersion {
						return ce.ErrConflict
					}
					now, err := tx.NowUTC()
					if err != nil {
						return err
					}
					item.Status = training.Published
					item.ResultVersionID = &v.ID
					item.Validation = v.Validation
					item.Operation.State = training.OperationCompleted
					item.Operation.FinishedAt = &now
					a, err := tx.AgentForUpdate()
					if err != nil {
						return err
					}
					if err := tx.SetTrainingPointer(item.ID, nil, a.RowVersion); err != nil {
						return err
					}
					return tx.SaveSession(item, item.RowVersion)
				})
			}
			item.Operation.PreparedVersion = nil
		}
		if err := s.bundles.RecoverCandidate(ctx, candidate(item)); err != nil {
			return err
		}
		if err := s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
			current, err := tx.SessionForUpdate(scope(item))
			if err != nil {
				return err
			}
			if current.RowVersion != item.RowVersion {
				return ce.ErrConflict
			}
			now, err := tx.NowUTC()
			if err != nil {
				return err
			}
			item.Operation.State = training.OperationInterrupted
			item.Operation.FinishedAt = &now
			item.Status = training.Active
			item.Validation = nil
			return tx.SaveSession(item, item.RowVersion)
		}); err != nil {
			return err
		}
		item.RowVersion++
	}
	_, err := s.reconcile(ctx, item)
	return err
}
