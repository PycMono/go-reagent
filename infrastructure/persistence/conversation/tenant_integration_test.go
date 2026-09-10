//go:build integration

package conversation_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/config"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	mysqldriver "github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	conversationpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/conversation"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestMySQLTenantOwnedConversations(t *testing.T) {
	host := tenantEnvOr("MYSQL_TEST_HOST", "127.0.0.1")
	port := tenantEnvIntOr(t, "MYSQL_TEST_PORT", 3306)
	user := tenantEnvOr("MYSQL_TEST_USER", "root")
	password := tenantRequiredEnv(t, "MYSQL_TEST_PASSWORD")
	schema := "go_reagent_conv_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", user, password, host, port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + schema + "`") })

	provider, err := mysqldriver.NewProvider(&config.Config{Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100}, MySQL: config.MySQLConfig{Host: host, Port: port, Database: schema, User: user, Password: password, MaxOpen: 10, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500}})
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := mysqldriver.NewTransactionManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	db := provider.UseDB(context.Background())
	for _, migration := range []string{"0001_conversation_persistence.up.sql", "0002_model_invocation_observability.up.sql", "0003_web_chat.up.sql", "0004_agent_profiles.up.sql", "0005_invocation_ledger_tracing.up.sql", "0006_invocation_usage_enhancement.up.sql", "0007_agent_catalog.up.sql"} {
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
	// This fixture models the post-0008 owner index after authoritative backfill.
	if err := db.Exec("ALTER TABLE agent_conversations DROP INDEX uq_agent_conversations_owner, ADD UNIQUE KEY uq_agent_conversations_tenant_owner (tenant_id,user_id,conversation_id)").Error; err != nil {
		t.Fatal(err)
	}
	seedTenantAgent(t, db, "tenant-a", "agent-a", "version-a1")
	seedTenantAgent(t, db, "tenant-b", "agent-b", "version-b1")

	idService := serviceimpl.NewIDService(0)
	repository := conversationpersistence.NewConversationRepo(provider, transactions, idService)
	ctxA := identity.WithPrincipal(context.Background(), identity.Principal{TenantID: "tenant-a", UserID: "SameUser", Role: identity.RoleUser})
	ctxB := identity.WithPrincipal(context.Background(), identity.Principal{TenantID: "tenant-b", UserID: "SameUser", Role: identity.RoleUser})
	conversationA := boundConversation("tenant-a", "SameUser", "same-chat", "agent-a", "version-a1")
	conversationB := boundConversation("tenant-b", "SameUser", "same-chat", "agent-b", "version-b1")
	if err := repository.CreateBound(ctxA, conversationA); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateBound(ctxB, conversationB); err != nil {
		t.Fatal(err)
	}
	onlyA := boundConversation("tenant-a", "SameUser", "only-a", "agent-a", "version-a1")
	if err := repository.CreateBound(ctxA, onlyA); err != nil {
		t.Fatal(err)
	}
	fixed := boundConversation("tenant-a", "SameUser", "fixed-a", "agent-a", "version-a1")
	fixed.FollowLatest = false
	if err := repository.CreateBound(ctxA, fixed); err != nil {
		t.Fatal(err)
	}
	loadedFixed, found, err := repository.FindByUserIDAndConversationID(ctxA, "SameUser", "fixed-a")
	if err != nil || !found || loadedFixed.FollowLatest {
		t.Fatalf("fixed-version binding = %#v, %v, %v", loadedFixed, found, err)
	}
	if conversationA.ID == conversationB.ID {
		t.Fatal("tenant conversations share internal ID")
	}
	mismatched := boundConversation("tenant-a", "SameUser", "bad-binding", "agent-a", "version-b1")
	if err := repository.CreateBound(ctxA, mismatched); !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("cross-agent/version binding error = %v", err)
	}
	for _, tc := range []struct {
		ctx    context.Context
		tenant string
	}{{ctxA, "tenant-a"}, {ctxB, "tenant-b"}} {
		got, found, err := repository.FindByUserIDAndConversationID(tc.ctx, "SameUser", "same-chat")
		if err != nil || !found || got.TenantID != tc.tenant {
			t.Fatalf("tenant find = %#v, %v, %v", got, found, err)
		}
	}
	if _, _, err := repository.FindByUserIDAndConversationID(ctxA, "sameuser", "same-chat"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("case-only owner attack = %v", err)
	}

	message := &conversationentity.Message{ID: "message-a", ConversationID: conversationA.ID, TurnVersion: 1, Ordinal: 0, RunID: "run-a", Role: conversationentity.RoleUser, Payload: conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{Type: conversationentity.ContentTypeText, Text: "secret"}}}}
	if err := db.Create(message).Error; err != nil {
		t.Fatal(err)
	}
	if messages, err := repository.ListMessagesByConversationID(ctxB, conversationA.ID, 10); err != nil || len(messages) != 0 {
		t.Fatalf("cross-tenant history = %#v, %v", messages, err)
	}
	if err := repository.Rename(ctxB, "SameUser", "only-a", "attack"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("cross-tenant rename = %v", err)
	}
	if err := repository.Delete(ctxB, "SameUser", "only-a"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("cross-tenant delete = %v", err)
	}
	if err := repository.AppendTurn(ctxB, "SameUser", "only-a", 0, []*conversationentity.Message{{Role: conversationentity.RoleUser, Payload: message.Payload}}, nil); !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("cross-tenant append = %v", err)
	}

	insertVersion(t, db, "tenant-a", "agent-a", "version-a2", 2)
	if err := db.Exec("UPDATE agents SET active_version_id=? WHERE tenant_id=? AND id=?", "version-a2", "tenant-a", "agent-a").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitAgentVersion(ctxA, "SameUser", "same-chat", "version-a1", "version-a2"); err != nil {
		t.Fatal(err)
	}
	updated, found, err := repository.FindByUserIDAndConversationID(ctxA, "SameUser", "same-chat")
	if err != nil || !found || updated.AgentVersionID != "version-a2" {
		t.Fatalf("committed version = %#v, %v, %v", updated, found, err)
	}

	training := boundConversation("tenant-a", "SameUser", "training-a", "agent-a", "version-a1")
	training.ID = "training-internal"
	training.ConversationType = "training"
	if err := db.Create(training).Error; err != nil {
		t.Fatal(err)
	}
	trainingMessage := &conversationentity.Message{ID: "training-message", ConversationID: training.ID, TurnVersion: 1, Ordinal: 0, RunID: "training-run", Role: conversationentity.RoleUser, Payload: message.Payload}
	if err := db.Create(trainingMessage).Error; err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.FindByUserIDAndConversationID(ctxA, "SameUser", "training-a"); err != nil || found {
		t.Fatalf("training conversation exposed: found=%v err=%v", found, err)
	}
	if messages, err := repository.ListMessagesByConversationID(ctxA, training.ID, 10); err != nil || len(messages) != 0 {
		t.Fatalf("training history exposed by internal ID: %#v, %v", messages, err)
	}
	trainingPage, err := repository.ListMessages(ctxA, conversationrepo.MessageQuery{UserID: "SameUser", ConversationID: "training-a", Limit: 10})
	if err != nil || len(trainingPage.Items) != 0 {
		t.Fatalf("training history exposed by public ID: %#v, %v", trainingPage, err)
	}
	if err := repository.Rename(ctxA, "SameUser", "training-a", "attack"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("training rename = %v", err)
	}
	if err := repository.Delete(ctxA, "SameUser", "training-a"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("training delete = %v", err)
	}
	if err := repository.AppendTurn(ctxA, "SameUser", "training-a", 0, []*conversationentity.Message{{Role: conversationentity.RoleUser, Payload: message.Payload}}, nil); !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("training append = %v", err)
	}
	page, err := repository.ListByUserID(ctxA, conversationrepo.ListQuery{UserID: "SameUser", AgentID: "agent-a", Limit: 20})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("scoped list = %#v, %v", page, err)
	}
}

