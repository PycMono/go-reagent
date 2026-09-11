//go:build integration

package agent_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentadmission"
	"github.com/PycMono/go-reagent/application/service/agenttraining"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	"github.com/PycMono/go-reagent/config"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	bundles "github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentsmoke"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	agentpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	trainingpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agenttraining"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
)

type trainingIDs struct{ n atomic.Uint64 }

type cancelPublicationPreparation struct {
	agentrepo.Repository
	cancel context.CancelFunc
}

func (r cancelPublicationPreparation) ListVersions(ctx context.Context, tenant, agentID string, before uint64, limit int) (agentrepo.VersionPage, error) {
	page, err := r.Repository.ListVersions(ctx, tenant, agentID, before, limit)
	r.cancel()
	return page, err
}

func (i *trainingIDs) NextID() string { return fmt.Sprintf("test-%d", i.n.Add(1)) }

type fileAuthor struct {
	store *bundles.Store
	calls int
}

func (a *fileAuthor) Preview(_ context.Context, _ training.Session, in dto.TrainingPreviewDTO, _, root string) (pi.RunResult, error) {
	behavior, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return pi.RunResult{}, err
	}
	return pi.RunResult{NewMessages: []ai.Message{{Role: ai.RoleAssistant, Content: ai.ContentBlocks{ai.TextBlock("测试响应：" + in.Content + "\n\n当前候选规则：\n" + string(behavior))}}}}, nil
}

func (a *fileAuthor) Run(ctx context.Context, s training.Session, in dto.TrainingRunDTO, _ []*conversation.Message, _ pi.EventListener) (pi.RunResult, error) {
	a.calls++
	root, err := a.store.CandidatePath(ctx, bundle.Candidate{TenantID: s.TenantID, AgentID: s.AgentID, TrainingID: s.ID, Head: s.CandidateHead})
	if err != nil {
		return pi.RunResult{}, err
	}
	err = os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Agent\n"+in.Content+"\n"), 0600)
	return pi.RunResult{NewMessages: []ai.Message{{Role: ai.RoleAssistant, Content: ai.ContentBlocks{ai.TextBlock("updated candidate")}}}}, err
}

