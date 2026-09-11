package agentversion

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	state "github.com/PycMono/go-reagent/application/port/agentstate"
	"github.com/PycMono/go-reagent/application/port/agenttemplate"
	"github.com/PycMono/go-reagent/common/dto"
	"github.com/PycMono/go-reagent/domain/entity/agent"
)

type SnapshotSource interface {
	CaptureAgentSnapshot(string, string, []string) (Snapshot, error)
}
type InitialBuilder struct {
	mu        sync.Mutex
	bundles   bundle.Store
	journal   state.Journal
	capture   SnapshotSource
	templates agenttemplate.Assembler
	validator *Validator
	tools     []string
}

func NewInitialBuilder(b bundle.Store, j state.Journal, c SnapshotSource, t agenttemplate.Assembler, v *Validator, tools []string) (*InitialBuilder, error) {
	if b == nil || j == nil || c == nil || t == nil || v == nil {
		return nil, errors.New("initial builder dependencies required")
	}
	return &InitialBuilder{bundles: b, journal: j, capture: c, templates: t, validator: v, tools: append([]string{}, tools...)}, nil
}

// PrepareInitial never publishes. The catalog must commit the returned version
// atomically with the draft row CAS, then call CompleteInitial.
func (b *InitialBuilder) PrepareInitial(ctx context.Context, p identity.Principal, a agent.Agent, id string, model dto.ModelChoice) (agent.Version, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var zero agent.Version
	if err := p.RequireAdmin(); err != nil {
		return zero, err
	}
	if p.TenantID != a.TenantID || !identity.ValidID(a.ID) || !identity.ValidID(id) || a.ActiveVersionID != nil {
		return zero, errors.New("invalid initial version scope")
	}
	if len(model.Parameters) > 0 && string(model.Parameters) != "{}" {
		return zero, errors.New("unsupported model parameters")
	}
	records, err := b.journal.List(ctx)
	if err != nil {
		return zero, err
	}
	recoveredID := ""
	for _, pending := range records {
		if pending.TenantID == a.TenantID && pending.AgentID == a.ID {
			if pending.ExpectedRowVersion != a.RowVersion {
				return zero, errors.New("stale initial preparation requires reconciliation")
			}
			if recoveredID != "" {
				return zero, errors.New("ambiguous initial preparations")
			}
			recoveredID = pending.VersionID
		}
	}
	if recoveredID != "" {
		id = recoveredID
	}
	record, err := b.journal.Load(ctx, a.TenantID, a.ID, id)
	if os.IsNotExist(err) {
		record, err = b.journal.Begin(ctx, a.TenantID, a.ID, id, a.RowVersion)
	}
	if err != nil {
		return zero, err
	}
	if record.ExpectedRowVersion != a.RowVersion {
		return zero, errors.New("stale initial preparation")
	}
	snapshotPath := filepath.Join(filepath.Dir(record.TempSource), "snapshot")
	raw, err := os.ReadFile(snapshotPath)
	var snapshot Snapshot
	if os.IsNotExist(err) {
		snapshot, err = b.capture.CaptureAgentSnapshot(model.ProviderRef, model.ModelID, b.tools)
		if err != nil {
			return zero, err
		}
		raw, err = json.Marshal(snapshot)
		if err != nil {
			return zero, err
		}
		if err = writePreparedSnapshot(snapshotPath, raw); err != nil {
			return zero, err
		}
	} else if err != nil {
		return zero, err
	}
	snapshot, err = ParseSnapshot(raw)
	if err != nil {
		return zero, err
	}
	if (model.ProviderRef != "" && model.ProviderRef != snapshot.Model.ProviderRef) || (model.ModelID != "" && model.ModelID != snapshot.Model.ModelID) {
		return zero, errors.New("preparation model changed")
	}
	ref := bundle.BundleRef{Commit: record.BundleCommit, Tag: record.Tag, Digest: record.BundleDigest}
	if record.Phase == state.PhaseReserved {
		// A durable Git tag may precede the bundled journal checkpoint.
		if recovery, ok := b.bundles.(interface {
			RecoverInitial(context.Context, string, string, string) (bundle.BundleRef, error)
		}); ok {
			ref, err = recovery.RecoverInitial(ctx, a.TenantID, a.ID, id)
			if err != nil && !os.IsNotExist(err) {
				return zero, err
			}
		}
		if ref.Commit == "" {
			// No immutable tag exists, so only this journal-owned disposable
			// template copy is replaced after an interrupted assembly.
			if err = os.RemoveAll(record.TempSource); err != nil {
				return zero, err
			}
			if err = os.MkdirAll(record.TempSource, 0700); err != nil {
				return zero, err
			}
			if err = b.templates.Assemble(ctx, a.TemplateCode, record.TempSource); err != nil {
				return zero, err
			}
			ref, err = b.bundles.CreateInitial(ctx, a.TenantID, a.ID, id, record.TempSource)
			if err != nil {
				return zero, err
			}
		}
		record.BundleCommit, record.BundleDigest, record.Tag = ref.Commit, ref.Digest, ref.Tag
		record.Phase = state.PhaseBundled
		if err = b.journal.Save(ctx, record); err != nil {
			return zero, err
		}
	}
	if record.SpecDigest != "" {
		digest, digestErr := SpecDigest(ref.Digest, snapshot)
		if digestErr != nil || digest != record.SpecDigest {
			return zero, errors.New("prepared snapshot digest mismatch")
		}
	}
	if err = b.bundles.Verify(ctx, a.TenantID, a.ID, ref); err != nil {
		return zero, err
	}
	record.Materialized, err = b.bundles.MaterializeVersion(ctx, a.TenantID, a.ID, id, ref)
	if err != nil {
		return zero, err
	}
	record.Phase = state.PhaseMaterialized
	if err = b.journal.Save(ctx, record); err != nil {
		return zero, err
	}
	workspace, err := b.bundles.MaterializeValidation(ctx, a.TenantID, a.ID, id, ref)
	if err != nil {
		return zero, err
	}
	report, err := b.validator.Validate(ctx, a.TenantID, a.ID, ref, workspace, snapshot)
	if err != nil {
		return zero, err
	}
	record.SpecDigest = report.SpecDigest
	record.Phase = state.PhaseValidated
	if err = b.journal.Save(ctx, record); err != nil {
		return zero, err
	}
	modelJSON, _ := json.Marshal(snapshot.Model)
	toolsJSON, _ := json.Marshal(snapshot.Tools)
	runtimeJSON, _ := json.Marshal(snapshot.Runtime)
	evidence, _ := json.Marshal(report)
	return agent.Version{ID: id, TenantID: a.TenantID, AgentID: a.ID, Number: 1, BundleCommit: ref.Commit, BundleTag: ref.Tag, BundleDigest: ref.Digest, SpecDigest: report.SpecDigest, ModelConfig: modelJSON, ToolPolicy: toolsJSON, RuntimeConfig: runtimeJSON, Validation: evidence, PublishedBy: p.UserID, PublishedAt: report.ValidatedAt, ChangeSummary: "Initial version"}, nil
}
func (b *InitialBuilder) CompleteInitial(ctx context.Context, v agent.Version) error {
	record, err := b.journal.Load(ctx, v.TenantID, v.AgentID, v.ID)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Phase != state.PhaseValidated || record.BundleCommit != v.BundleCommit || record.SpecDigest != v.SpecDigest {
		return errors.New("committed version differs from preparation")
	}
	return b.journal.Complete(ctx, record)
}
func writePreparedSnapshot(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".snapshot-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
