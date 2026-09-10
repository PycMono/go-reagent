package agent

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	agententity "github.com/PycMono/go-reagent/domain/entity/agent"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestFindScopesInitialSelectByTenantAndAgent(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, tenant_id, name, description, status, presentation_json, template_code, bootstrap_key, active_version_id, active_training_session_id, row_version, created_by, created_at, updated_at FROM agents WHERE tenant_id = ? AND id = ? LIMIT 1")).
		WithArgs("tenant-a", "agent-a").
		WillReturnRows(agentRows().AddRow("agent-a", "tenant-a", "Writer", "desc", "enabled", `{"icon":"pen","welcome":"hi","starters":[],"order":2}`, "general", nil, "version-a", nil, 3, "admin", now(), now()))

	got, err := repository.Find(context.Background(), "tenant-a", "agent-a")
	if err != nil || got.ID != "agent-a" || got.Presentation.Order != 2 {
		t.Fatalf("Find() = %#v, %v", got, err)
	}
	assertExpectations(t, mock)
}

func TestFindVersionScopesByTenantAgentAndVersion(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, tenant_id, agent_id, version, bundle_commit, bundle_tag, bundle_digest, model_config_json, tool_policy_json, runtime_config_json, spec_digest, validation_json, source_training_session_id, published_by, published_at, change_summary FROM agent_versions WHERE tenant_id = ? AND agent_id = ? AND id = ? LIMIT 1")).
		WithArgs("tenant-a", "agent-a", "version-a").
		WillReturnRows(versionRows().AddRow("version-a", "tenant-a", "agent-a", 1, "commit", "tag", "sha256:bundle", `{}`, `{}`, `{}`, "sha256:spec", `{}`, nil, "admin", now(), "initial"))

	got, err := repository.FindVersion(context.Background(), "tenant-a", "agent-a", "version-a")
	if err != nil || got.ID != "version-a" || got.Number != 1 {
		t.Fatalf("FindVersion() = %#v, %v", got, err)
	}
	assertExpectations(t, mock)
}

