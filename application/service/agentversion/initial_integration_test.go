package agentversion_test

import (
	"context"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	state "github.com/PycMono/go-reagent/application/port/agentstate"
	version "github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentstate"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type template struct {
	calls int
	fail  bool
}

func (t *template) Assemble(ctx context.Context, code, dest string) error {
	t.calls++
	if err := os.WriteFile(filepath.Join(dest, "AGENTS.md"), []byte("You are a helpful assistant.\n"), 0600); err != nil {
		return err
	}
	if t.fail {
		return errors.New("interrupted assembly")
	}
	return nil
}

type failedCheckpoint struct {
	state.Journal
	fail bool
}

func (j *failedCheckpoint) Save(ctx context.Context, p state.Preparation) error {
	if j.fail && p.Phase == state.PhaseBundled {
		j.fail = false
		return errors.New("checkpoint failed")
	}
	return j.Journal.Save(ctx, p)
}
func TestInitialRecoveryPreservesGitAndSnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				_ = os.Chmod(path, 0700)
			}
			return nil
		})
	})
	store, e := agentbundle.New(root)
	if e != nil {
		t.Fatal(e)
	}
	disk, e := agentstate.New(root)
	if e != nil {
		t.Fatal(e)
	}
	journal := &failedCheckpoint{Journal: disk, fail: true}
	cfg := &config.Config{CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid/v1/", APIKey: "secret", Model: "original", Pricing: &providers.Pricing{InputUSDPerMillionTokens: 1}}}}
	smokeCalls := 0
	check, e := version.NewValidator(store, func(context.Context, version.Snapshot) error { return nil }, func(context.Context, string, version.Snapshot) error { smokeCalls++; return nil }, func(context.Context) (time.Time, error) { return time.Now().UTC(), nil }, version.ValidationPolicy{Version: "test-v1", Environment: "fake-smoke-test", Timeout: time.Second, TTL: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	templates := &template{}
	builder, e := version.NewInitialBuilder(store, journal, cfg, templates, check, nil)
	if e != nil {
		t.Fatal(e)
	}
	p := identity.Principal{TenantID: "t", UserID: "admin", Role: identity.RoleAdmin}
	a := agent.Agent{ID: "a", TenantID: "t", TemplateCode: "x"}
	if _, e = builder.PrepareInitial(ctx, p, a, "v", dto.ModelChoice{}); e == nil {
		t.Fatal("checkpoint failure accepted")
	}
	ref, e := store.RecoverInitial(ctx, "t", "a", "v")
	if e != nil {
		t.Fatal(e)
	}
	cfg.Platforms[0].Model = "changed"
	got, e := builder.PrepareInitial(ctx, p, a, "new-id-on-retry", dto.ModelChoice{})
	if e != nil {
		t.Fatal(e)
	}
	if got.BundleCommit != ref.Commit || templates.calls != 1 || smokeCalls != 1 {
		t.Fatalf("recovery rebuilt content: %+v calls=%d", got, templates.calls)
	}
	if string(got.ModelConfig) == "" {
		t.Fatal("snapshot missing")
	}
	if _, e = builder.PrepareInitial(ctx, identity.Principal{TenantID: "other", UserID: "admin", Role: identity.RoleAdmin}, a, "v", dto.ModelChoice{}); e == nil {
		t.Fatal("cross tenant accepted")
	}
}

func TestInterruptedTemplateAssemblyCanRetry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, _ := agentbundle.New(root)
	journal, _ := agentstate.New(root)
	cfg := &config.Config{CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid/v1/", APIKey: "secret", Model: "original", Pricing: &providers.Pricing{InputUSDPerMillionTokens: 1}}}}
	check, _ := version.NewValidator(store, func(context.Context, version.Snapshot) error { return nil }, func(context.Context, string, version.Snapshot) error {
		return errors.New("intentional smoke rejection")
	}, func(context.Context) (time.Time, error) { return time.Now(), nil }, version.ValidationPolicy{Version: "test", Environment: "test", Timeout: time.Second, TTL: time.Minute})
	templates := &template{fail: true}
	builder, _ := version.NewInitialBuilder(store, journal, cfg, templates, check, nil)
	p := identity.Principal{TenantID: "t", UserID: "admin", Role: identity.RoleAdmin}
	a := agent.Agent{ID: "a", TenantID: "t", TemplateCode: "x"}
	_, _ = builder.PrepareInitial(ctx, p, a, "v", dto.ModelChoice{})
	templates.fail = false
	_, err := builder.PrepareInitial(ctx, p, a, "v", dto.ModelChoice{})
	if err == nil || err.Error() != "intentional smoke rejection" {
		t.Fatalf("retry did not reach validation: %v", err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				_ = os.Chmod(path, 0700)
			}
			return nil
		})
	})
}
