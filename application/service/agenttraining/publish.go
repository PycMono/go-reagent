package agenttraining

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	agent "github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"strings"
	"unicode/utf8"
)

func versionVO(v agent.Version) vo.AgentVersionVO {
	return vo.AgentVersionVO{ID: v.ID, Number: v.Number, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt, ChangeSummary: v.ChangeSummary, SourceTrainingSessionID: v.SourceTrainingSessionID, Active: true}
}
func (s *Service) Publish(ctx context.Context, p identity.Principal, id string, in dto.TrainingPublishDTO) (vo.AgentVersionVO, error) {
	var zero vo.AgentVersionVO
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return zero, err
	}
	if item.Status == training.Published && item.ResultVersionID != nil {
		v, err := s.agents.FindVersion(ctx, p.TenantID, item.AgentID, *item.ResultVersionID)
		return versionVO(v), err
	}
	if !in.HumanConfirmed || strings.TrimSpace(in.ChangeSummary) == "" || len(in.ChangeSummary) > 4096 || !utf8.ValidString(in.ChangeSummary) {
		return zero, ce.ErrInvalidParam
	}
	digest, err := requestDigest("publish", in)
	if err != nil {
		return zero, err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RequestID)
	if err != nil {
		return zero, err
	}
	defer release()
	item, replay, err := s.admit(ctx, item, in.TrainingMutationDTO, "publish", digest)
	if err != nil {
		return zero, err
	}
	if replay {
		return zero, ce.ErrConflict
	}
	ctx, complete := s.trackOperation(ctx, item, release)
	defer complete()
	fail := func(cause error) (vo.AgentVersionVO, error) {
		final, cancel := finalizeContext()
		defer cancel()
		current, readErr := s.repository.Find(final, scope(item))
		if readErr != nil {
			return zero, errors.Join(cause, readErr)
		}
		if current.Operation.ID != in.RequestID {
			return zero, errors.Join(cause, ce.ErrConflict)
		}
		item = current
		if item.Operation.PreparedVersion != nil {
			committed, err := s.reconcilePreparation(final, item)
			if err != nil {
				return zero, errors.Join(cause, err)
			}
			if committed != nil {
				return versionVO(*committed), nil
			}
			item.Operation.PreparedVersion = nil
		}
		if !errors.Is(cause, agentruntime.ErrBusy) {
			item.Status = training.Active
			item.Validation = nil
		}
		_, finishErr := s.finish(final, item, cause)
		return zero, errors.Join(cause, finishErr)
	}
	// Ready must carry complete, known evidence tied to this immutable candidate.
	var prior agentversion.ValidationReport
	var fields map[string]json.RawMessage
	if err = agentversion.DecodeStrict(item.Validation, &prior); err != nil {
		return fail(err)
	}
	if err = json.Unmarshal(item.Validation, &fields); err != nil {
		return fail(err)
	}
	for _, key := range []string{"head", "digest_version", "bundle_digest", "spec_digest", "validator_version", "validation_policy_digest", "validated_at", "valid_until", "review_mode", "passed", "diagnostics"} {
		if _, ok := fields[key]; !ok {
			return fail(ce.ErrConflict)
		}
	}
	if prior.DigestVersion != 1 || prior.Head != item.CandidateHead || !prior.Passed || prior.ReviewMode != "manual" {
		return fail(ce.ErrConflict)
	}
	snapshot, err := agentversion.ParseSnapshot(item.CandidateConfig)
	if err != nil {
		return fail(err)
	}
	expectedSpec, err := agentversion.SpecDigest(prior.BundleDigest, snapshot)
	if err != nil || expectedSpec != prior.SpecDigest {
		return fail(ce.ErrConflict)
	}
	page, err := s.agents.ListVersions(ctx, p.TenantID, item.AgentID, 0, 1)
	if err != nil || len(page.Items) != 1 {
		if err == nil {
			err = ce.ErrConflict
		}
		return fail(err)
	}
	versionID := s.ids.NextID()
	item.Operation.PreparedVersion = &training.PreparedVersion{ID: versionID, Tag: "versions/" + versionID, Version: page.Items[0].Number + 1, BundleDigest: prior.BundleDigest, SpecDigest: prior.SpecDigest}
	err = s.repository.WithAgentTx(ctx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		if err := tx.SaveSession(item, item.RowVersion); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	item.RowVersion++
	publishCtx, cancel := context.WithDeadline(ctx, item.ExpiresAt)
	defer cancel()
	ref, evidence, err := s.validateCandidate(publishCtx, item, versionID)
	if err != nil {
		return fail(err)
	}
	if !evidence.Passed || evidence.BundleDigest != prior.BundleDigest || evidence.SpecDigest != prior.SpecDigest {
		return fail(ce.ErrConflict)
	}
	base, err := s.agents.FindVersion(ctx, p.TenantID, item.AgentID, item.BaseVersionID)
	if err != nil {
		return fail(err)
	}
	if base.BundleDigest == ref.Digest && base.SpecDigest == evidence.SpecDigest {
		return fail(ce.ErrInvalidParam)
	}
	if _, err = s.bundles.MaterializeVersion(publishCtx, item.TenantID, item.AgentID, versionID, ref); err != nil {
		return fail(err)
	}
	if _, err = s.bundles.FreezeCandidate(publishCtx, candidate(item), versionID); err != nil {
		return fail(err)
	}
	model, _ := json.Marshal(snapshot.Model)
	tools, _ := json.Marshal(snapshot.Tools)
	runtime, _ := json.Marshal(snapshot.Runtime)
	rawEvidence, _ := json.Marshal(evidence)
	v := agent.Version{ID: versionID, TenantID: item.TenantID, AgentID: item.AgentID, Number: item.Operation.PreparedVersion.Version, BundleCommit: ref.Commit, BundleTag: ref.Tag, BundleDigest: ref.Digest, SpecDigest: evidence.SpecDigest, ModelConfig: model, ToolPolicy: tools, RuntimeConfig: runtime, Validation: rawEvidence, SourceTrainingSessionID: &item.ID, PublishedBy: p.UserID, ChangeSummary: strings.TrimSpace(in.ChangeSummary)}
	err = s.repository.WithAgentTx(publishCtx, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		if current.RowVersion != item.RowVersion || current.Operation.ID != in.RequestID || current.Status != training.Ready || !current.Running() {
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
		if item.IsExpired(now) || !now.Before(evidence.ValidUntil) || a.ActiveVersionID == nil || *a.ActiveVersionID != item.BaseVersionID {
			return ce.ErrConflict
		}
		v.PublishedAt = now
		if err = tx.CommitPublication(v, a.RowVersion); err != nil {
			return err
		}
		item.Validation = rawEvidence
		item.Operation.State = training.OperationCompleted
		item.Operation.FinishedAt = &now
		if err = item.Publish(true, versionID); err != nil {
			return err
		}
		return tx.SaveSession(item, item.RowVersion)
	})
	if err != nil {
		return fail(err)
	}
	return versionVO(v), nil
}