func TestMySQLTrainingPublishAndRollback(t *testing.T) {
	ctx := context.Background()
	password := requiredEnv(t, "MYSQL_TEST_PASSWORD")
	host := envOr("MYSQL_TEST_HOST", "127.0.0.1")
	port := envIntOr(t, "MYSQL_TEST_PORT", 3306)
	user := envOr("MYSQL_TEST_USER", "root")
	schema := fmt.Sprintf("go_reagent_training_%d", time.Now().UnixNano())
	admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", user, password, host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP DATABASE `" + schema + "`")
	cfg := &config.Config{Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100}, MySQL: config.MySQLConfig{Host: host, Port: port, Database: schema, User: user, Password: password, MaxOpen: 10, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 1, SlowThreshold: 500}, CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid", APIKey: "test", Model: "model", Pricing: &providers.Pricing{}}}, AgentTraining: config.AgentTrainingConfig{SessionTTLSeconds: 3600, ValidationTTLSeconds: 300, SmokeTimeoutSeconds: 10}}
	provider, err := mysql.NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := mysql.NewTransactionManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	db := provider.UseDB(ctx)
	for _, name := range []string{"0001_conversation_persistence.up.sql", "0003_web_chat.up.sql", "0004_agent_profiles.up.sql", "0007_agent_catalog.up.sql", "0008_agent_conversation_ownership.up.sql", "0009_remove_legacy_profile.up.sql", "0010_agent_training_sessions.up.sql"} {
		content, err := os.ReadFile(filepath.Join("../../../migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(string(content), ";") {
			if strings.TrimSpace(stmt) != "" {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			}
		}
	}
	dataDir := t.TempDir()
	cfg.AgentDataDir = dataDir
	t.Cleanup(func() {
		_ = filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0700)
			}
			return nil
		})
	})
	store, err := bundles.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("# Agent\nOriginal behavior.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := store.CreateInitial(ctx, "tenant-a", "agent-a", "version-a1", source)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cfg.CaptureAgentSnapshot("p", "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := agentpersistence.NewRepository(provider, tx)
	draft, err := repo.ReserveDraft(ctx, validAgent("agent-a", nil))
	if err != nil {
		t.Fatal(err)
	}
	v := validVersion("version-a1", "agent-a", 1, nil)
	v.BundleCommit = ref.Commit
	v.BundleTag = ref.Tag
	v.BundleDigest = ref.Digest
	v.ModelConfig, _ = json.Marshal(snapshot.Model)
	v.ToolPolicy, _ = json.Marshal(snapshot.Tools)
	v.RuntimeConfig, _ = json.Marshal(snapshot.Runtime)
	v.SpecDigest, err = agentversion.SpecDigest(ref.Digest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	smoke, _ := agentsmoke.New(cfg.AgentTraining)
	clock := func(context.Context) (time.Time, error) {
		return mysql.UTCNow(db)
	}
	validator, err := agentversion.NewValidator(store, func(_ context.Context, s agentversion.Snapshot) error {
		_, err := cfg.ResolveAgentModel(s.Model)
		return err
	}, smoke.Check, clock, agentversion.ValidationPolicy{Version: "1", Environment: "integration", Timeout: 10 * time.Second, TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.MaterializeValidation(ctx, v.TenantID, v.AgentID, "initial", ref)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := validator.Validate(ctx, v.TenantID, v.AgentID, ref, root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	v.Validation, _ = json.Marshal(evidence)
	if err := repo.CommitInitial(ctx, draft, v, draft.RowVersion); err != nil {
		t.Fatal(err)
	}
	ids := &trainingIDs{}
	r := trainingpersistence.NewRepository(provider, tx, ids)
	author := &fileAuthor{store: store}
	admission := agentadmission.New()
	service, err := agenttraining.NewService(r, repo, store, validator, author, admission, cfg.AgentTraining, ids, cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{TenantID: "tenant-a", UserID: "admin", Role: identity.RoleAdmin}
	a, err := repo.Find(ctx, p.TenantID, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Create(ctx, p, a.ID, dto.CreateTrainingDTO{ExpectedRowVersion: a.RowVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, p, a.ID, dto.CreateTrainingDTO{ExpectedRowVersion: a.RowVersion}); err == nil {
		t.Fatal("second active training accepted")
	}
	other := p
	other.TenantID = "other"
	if _, err := service.Get(ctx, other, session.ID); err == nil {
		t.Fatal("cross tenant read accepted")
	}
	request := dto.TrainingRunDTO{TrainingMutationDTO: dto.TrainingMutationDTO{ExpectedRowVersion: session.RowVersion, RequestID: "run-1"}, RunID: "run-1", Content: "New behavior."}
	session, err = service.Run(ctx, p, session.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Run(ctx, p, session.ID, request); err != nil || author.calls != 1 {
		t.Fatalf("duplicate run repeated: %v %d", err, author.calls)
	}
	diff, err := service.Diff(ctx, p, session.ID, 4096)
	if err != nil || diff.ChangedPaths != 1 {
		t.Fatalf("checkpoint diff: %+v %v", diff, err)
	}
	messages, err := service.Messages(ctx, p, session.ID, 0, 100)
	if err != nil || len(messages) != 2 {
		t.Fatalf("persisted messages: %d %v", len(messages), err)
	}
	preview, err := service.Preview(ctx, p, session.ID, dto.TrainingPreviewDTO{Content: "test candidate"})
	if err != nil || !strings.Contains(preview, "New behavior.") {
		t.Fatalf("candidate preview: %q %v", preview, err)
	}
	unchanged, err := service.Get(ctx, p, session.ID)
	if err != nil || unchanged.RowVersion != session.RowVersion {
		t.Fatal("preview modified training state", err)
	}
	messages, err = service.Messages(ctx, p, session.ID, 0, 100)
	if err != nil || len(messages) != 2 {
		t.Fatal("preview entered author history", err)
	}
	session, err = service.Validate(ctx, p, session.ID, dto.TrainingMutationDTO{ExpectedRowVersion: session.RowVersion, RequestID: "validate-1"})
	if err != nil || session.Status != training.Ready {
		t.Fatalf("validation: %+v %v", session, err)
	}
	interruptedCtx, interrupt := context.WithCancel(ctx)
	interrupted, err := agenttraining.NewService(r, cancelPublicationPreparation{repo, interrupt}, store, validator, author, admission, cfg.AgentTraining, ids, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interrupted.Publish(interruptedCtx, p, session.ID, dto.TrainingPublishDTO{TrainingMutationDTO: dto.TrainingMutationDTO{ExpectedRowVersion: session.RowVersion, RequestID: "cancel-preparation"}, HumanConfirmed: true, ChangeSummary: "interrupted preparation"}); err == nil {
		t.Fatal("interrupted publication accepted")
	}
	session, err = service.Get(ctx, p, session.ID)
	if err != nil || session.Operation.State == training.OperationRunning {
		t.Fatalf("preparation failure stranded operation: %+v %v", session, err)
	}
	session, err = service.Validate(ctx, p, session.ID, dto.TrainingMutationDTO{ExpectedRowVersion: session.RowVersion, RequestID: "validate-after-interruption"})
	if err != nil {
		t.Fatal(err)
	}
	release := dto.TrainingPublishDTO{TrainingMutationDTO: dto.TrainingMutationDTO{ExpectedRowVersion: session.RowVersion, RequestID: "publish-1"}, HumanConfirmed: true, ChangeSummary: "new behavior"}
	published, err := service.Publish(ctx, p, session.ID, release)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.Publish(ctx, p, session.ID, release)
	if err != nil || duplicate.ID != published.ID {
		t.Fatalf("duplicate publish: %+v %v", duplicate, err)
	}
	a, err = repo.Find(ctx, p.TenantID, a.ID)
	if err != nil || a.ActiveVersionID == nil || *a.ActiveVersionID != published.ID || a.ActiveTrainingSessionID != nil {
		t.Fatalf("publication pointers: %+v %v", a, err)
	}
	publication, err := agentversion.NewPublicationService(repo, store, store, validator, cfg, admission, ids)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publication.Activate(ctx, p, a.ID, v.ID, a.RowVersion); err != nil {
		t.Fatal(err)
	}
	a, err = repo.Find(ctx, p.TenantID, a.ID)
	if err != nil || *a.ActiveVersionID != v.ID {
		t.Fatal("rollback failed", err)
	}
	browserQA(t, cfg, repo, store, validator, ids, service, publication, p)
}
