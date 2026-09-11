//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	entity "github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	persistence "github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLOwnershipBackfillAndRollbackGuard(t *testing.T) {
	password := os.Getenv("MYSQL_TEST_PASSWORD")
	if password == "" {
		t.Skip("MYSQL_TEST_PASSWORD not configured")
	}
	host := os.Getenv("MYSQL_TEST_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port, _ := strconv.Atoi(os.Getenv("MYSQL_TEST_PORT"))
	if port == 0 {
		port = 3306
	}
	user := os.Getenv("MYSQL_TEST_USER")
	if user == "" {
		user = "root"
	}
	schema := fmt.Sprintf("go_reagent_migration_%d", time.Now().UnixNano())
	admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", user, password, host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP DATABASE `" + schema + "`")
	cfg := &config.Config{Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100}, MySQL: config.MySQLConfig{Host: host, Port: port, User: user, Password: password, Database: schema, MaxOpen: 5, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 1, SlowThreshold: 500}, CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid", APIKey: "test", Model: "model", Pricing: &providers.Pricing{}}}}
	provider, err := mysql.NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := mysql.NewTransactionManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := provider.UseDB(ctx)
	apply := func(name string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("../../migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(stmt) != "" {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			}
		}
	}
	for _, name := range []string{"0001_conversation_persistence.up.sql", "0003_web_chat.up.sql", "0004_agent_profiles.up.sql", "0007_agent_catalog.up.sql"} {
		apply(name)
	}
	repo := persistence.NewRepository(provider, tx)
	data := t.TempDir()
	store, err := agentbundle.New(data)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("# Agent\nUse trusted sources."), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := cfg.CaptureAgentSnapshot("p", "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := func(tenant, id, version string) {
		t.Helper()
		key := "general"
		draft, err := repo.ReserveDraft(ctx, entity.Agent{ID: id, TenantID: tenant, Name: "Agent", Description: "test", Status: "enabled", TemplateCode: "general", BootstrapKey: &key, CreatedBy: "operator", Presentation: entity.Presentation{Icon: "sparkles", Starters: []entity.Starter{}}})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := store.CreateInitial(ctx, tenant, id, version, source)
		if err != nil {
			t.Fatal(err)
		}
		v := entity.Version{ID: version, TenantID: tenant, AgentID: id, Number: 1, BundleCommit: ref.Commit, BundleTag: ref.Tag, BundleDigest: ref.Digest, PublishedBy: "operator", PublishedAt: time.Now().UTC(), ChangeSummary: "initial", Validation: json.RawMessage(`{"digest_version":1}`)}
		v.ModelConfig, _ = json.Marshal(snapshot.Model)
		v.ToolPolicy, _ = json.Marshal(snapshot.Tools)
		v.RuntimeConfig, _ = json.Marshal(snapshot.Runtime)
		v.SpecDigest, err = agentversion.SpecDigest(ref.Digest, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CommitInitial(ctx, draft, v, draft.RowVersion); err != nil {
			t.Fatal(err)
		}
	}
	seed("tenant-a", "a", "v-a")
	seed("tenant-b", "b", "v-b")
	if err := db.Exec("INSERT INTO agent_conversations (id,user_id,conversation_id,profile_code) VALUES ('c1','same-user','same-public','general')").Error; err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := migrate(ctx, db, repo, store, "backfill", "", false, nil, true, &output); err == nil {
		t.Fatal("inferred missing tenant")
	}
	owners := map[string]string{"c1": "tenant-a"}
	if err := migrate(ctx, db, repo, store, "backfill", "", false, owners, true, &output); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("agent_conversations").Where("tenant_id IS NULL").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("dry run wrote bindings", err)
	}
	if err := migrate(ctx, db, repo, store, "backfill", "", false, owners, false, &output); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db, repo, store, "backfill", "", false, owners, false, &output); err != nil {
		t.Fatal("backfill not resumable", err)
	}
	if err := migrate(ctx, db, repo, store, "verify", "", false, nil, true, &output); err != nil {
		t.Fatal(err)
	}
	apply("0008_agent_conversation_ownership.up.sql")
	if err := db.Exec("INSERT INTO agent_conversations (id,user_id,conversation_id,profile_code,tenant_id,agent_id,agent_version_id) VALUES ('c2','same-user','same-public','general','tenant-b','b','v-b')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE agent_conversations ADD UNIQUE KEY uq_agent_conversations_owner(user_id,conversation_id)").Error; err == nil {
		t.Fatal("unsafe ownership rollback accepted")
	}
	if err := db.Table("agent_conversations").Count(&count).Error; err != nil || count != 2 {
		t.Fatal("rollback removed customer rows", err)
	}
	apply("0009_remove_legacy_profile.up.sql")
	apply("0010_agent_training_sessions.up.sql")
	if err := migrate(ctx, db, repo, store, "verify", "", false, nil, true, &output); err != nil {
		t.Fatal("retired schema cannot verify", err)
	}
	// Copy the complete Git inventory to another data root, keeping the same
	// consistent DB snapshot. Verification cannot depend on disposable caches.
	restored := t.TempDir()
	if err := os.CopyFS(restored, os.DirFS(data)); err != nil {
		t.Fatal(err)
	}
	restoredStore, err := agentbundle.New(restored)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db, repo, restoredStore, "verify", "", false, nil, true, &output); err != nil {
		t.Fatal("restored Git inventory invalid", err)
	}
	v, err := repo.FindVersion(ctx, "tenant-a", "a", "v-a")
	if err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(restored, "tenants", "tenant-a", "agents", "a", "bundle.git", "objects", v.BundleCommit[:2], v.BundleCommit[2:])
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db, repo, restoredStore, "verify", "", false, nil, true, &output); err == nil {
		t.Fatal("missing Git object accepted after restore")
	}
}
