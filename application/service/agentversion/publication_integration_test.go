package agentversion_test

import (
	"context"
	"encoding/json"
	"github.com/PycMono/go-reagent/application/identity"
	version "github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	repo "github.com/PycMono/go-reagent/domain/repository/agent"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentstate"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type publicationRepo struct {
	repo.Repository
	a        agent.Agent
	versions map[string]agent.Version
}

func (r *publicationRepo) Find(context.Context, string, string) (agent.Agent, error) { return r.a, nil }
func (r *publicationRepo) FindVersion(_ context.Context, tenant, agentID, id string) (agent.Version, error) {
	v, ok := r.versions[id]
	if !ok || v.TenantID != tenant || v.AgentID != agentID {
		return v, commonerrors.ErrNotFound
	}
	return v, nil
}
func (r *publicationRepo) ListVersions(context.Context, string, string, uint64, int) (repo.VersionPage, error) {
	var latest agent.Version
	for _, v := range r.versions {
		if v.Number > latest.Number {
			latest = v
		}
	}
	return repo.VersionPage{Items: []agent.Version{latest}}, nil
}
func (r *publicationRepo) CommitModelRelease(_ context.Context, t, a string, expected uint64, base string, v agent.Version, e version.ValidationReport) error {
	if expected != r.a.RowVersion || *r.a.ActiveVersionID != base {
		return commonerrors.ErrConflict
	}
	r.versions[v.ID] = v
	r.a.ActiveVersionID = &v.ID
	r.a.RowVersion++
	return nil
}
func (r *publicationRepo) ActivateVersion(_ context.Context, t, a, id string, expected uint64, e version.ValidationReport) error {
	if expected != r.a.RowVersion {
		return commonerrors.ErrConflict
	}
	r.a.ActiveVersionID = &id
	r.a.RowVersion++
	return nil
}

type admission struct{}

func (admission) Reserve(context.Context, string, string, string) (func(), error) {
	return func() {}, nil
}

type ids struct{ n int }

func (i *ids) NextID() string { i.n++; return string(rune('a' + i.n)) }
func TestModelReleaseKeepsHistoricalSnapshotAndRejectsOldCAS(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				_ = os.Chmod(p, 0700)
			}
			return nil
		})
	})
	store, _ := agentbundle.New(root)
	journal, _ := agentstate.New(root)
	cfg := &config.Config{CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid/v1/", APIKey: "secret", Model: "original", Pricing: &providers.Pricing{InputUSDPerMillionTokens: 1}}}}
	check, _ := version.NewValidator(store, func(context.Context, version.Snapshot) error { return nil }, func(context.Context, string, version.Snapshot) error { return nil }, func(context.Context) (time.Time, error) { return time.Now(), nil }, version.ValidationPolicy{Version: "test", Environment: "fake-smoke-test", Timeout: time.Second, TTL: time.Minute})
	builder, _ := version.NewInitialBuilder(store, journal, cfg, &template{}, check, nil)
	principal := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleAdmin}
	a := agent.Agent{ID: "a", TenantID: "t", Status: "enabled", TemplateCode: "x"}
	base, err := builder.PrepareInitial(ctx, principal, a, "v1", dto.ModelChoice{})
	if err != nil {
		t.Fatal(err)
	}
	a.ActiveVersionID = &base.ID
	a.RowVersion = 1
	r := &publicationRepo{a: a, versions: map[string]agent.Version{base.ID: base}}
	service, _ := version.NewPublicationService(r, store, store, check, cfg, admission{}, &ids{})
	cfg.Platforms[0].Model = "new-model"
	cfg.Conversation.HistoryMessageLimit = 999
	request := dto.ModelConfigReleaseDTO{ExpectedRowVersion: 1, BaseVersionID: base.ID, ModelConfig: dto.ModelChoice{ProviderRef: "p", ModelID: "new-model"}, ChangeSummary: "new model", Confirmed: true}
	before, _ := json.Marshal(base)
	result, err := service.ReleaseModel(ctx, principal, a.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	got := r.versions[result.ID]
	if got.BundleDigest != base.BundleDigest || got.SpecDigest == base.SpecDigest || got.BundleTag == base.BundleTag || string(got.RuntimeConfig) != string(base.RuntimeConfig) {
		t.Fatal("release did not preserve immutable non-model behavior")
	}
	after, _ := json.Marshal(r.versions[base.ID])
	if string(before) != string(after) {
		t.Fatal("historical evidence changed")
	}
	if _, err = service.ReleaseModel(ctx, principal, a.ID, request); err != commonerrors.ErrConflict {
		t.Fatalf("stale CAS: %v", err)
	}
	if _, err = service.Activate(ctx, principal, a.ID, base.ID, r.a.RowVersion); err != nil {
		t.Fatal(err)
	}
	after, _ = json.Marshal(r.versions[base.ID])
	if string(before) != string(after) {
		t.Fatal("activation rewrote historical evidence")
	}
}
