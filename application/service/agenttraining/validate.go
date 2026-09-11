package agenttraining

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	"github.com/PycMono/go-reagent/common/vo"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
)

func (s *Service) validateCandidate(ctx context.Context, item training.Session, id string) (bundle.BundleRef, agentversion.ValidationReport, error) {
	var report agentversion.ValidationReport
	ref, err := s.bundles.FreezeCandidate(ctx, candidate(item), id)
	if err != nil {
		return ref, report, err
	}
	snapshot, err := agentversion.ParseSnapshot(item.CandidateConfig)
	if err != nil {
		return ref, report, err
	}
	root, err := s.bundles.MaterializeValidation(ctx, item.TenantID, item.AgentID, id, ref)
	if err != nil {
		return ref, report, err
	}
	report, err = s.validator.Validate(ctx, item.TenantID, item.AgentID, ref, root, snapshot)
	if report.ValidUntil.After(item.ExpiresAt) {
		report.ValidUntil = item.ExpiresAt
	}
	return ref, report, err
}
func (s *Service) Validate(ctx context.Context, p identity.Principal, id string, in dto.TrainingMutationDTO) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	digest, err := requestDigest("validate", in)
	if err != nil {
		return sessionVO(item), err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RequestID)
	if err != nil {
		return sessionVO(item), err
	}
	defer release()
	item, replay, err := s.admit(ctx, item, in, "validate", digest)
	if err != nil || replay {
		return sessionVO(item), err
	}
	validateCtx, complete := s.trackOperation(ctx, item, release)
	defer complete()
	validationID := s.ids.NextID()
	ref, report, opErr := s.validateCandidate(validateCtx, item, validationID)
	if ref.Commit != "" {
		final, cancel := finalizeContext()
		opErr = errors.Join(opErr, s.bundles.CleanupPreparation(final, bundle.PreparationArtifacts{TenantID: item.TenantID, AgentID: item.AgentID, VersionID: validationID, Ref: ref}))
		cancel()
	}
	if opErr == nil {
		opErr = validateCtx.Err()
	}
	if opErr == nil && report.Passed {
		item.Validation, opErr = json.Marshal(report)
		item.Status = training.Ready
	} else {
		item.Status = training.Active
		item.Validation = nil
		if opErr == nil {
			opErr = errors.New("candidate validation did not pass")
		}
		message := opErr.Error()
		if len(message) > 2048 {
			message = message[:2048]
		}
		item.Operation.Result, _ = json.Marshal(struct {
			Error  string                        `json:"error"`
			Report agentversion.ValidationReport `json:"report"`
		}{message, report})
	}
	final, finishCancel := finalizeContext()
	defer finishCancel()
	item, err = s.finish(final, item, opErr)
	return sessionVO(item), errors.Join(opErr, err)
}