func seedTenantAgent(t *testing.T, db *gorm.DB, tenant, agentID, versionID string) {
	t.Helper()
	if err := db.Exec("INSERT INTO agents (id,tenant_id,name,description,status,presentation_json,template_code,row_version,created_by) VALUES (?,?,?,?,?,JSON_OBJECT(),'general',0,'admin')", agentID, tenant, agentID, "desc", "enabled").Error; err != nil {
		t.Fatal(err)
	}
	insertVersion(t, db, tenant, agentID, versionID, 1)
	if err := db.Exec("UPDATE agents SET active_version_id=? WHERE tenant_id=? AND id=?", versionID, tenant, agentID).Error; err != nil {
		t.Fatal(err)
	}
}

func insertVersion(t *testing.T, db *gorm.DB, tenant, agentID, versionID string, number uint64) {
	t.Helper()
	if err := db.Exec("INSERT INTO agent_versions (id,tenant_id,agent_id,version,bundle_commit,bundle_tag,bundle_digest,model_config_json,tool_policy_json,runtime_config_json,spec_digest,validation_json,published_by,published_at,change_summary) VALUES (?,?,?,?,'commit','tag','sha256:bundle',JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),'sha256:spec',JSON_OBJECT(),'admin',NOW(6),'initial')", versionID, tenant, agentID, number).Error; err != nil {
		t.Fatal(err)
	}
}

func boundConversation(tenant, user, publicID, agentID, versionID string) *conversationentity.Conversation {
	return &conversationentity.Conversation{TenantID: tenant, UserID: user, ConversationID: publicID, ConversationType: "chat", AgentID: agentID, AgentVersionID: versionID, FollowLatest: true, Name: "Untitled Chat", ProfileCode: "general"}
}
func tenantEnvOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func tenantEnvIntOr(t *testing.T, name string, fallback int) int {
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
func tenantRequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skip(name + " is not set")
	}
	return value
}