func TestCommitInitialRollsBackWhenPointerCASFails(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `agent_versions`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `agents` SET .*active_version_id.*row_version.*WHERE tenant_id = .*id = .*status = 'enabled'.*active_version_id IS NULL.*row_version =").
		WithArgs("version-a", "tenant-a", "agent-a", uint64(0)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repository.CommitInitial(context.Background(), draftAgent(), initialVersion(), 0)
	if !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("CommitInitial() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func TestCommitInitialCASRequiresEnabledDraft(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `agent_versions`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `agents` SET .*WHERE tenant_id = .*id = .*status = 'enabled'.*active_version_id IS NULL.*row_version =").
		WithArgs("version-a", "tenant-a", "agent-a", uint64(0)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repository.CommitInitial(context.Background(), draftAgent(), initialVersion(), 0)
	if !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("CommitInitial() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func TestListClampsLimitAndComputesDefaultIndependently(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectQuery("SELECT .* FROM agents AS a JOIN agent_versions AS v .*WHERE a.tenant_id = .*ORDER BY .*LIMIT ").
		WithArgs("tenant-a", 101).
		WillReturnRows(agentRows())
	mock.ExpectQuery("SELECT a.id FROM agents AS a JOIN agent_versions AS v .*WHERE a.tenant_id = .*status = 'enabled'.*ORDER BY .*LIMIT 1").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("default-a"))

	page, err := repository.List(context.Background(), agentrepo.ListQuery{TenantID: "tenant-a", Limit: 500})
	if err != nil || page.DefaultAgentID == nil || *page.DefaultAgentID != "default-a" {
		t.Fatalf("List() = %#v, %v", page, err)
	}
	assertExpectations(t, mock)
}

func TestListIncludingArchivedUsesLeftJoinSoDraftsRemainManageable(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectQuery("SELECT .* FROM agents AS a LEFT JOIN agent_versions AS v .*WHERE a.tenant_id = .*ORDER BY .*LIMIT ").
		WithArgs("tenant-a", 11).
		WillReturnRows(agentRows().AddRow("draft-a", "tenant-a", "Draft", "recover me", "enabled", `{"icon":"","welcome":"","starters":[],"order":-1}`, "general", nil, nil, nil, 0, "admin", now(), now()))
	mock.ExpectQuery("SELECT a.id FROM agents AS a JOIN agent_versions AS v .*WHERE a.tenant_id = .*status = 'enabled'.*ORDER BY .*LIMIT 1").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	page, err := repository.List(context.Background(), agentrepo.ListQuery{TenantID: "tenant-a", Limit: 10, IncludeArchived: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "draft-a" || page.Items[0].ActiveVersionID != nil {
		t.Fatalf("List(include archived) = %#v, %v", page, err)
	}
	assertExpectations(t, mock)
}

func TestListVersionsUsesBoundedDescendingStoragePagination(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	mock.ExpectQuery("SELECT .* FROM agent_versions WHERE tenant_id = .*agent_id = .*version < .*ORDER BY version DESC LIMIT ").
		WithArgs("tenant-a", "agent-a", uint64(8), 101).
		WillReturnRows(versionRows())
	if _, err := repository.ListVersions(context.Background(), "tenant-a", "agent-a", 8, 1000); err != nil {
		t.Fatal(err)
	}
	assertExpectations(t, mock)
}

func TestUpdatePresentationArchivesOnlyWithoutActiveTrainingAndUsesCAS(t *testing.T) {
	repository, mock := newRepositoryTest(t)
	agent := draftAgent()
	agent.Status = "archived"
	agent.Name = "Archived writer"
	mock.ExpectExec("UPDATE `agents` SET .*description.*name.*presentation_json.*row_version.*status.*WHERE .*tenant_id = .*id = .*row_version = .*active_training_session_id IS NULL").
		WithArgs("desc", "Archived writer", sqlmock.AnyArg(), "archived", "tenant-a", "agent-a", uint64(4)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repository.UpdatePresentation(context.Background(), agent, 4)
	if !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("UpdatePresentation() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func draftAgent() agententity.Agent {
	return agententity.Agent{ID: "agent-a", TenantID: "tenant-a", Name: "Writer", Description: "desc", Status: "enabled", Presentation: agententity.Presentation{Starters: []agententity.Starter{}}, TemplateCode: "general", RowVersion: 0, CreatedBy: "admin"}
}

func initialVersion() agententity.Version {
	return agententity.Version{ID: "version-a", TenantID: "tenant-a", AgentID: "agent-a", Number: 1, BundleCommit: "commit", BundleTag: "tag", BundleDigest: "sha256:bundle", ModelConfig: json.RawMessage(`{}`), ToolPolicy: json.RawMessage(`{}`), RuntimeConfig: json.RawMessage(`{}`), SpecDigest: "sha256:spec", Validation: json.RawMessage(`{}`), PublishedBy: "admin", PublishedAt: now(), ChangeSummary: "initial"}
}

func now() time.Time { return time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC) }

func agentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "status", "presentation_json", "template_code", "bootstrap_key", "active_version_id", "active_training_session_id", "row_version", "created_by", "created_at", "updated_at"})
}

func versionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "agent_id", "version", "bundle_commit", "bundle_tag", "bundle_digest", "model_config_json", "tool_policy_json", "runtime_config_json", "spec_digest", "validation_json", "source_training_session_id", "published_by", "published_at", "change_summary"})
}

type testProvider struct {
	db  *gorm.DB
	key struct{}
}

func (provider *testProvider) UseDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(provider.key).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return provider.db.WithContext(ctx)
}

func (provider *testProvider) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return provider.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return callback(context.WithValue(ctx, provider.key, tx))
	})
}

func (provider *testProvider) IsInTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(provider.key).(*gorm.DB)
	return ok
}
func (provider *testProvider) FindDB4TransContext(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(provider.key).(*gorm.DB)
	return tx
}

func newRepositoryTest(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{db: db}
	return NewRepository(provider, provider), mock
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var _ sqlsdk.Provider = (*testProvider)(nil)
var _ transaction.Manager = (*testProvider)(nil)
