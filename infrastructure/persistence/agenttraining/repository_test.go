package agenttraining

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/PycMono/go-reagent/application/identity"
	ce "github.com/PycMono/go-reagent/common/errors"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
	"time"
)

type testIDs struct{}

func (testIDs) NextID() string { return "generated-id" }

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
	return NewRepository(provider, provider, testIDs{}), mock
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOwnedRejectsNonAdminBeforeQuery(t *testing.T) {
	r, m := newRepositoryTest(t)
	_, err := r.FindOwned(context.Background(), identity.Principal{TenantID: "tenant", UserID: "user", Role: identity.RoleUser}, "id")
	if !errors.Is(err, ce.ErrForbidden) {
		t.Fatalf("%v", err)
	}
	assertExpectations(t, m)
}
func TestFindOwnedScopesTenantAdminAndID(t *testing.T) {
	r, m := newRepositoryTest(t)
	m.ExpectQuery("SELECT .* FROM `agent_training_sessions` WHERE tenant_id = .*admin_user_id = .*id =").WithArgs("tenant", "admin", "session").WillReturnRows(sqlmock.NewRows([]string{"id", "operation_json"}).AddRow("session", `{}`))
	s, err := r.FindOwned(context.Background(), identity.Principal{TenantID: "tenant", UserID: "admin", Role: identity.RoleAdmin}, "session")
	if err != nil || s.ID != "session" {
		t.Fatalf("%#v %v", s, err)
	}
	assertExpectations(t, m)
}
func TestWithAgentTxUsesDatabaseClockAndRollsBack(t *testing.T) {
	r, m := newRepositoryTest(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT .* FROM `agents` WHERE tenant_id = .*id = .*FOR UPDATE").WithArgs("tenant", "agent").WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "presentation_json"}).AddRow("agent", "tenant", `{}`))
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	m.ExpectQuery("SELECT CAST").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now.Format("2006-01-02 15:04:05.999999")))
	m.ExpectRollback()
	err := r.WithAgentTx(context.Background(), "tenant", "agent", func(tx repo.Tx) error {
		got, err := tx.NowUTC()
		if err != nil || !got.Equal(now) {
			t.Fatalf("clock %v %v", got, err)
		}
		return ce.ErrConflict
	})
	if !errors.Is(err, ce.ErrConflict) {
		t.Fatal(err)
	}
	assertExpectations(t, m)
}
func TestSaveSessionRejectsExpiryRenewal(t *testing.T) {
	r, m := newRepositoryTest(t)
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	m.ExpectQuery("SELECT .* FROM `agent_training_sessions` WHERE .*FOR UPDATE").WithArgs("tenant", "agent", "session", "admin").WillReturnRows(sqlmock.NewRows([]string{"id", "status", "operation_json", "expires_at", "created_at", "row_version"}).AddRow("session", "active", `{}`, now.Add(time.Hour), now, 3))
	tx := &tx{db: r.provider.UseDB(context.Background()), tenant: "tenant", agent: "agent", ids: testIDs{}}
	err := tx.SaveSession(training.Session{ID: "session", TenantID: "tenant", AgentID: "agent", AdminUserID: "admin", ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now}, 3)
	if !errors.Is(err, ce.ErrConflict) {
		t.Fatalf("renewal: %v", err)
	}
	assertExpectations(t, m)
}
func TestTrainingPointerClearUsesExpectedSessionCAS(t *testing.T) {
	r, m := newRepositoryTest(t)
	m.ExpectExec("UPDATE `agents` SET .*active_training_session_id.*row_version.*WHERE .*tenant_id = .*id = .*row_version = .*active_training_session_id =").WithArgs(nil, "tenant", "agent", uint64(4), "session").WillReturnResult(sqlmock.NewResult(0, 0))
	tx := &tx{db: r.provider.UseDB(context.Background()), tenant: "tenant", agent: "agent"}
	if err := tx.SetTrainingPointer("session", nil, 4); !errors.Is(err, ce.ErrConflict) {
		t.Fatalf("CAS %v", err)
	}
	assertExpectations(t, m)
}
