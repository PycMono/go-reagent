//go:build integration

package agent_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/config"
	agententity "github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	agentpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestMySQLAgentCatalogConstraintsAndRecovery(t *testing.T) {
	host := envOr("MYSQL_TEST_HOST", "127.0.0.1")
	port := envIntOr(t, "MYSQL_TEST_PORT", 3306)
	user := envOr("MYSQL_TEST_USER", "root")
	password := requiredEnv(t, "MYSQL_TEST_PASSWORD")
	schema := "go_reagent_agent_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", user, password, host, port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + schema + "`") })

	provider, err := mysql.NewProvider(&config.Config{
		Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100},
		MySQL:        config.MySQLConfig{Host: host, Port: port, Database: schema, User: user, Password: password, MaxOpen: 10, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := mysql.NewTransactionManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	db := provider.UseDB(context.Background())
	var serverVersion string
	if err := db.Raw("SELECT VERSION()").Scan(&serverVersion).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("MySQL server version: %s", serverVersion)

	for _, migration := range []string{"0001_conversation_persistence.up.sql", "0003_web_chat.up.sql", "0004_agent_profiles.up.sql", "0007_agent_catalog.up.sql"} {
		content, err := os.ReadFile(filepath.Join("../../../migrations", migration))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(content), ";") {
			if statement = strings.TrimSpace(statement); statement != "" {
				if err := db.Exec(statement).Error; err != nil {
					t.Fatalf("apply %s: %v", migration, err)
				}
			}
		}
	}

	repository := agentpersistence.NewRepository(provider, transactions)
	ctx := context.Background()
	draft := validAgent("agent-a", stringPtr("general"))
	reserved, err := repository.ReserveDraft(ctx, draft)
	if err != nil || reserved.ActiveVersionID != nil {
		t.Fatalf("ReserveDraft() = %#v, %v", reserved, err)
	}

	failedVersion := validVersion("version-failed", draft.ID, 1, nil)
	if err := repository.CommitInitial(ctx, reserved, failedVersion, reserved.RowVersion+1); err == nil {
		t.Fatal("stale initial pointer CAS accepted")
	}
	retained, err := repository.Find(ctx, draft.TenantID, draft.ID)
	if err != nil || retained.ActiveVersionID != nil {
		t.Fatalf("draft not retained after failed commit: %#v, %v", retained, err)
	}
	var failedVersionCount int64
	if err := db.Table("agent_versions").Where("id = ?", failedVersion.ID).Count(&failedVersionCount).Error; err != nil || failedVersionCount != 0 {
		t.Fatalf("failed version insert was not rolled back: count=%d err=%v", failedVersionCount, err)
	}

	version := validVersion("version-a1", draft.ID, 1, nil)
	if err := repository.CommitInitial(ctx, reserved, version, reserved.RowVersion); err != nil {
		t.Fatal(err)
	}
	active, err := repository.Find(ctx, draft.TenantID, draft.ID)
	if err != nil || active.ActiveVersionID == nil || *active.ActiveVersionID != version.ID {
		t.Fatalf("active agent = %#v, %v", active, err)
	}

	if _, err := repository.ReserveDraft(ctx, validAgent("agent-b", stringPtr("general"))); err == nil {
		t.Fatal("duplicate non-NULL bootstrap key accepted")
	}
	if _, err := repository.ReserveDraft(ctx, validAgent("agent-c", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReserveDraft(ctx, validAgent("agent-d", nil)); err != nil {
		t.Fatal("two NULL bootstrap keys must be allowed: ", err)
	}

	if err := db.Exec("INSERT INTO agent_versions (id,tenant_id,agent_id,version,bundle_commit,bundle_tag,bundle_digest,model_config_json,tool_policy_json,runtime_config_json,spec_digest,validation_json,source_training_session_id,published_by,published_at,change_summary) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		"version-dup", "tenant-a", "agent-a", 1, "commit", "tag", "sha256:bundle", `{}`, `{}`, `{}`, "sha256:spec", `{}`, nil, "admin", time.Now().UTC(), "duplicate").Error; err == nil {
		t.Fatal("duplicate version integer accepted")
	}

	if err := db.Exec("UPDATE agents SET active_version_id = ? WHERE tenant_id = ? AND id = ?", version.ID, "tenant-a", "agent-c").Error; err == nil {
		t.Fatal("cross-agent active version accepted")
	}

	if err := db.Exec("INSERT INTO agent_versions (id,tenant_id,agent_id,version,bundle_commit,bundle_tag,bundle_digest,model_config_json,tool_policy_json,runtime_config_json,spec_digest,validation_json,source_training_session_id,published_by,published_at,change_summary) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		"version-a2", "tenant-a", "agent-a", 2, "commit2", "tag2", "sha256:bundle2", `{}`, `{}`, `{}`, "sha256:spec2", `{}`, "session-a", "admin", time.Now().UTC(), "second").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO agent_versions (id,tenant_id,agent_id,version,bundle_commit,bundle_tag,bundle_digest,model_config_json,tool_policy_json,runtime_config_json,spec_digest,validation_json,source_training_session_id,published_by,published_at,change_summary) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		"version-a3", "tenant-a", "agent-a", 3, "commit3", "tag3", "sha256:bundle3", `{}`, `{}`, `{}`, "sha256:spec3", `{}`, "session-a", "admin", time.Now().UTC(), "third").Error; err == nil {
		t.Fatal("duplicate non-NULL source session accepted")
	}

	assertBinaryColumn(t, db, schema, "agents", "tenant_id")
	assertBinaryColumn(t, db, schema, "agent_versions", "agent_id")
	assertBinaryColumn(t, db, schema, "agent_conversations", "tenant_id")
}

func validAgent(id string, bootstrap *string) agententity.Agent {
	return agententity.Agent{ID: id, TenantID: "tenant-a", Name: id, Description: "desc", Status: "enabled", Presentation: agententity.Presentation{Starters: []agententity.Starter{}}, TemplateCode: "general", BootstrapKey: bootstrap, CreatedBy: "admin"}
}

func validVersion(id, agentID string, number uint64, source *string) agententity.Version {
	return agententity.Version{ID: id, TenantID: "tenant-a", AgentID: agentID, Number: number, BundleCommit: "commit", BundleTag: "tag", BundleDigest: "sha256:bundle", ModelConfig: []byte(`{}`), ToolPolicy: []byte(`{}`), RuntimeConfig: []byte(`{}`), SpecDigest: "sha256:spec", Validation: []byte(`{}`), SourceTrainingSessionID: source, PublishedBy: "admin", PublishedAt: time.Now().UTC(), ChangeSummary: "initial"}
}

func stringPtr(value string) *string { return &value }
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func envIntOr(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skip(name + " is not set")
	}
	return value
}

func assertBinaryColumn(t *testing.T, db *gorm.DB, schema, table, column string) {
	t.Helper()
	var collation string
	if err := db.Raw("SELECT collation_name FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?", schema, table, column).Scan(&collation).Error; err != nil {
		t.Fatal(err)
	}
	if collation != "utf8mb4_bin" {
		t.Fatalf("%s.%s collation = %q, want utf8mb4_bin", table, column, collation)
	}
}
