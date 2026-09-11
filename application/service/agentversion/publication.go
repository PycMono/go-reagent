package agentversion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	repo "github.com/PycMono/go-reagent/domain/repository/agent"
)

type PublicationRepository interface {
	repo.Repository
	CommitModelRelease(context.Context, string, string, uint64, string, agent.Version, ValidationReport) error
	ActivateVersion(context.Context, string, string, string, uint64, ValidationReport) error
}
type ReleaseAdmission interface {
	Reserve(context.Context, string, string, string) (func(), error)
}
type VersionIDs interface{ NextID() string }
type PublicationService struct {
	repository PublicationRepository
	bundles    bundle.Store
	intents    bundle.IntentStore
	validator  *Validator
	capture    SnapshotSource
	admission  ReleaseAdmission
	ids        VersionIDs
}

func NewPublicationService(r PublicationRepository, b bundle.Store, i bundle.IntentStore, v *Validator, c SnapshotSource, a ReleaseAdmission, ids VersionIDs) (*PublicationService, error) {
	if r == nil || b == nil || i == nil || v == nil || c == nil || a == nil || ids == nil {
		return nil, errors.New("publication dependencies required")
	}
	return &PublicationService{r, b, i, v, c, a, ids}, nil
}
func SnapshotFromVersion(v agent.Version) (Snapshot, error) {
	raw := []byte(`{"schema_version":1,"model":` + string(v.ModelConfig) + `,"tools":` + string(v.ToolPolicy) + `,"runtime":` + string(v.RuntimeConfig) + `}`)
	s, err := ParseSnapshot(raw)
	if err != nil {
		return s, err
	}
	digest, err := SpecDigest(v.BundleDigest, s)
	if err != nil || digest != v.SpecDigest {
		return s, errors.New("saved version spec digest mismatch")
	}
	return s, nil
}
func (s *PublicationService) Activate(ctx context.Context, p identity.Principal, agentID, versionID string, expected uint64) (*vo.AgentVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	if !identity.ValidID(agentID) || !identity.ValidID(versionID) {
		return nil, commonerrors.ErrInvalidParam
	}
	release, err := s.admission.Reserve(ctx, p.TenantID, agentID, s.ids.NextID())
	if err != nil {
		return nil, err
	}
	defer release()
	a, err := s.repository.Find(ctx, p.TenantID, agentID)
	if err != nil {
		return nil, err
	}
	if a.RowVersion != expected || a.Status != "enabled" {
		return nil, commonerrors.ErrConflict
	}
	v, err := s.repository.FindVersion(ctx, p.TenantID, agentID, versionID)
	if err != nil {
		return nil, err
	}
	snapshot, err := SnapshotFromVersion(v)
	if err != nil {
		return nil, err
	}
	ref := bundle.BundleRef{Commit: v.BundleCommit, Tag: v.BundleTag, Digest: v.BundleDigest}
	workspace, err := s.bundles.MaterializeValidation(ctx, p.TenantID, agentID, s.ids.NextID(), ref)
	if err != nil {
		return nil, err
	}
	evidence, err := s.validator.Validate(ctx, p.TenantID, agentID, ref, workspace, snapshot)
	if err != nil {
		return nil, err
	}
	if err = s.repository.ActivateVersion(ctx, p.TenantID, agentID, versionID, expected, evidence); err != nil {
		return nil, err
	}
	a, err = s.repository.Find(ctx, p.TenantID, agentID)
	if err != nil {
		return nil, err
	}
	return &vo.AgentVO{ID: a.ID, Name: a.Name, Description: a.Description, Status: a.Status, Presentation: a.Presentation, Icon: a.Presentation.Icon, Welcome: a.Presentation.Welcome, Starters: a.Presentation.Starters, TemplateCode: a.TemplateCode, ActiveVersionID: a.ActiveVersionID, ActiveTrainingSessionID: a.ActiveTrainingSessionID, RowVersion: a.RowVersion, Selectable: a.Status == "enabled" && a.ActiveVersionID != nil}, nil
}
func (s *PublicationService) ReleaseModel(ctx context.Context, p identity.Principal, agentID string, in dto.ModelConfigReleaseDTO) (vo.AgentVersionVO, error) {
	var zero vo.AgentVersionVO
	if err := p.RequireAdmin(); err != nil {
		return zero, err
	}
	if !in.Confirmed || !identity.ValidID(agentID) || !identity.ValidID(in.BaseVersionID) || strings.TrimSpace(in.ChangeSummary) == "" || len(in.ChangeSummary) > 4096 || !utf8.ValidString(in.ChangeSummary) {
		return zero, commonerrors.ErrInvalidParam
	}
	if len(in.ModelConfig.Parameters) > 0 {
		var params map[string]json.RawMessage
		if DecodeStrict(in.ModelConfig.Parameters, &params) != nil || len(params) != 0 {
			return zero, commonerrors.ErrInvalidParam
		}
	}
	id := s.ids.NextID()
	release, err := s.admission.Reserve(ctx, p.TenantID, agentID, id)
	if err != nil {
		return zero, err
	}
	defer release()
	// Pending durable work must be reconciled before a new publication is admitted.
	if err = s.ReconcilePublications(ctx, p.TenantID, agentID); err != nil {
		return zero, err
	}
	a, err := s.repository.Find(ctx, p.TenantID, agentID)
	if err != nil {
		return zero, err
	}
	if a.Status != "enabled" || a.RowVersion != in.ExpectedRowVersion || a.ActiveVersionID == nil || *a.ActiveVersionID != in.BaseVersionID {
		return zero, commonerrors.ErrConflict
	}
	base, err := s.repository.FindVersion(ctx, p.TenantID, agentID, in.BaseVersionID)
	if err != nil {
		return zero, err
	}
	snapshot, err := SnapshotFromVersion(base)
	if err != nil {
		return zero, err
	}
	selected, err := s.capture.CaptureAgentSnapshot(in.ModelConfig.ProviderRef, in.ModelConfig.ModelID, []string{})
	if err != nil {
		return zero, err
	}
	oldModel, _ := canonicalMarshal(snapshot.Model)
	newModel, _ := canonicalMarshal(selected.Model)
	if bytes.Equal(oldModel, newModel) {
		return zero, commonerrors.ErrInvalidParam
	}
	snapshot.Model = selected.Model
	digest, err := SpecDigest(base.BundleDigest, snapshot)
	if err != nil {
		return zero, err
	}
	page, err := s.repository.ListVersions(ctx, p.TenantID, agentID, 0, 1)
	if err != nil {
		return zero, err
	}
	if len(page.Items) != 1 {
		return zero, commonerrors.ErrConflict
	}
	intent := bundle.PublicationIntent{TenantID: p.TenantID, AgentID: agentID, VersionID: id, BaseVersionID: base.ID, Tag: "versions/" + id, BundleDigest: base.BundleDigest, SpecDigest: digest, Phase: "reserved", Version: page.Items[0].Number + 1, ExpectedRowVersion: in.ExpectedRowVersion}
	if err = s.intents.Prepare(ctx, intent); err != nil {
		return zero, err
	}
	cloner, ok := s.bundles.(bundle.VersionCloner)
	if !ok {
		return zero, errors.New("version cloning unavailable")
	}
	ref, err := cloner.CloneVersion(ctx, p.TenantID, agentID, id, bundle.BundleRef{Commit: base.BundleCommit, Tag: base.BundleTag, Digest: base.BundleDigest})
	if err != nil {
		return zero, err
	}
	if _, err = s.bundles.MaterializeVersion(ctx, p.TenantID, agentID, id, ref); err != nil {
		return zero, err
	}
	workspace, err := s.bundles.MaterializeValidation(ctx, p.TenantID, agentID, id, ref)
	if err != nil {
		return zero, err
	}
	evidence, err := s.validator.Validate(ctx, p.TenantID, agentID, ref, workspace, snapshot)
	if err != nil {
		return zero, err
	}
	intent.Phase = "validated"
	if err = s.intents.Prepare(ctx, intent); err != nil {
		return zero, err
	}
	model, _ := json.Marshal(snapshot.Model)
	validation, _ := json.Marshal(evidence)
	v := agent.Version{ID: id, TenantID: p.TenantID, AgentID: agentID, Number: intent.Version, BundleCommit: ref.Commit, BundleTag: ref.Tag, BundleDigest: ref.Digest, SpecDigest: digest, ModelConfig: model, ToolPolicy: append(json.RawMessage{}, base.ToolPolicy...), RuntimeConfig: append(json.RawMessage{}, base.RuntimeConfig...), Validation: validation, PublishedBy: p.UserID, PublishedAt: evidence.ValidatedAt, ChangeSummary: strings.TrimSpace(in.ChangeSummary)}
	if err = s.repository.CommitModelRelease(ctx, p.TenantID, agentID, in.ExpectedRowVersion, base.ID, v, evidence); err != nil {
		return zero, err
	}
	_ = s.intents.Remove(ctx, intent)
	return vo.AgentVersionVO{ID: v.ID, Number: v.Number, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt, ChangeSummary: v.ChangeSummary, Active: true}, nil
}

// ReconcilePublications must run under shared mutation admission before new
// work on an Agent. Committed immutable rows are authoritative after lost replies.
func (s *PublicationService) ReconcilePublications(ctx context.Context, tenant, agentID string) error {
	intents, err := s.intents.List(ctx, tenant, agentID)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		v, findErr := s.repository.FindVersion(ctx, tenant, agentID, intent.VersionID)
		if findErr == nil {
			if v.BundleTag != intent.Tag || v.BundleDigest != intent.BundleDigest || v.SpecDigest != intent.SpecDigest || v.Number != intent.Version {
				return errors.New("committed publication intent mismatch")
			}
			if err = s.bundles.Verify(ctx, tenant, agentID, bundle.BundleRef{Commit: v.BundleCommit, Tag: v.BundleTag, Digest: v.BundleDigest}); err != nil {
				return err
			}
		} else {
			if !errors.Is(findErr, commonerrors.ErrNotFound) {
				return findErr
			}
			recoverer, ok := s.bundles.(interface {
				RecoverInitial(context.Context, string, string, string) (bundle.BundleRef, error)
			})
			if !ok {
				return errors.New("publication recovery unavailable")
			}
			ref, recoverErr := recoverer.RecoverInitial(ctx, tenant, agentID, intent.VersionID)
			if recoverErr != nil && !errors.Is(recoverErr, os.ErrNotExist) {
				return recoverErr
			}
			if recoverErr == nil {
				if ref.Tag != intent.Tag || ref.Digest != intent.BundleDigest {
					return errors.New("uncommitted publication intent mismatch")
				}
				cleaner, ok := s.bundles.(bundle.PreparationCleaner)
				if !ok {
					return errors.New("publication cleanup unavailable")
				}
				if err = cleaner.CleanupPreparation(ctx, bundle.PreparationArtifacts{TenantID: tenant, AgentID: agentID, VersionID: intent.VersionID, Ref: ref}); err != nil {
					return err
				}
			}
		}
		if err = s.intents.Remove(ctx, intent); err != nil {
			return err
		}
	}
	return nil
}
