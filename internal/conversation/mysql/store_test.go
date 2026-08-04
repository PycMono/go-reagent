package mysql

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/PycMono/go-reagent/internal/conversation"
	"github.com/PycMono/go-reagent/internal/schema"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStoreLoadOrCreateLoadsOwnedHistory(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	now := time.Now()
	mock.ExpectQuery("SELECT .*agent_conversations.*user_id.*conversation_id.*LIMIT").
		WithArgs("user-1", "conversation-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "conversation_id", "version", "created_at", "updated_at"}).
			AddRow(7, "user-1", "conversation-1", 3, now, now))
	mock.ExpectQuery("SELECT .*agent_messages.*conversation_pk.*turn_version DESC, ordinal DESC.*LIMIT").
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"id", "conversation_pk", "turn_version", "ordinal", "run_id", "role", "payload", "created_at"}).
			AddRow(2, 7, 1, 1, "run-1", "assistant", `{"role":"assistant","content":[{"type":"text","text":"answer"}]}`, now).
			AddRow(1, 7, 1, 0, "run-1", "user", `{"role":"user","content":[{"type":"text","text":"question"}]}`, now))

	store := NewStore(provider, provider)
	snapshot, err := store.LoadOrCreate(context.Background(), conversation.Key{
		UserID: "user-1", ConversationID: "conversation-1",
	}, 100)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	wantMessages := []schema.Message{
		{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("question")}},
		{Role: schema.RoleAssistant, Content: []schema.ContentBlock{schema.TextBlock("answer")}},
	}
	if snapshot.ConversationPK != 7 || snapshot.Version != 3 || !reflect.DeepEqual(snapshot.Messages, wantMessages) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnCommitsMessagesAndVersion(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*agent_conversations.*SET .*version.*WHERE id = .*version =").
		WithArgs(sqlmock.AnyArg(), 11, 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO .*agent_messages").
		WithArgs(
			11, 8, 0, "run-8", "user", sqlmock.AnyArg(), sqlmock.AnyArg(),
			11, 8, 1, "run-8", "assistant", sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 2))
	mock.ExpectCommit()

	err := NewStore(provider, provider).AppendTurn(context.Background(), conversation.AppendRequest{
		ConversationPK:  11,
		ExpectedVersion: 7,
		RunID:           "run-8",
		Messages: []schema.Message{
			{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("question")}},
			{Role: schema.RoleAssistant, Content: []schema.ContentBlock{schema.TextBlock("answer")}},
		},
	})
	if err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnReturnsConflictAndRollsBack(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	mock.ExpectBegin()
	expectVersionUpdate(mock, 11, 7).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := NewStore(provider, provider).AppendTurn(context.Background(), validAppendRequest())
	if !errors.Is(err, conversation.ErrConflict) {
		t.Fatalf("AppendTurn() error = %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnRollsBackWhenInsertFails(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	insertErr := errors.New("insert messages failed")
	mock.ExpectBegin()
	expectVersionUpdate(mock, 11, 7).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO .*agent_messages").
		WithArgs(
			11, 8, 0, "run-8", "user", sqlmock.AnyArg(), sqlmock.AnyArg(),
			11, 8, 1, "run-8", "assistant", sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnError(insertErr)
	mock.ExpectRollback()

	err := NewStore(provider, provider).AppendTurn(context.Background(), validAppendRequest())
	if !errors.Is(err, insertErr) {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnRollsBackWhenVersionUpdateFails(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	updateErr := errors.New("update failed")
	mock.ExpectBegin()
	expectVersionUpdate(mock, 11, 7).WillReturnError(updateErr)
	mock.ExpectRollback()

	err := NewStore(provider, provider).AppendTurn(context.Background(), validAppendRequest())
	if !errors.Is(err, updateErr) {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnRejectsInvalidRequestBeforeTransaction(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	tests := []struct {
		name   string
		mutate func(*conversation.AppendRequest)
	}{
		{name: "zero conversation primary key", mutate: func(r *conversation.AppendRequest) { r.ConversationPK = 0 }},
		{name: "empty messages", mutate: func(r *conversation.AppendRequest) { r.Messages = nil }},
		{name: "unknown role", mutate: func(r *conversation.AppendRequest) { r.Messages[1].Role = schema.RoleSystem }},
		{name: "invalid JSON", mutate: func(r *conversation.AppendRequest) {
			r.Messages[1].ToolCalls = []schema.ToolCall{{ID: "call", Name: "read", Arguments: []byte(`{"path":`)}}
		}},
	}
	store := NewStore(provider, provider)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validAppendRequest()
			tt.mutate(&request)
			if err := store.AppendTurn(context.Background(), request); err == nil {
				t.Fatal("AppendTurn() error = nil")
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendTurnPreservesTransactionManagerError(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	transactionErr := errors.New("transaction unavailable")
	store := NewStore(provider, transactionManagerFake{err: transactionErr})

	err := store.AppendTurn(context.Background(), validAppendRequest())
	if !errors.Is(err, transactionErr) {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadOrCreateCreatesMissingConversation(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	expectOwnedConversation(mock, "user", "conversation").WillReturnRows(sqlmock.NewRows(conversationColumns))
	mock.ExpectExec("INSERT INTO .*agent_conversations").
		WithArgs("user", "conversation", 0, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(9, 1))

	snapshot, err := NewStore(provider, provider).LoadOrCreate(context.Background(), conversation.Key{
		UserID: "user", ConversationID: "conversation",
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ConversationPK != 9 || snapshot.Version != 0 || len(snapshot.Messages) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadOrCreateAcceptsConcurrentCreateWinner(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	now := time.Now()
	expectOwnedConversation(mock, "user", "conversation").WillReturnRows(sqlmock.NewRows(conversationColumns))
	mock.ExpectExec("INSERT INTO .*agent_conversations").
		WithArgs("user", "conversation", 0, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("duplicate key"))
	expectOwnedConversation(mock, "user", "conversation").WillReturnRows(
		sqlmock.NewRows(conversationColumns).AddRow(12, "user", "conversation", 4, now, now),
	)
	expectMessages(mock, 12).WillReturnRows(sqlmock.NewRows(messageColumns))

	snapshot, err := NewStore(provider, provider).LoadOrCreate(context.Background(), conversation.Key{
		UserID: "user", ConversationID: "conversation",
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ConversationPK != 12 || snapshot.Version != 4 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadOrCreateJoinsCreateAndReloadErrors(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	createErr := errors.New("insert failed")
	reloadErr := errors.New("reload failed")
	expectOwnedConversation(mock, "user", "conversation").WillReturnRows(sqlmock.NewRows(conversationColumns))
	mock.ExpectExec("INSERT INTO .*agent_conversations").
		WithArgs("user", "conversation", 0, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(createErr)
	expectOwnedConversation(mock, "user", "conversation").WillReturnError(reloadErr)

	_, err := NewStore(provider, provider).LoadOrCreate(context.Background(), conversation.Key{
		UserID: "user", ConversationID: "conversation",
	}, 100)
	if !errors.Is(err, createErr) || !errors.Is(err, reloadErr) {
		t.Fatalf("LoadOrCreate() error = %v, want both errors", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadOrCreateScopesSameConversationIDByUser(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	now := time.Now()
	for index, userID := range []string{"user-1", "user-2"} {
		id := index + 1
		expectOwnedConversation(mock, userID, "shared").WillReturnRows(
			sqlmock.NewRows(conversationColumns).AddRow(id, userID, "shared", 0, now, now),
		)
		expectMessages(mock, id).WillReturnRows(sqlmock.NewRows(messageColumns))
	}
	store := NewStore(provider, provider)
	for _, userID := range []string{"user-1", "user-2"} {
		if _, err := store.LoadOrCreate(context.Background(), conversation.Key{UserID: userID, ConversationID: "shared"}, 100); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadWindowDropsIncompleteOldestTurn(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	now := time.Now()
	expectOwnedConversation(mock, "user", "conversation").WillReturnRows(
		sqlmock.NewRows(conversationColumns).AddRow(7, "user", "conversation", 2, now, now),
	)
	expectMessages(mock, 7).WillReturnRows(sqlmock.NewRows(messageColumns).
		AddRow(4, 7, 2, 1, "run-2", "assistant", messageJSON("assistant", "new answer"), now).
		AddRow(3, 7, 2, 0, "run-2", "user", messageJSON("user", "new question"), now).
		AddRow(2, 7, 1, 1, "run-1", "assistant", messageJSON("assistant", "old answer"), now).
		AddRow(1, 7, 1, 0, "run-1", "user", messageJSON("user", "old question"), now))

	snapshot, err := NewStore(provider, provider).LoadOrCreate(context.Background(), conversation.Key{
		UserID: "user", ConversationID: "conversation",
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].Role != schema.RoleUser || snapshot.Messages[1].Role != schema.RoleAssistant {
		t.Fatalf("Messages = %#v", snapshot.Messages)
	}
	if text, _ := schema.TextContent(snapshot.Messages[0].Content); text != "new question" {
		t.Fatalf("first message = %q", text)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoadWindowRejectsCorruptMessage(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		payload string
	}{
		{name: "malformed JSON", role: "user", payload: `{"role":`},
		{name: "role mismatch", role: "assistant", payload: messageJSON("user", "secret input")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, mock, cleanup := newStoreTestProvider(t)
			defer cleanup()
			now := time.Now()
			expectOwnedConversation(mock, "user", "conversation").WillReturnRows(
				sqlmock.NewRows(conversationColumns).AddRow(7, "user", "conversation", 1, now, now),
			)
			expectMessages(mock, 7).WillReturnRows(sqlmock.NewRows(messageColumns).
				AddRow(1, 7, 1, 0, "run", tt.role, tt.payload, now))

			_, err := NewStore(provider, provider).LoadOrCreate(context.Background(), conversation.Key{
				UserID: "user", ConversationID: "conversation",
			}, 100)
			if !errors.Is(err, conversation.ErrCorruptMessage) {
				t.Fatalf("LoadOrCreate() error = %v", err)
			}
			if strings.Contains(err.Error(), "secret input") {
				t.Fatalf("LoadOrCreate() error leaks payload: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreLoadOrCreateValidatesBeforeSQL(t *testing.T) {
	provider, mock, cleanup := newStoreTestProvider(t)
	defer cleanup()
	tests := []struct {
		name  string
		ctx   context.Context
		key   conversation.Key
		limit int
	}{
		{name: "nil context", key: conversation.Key{UserID: "user", ConversationID: "conversation"}, limit: 1},
		{name: "empty user", ctx: context.Background(), key: conversation.Key{ConversationID: "conversation"}, limit: 1},
		{name: "empty conversation", ctx: context.Background(), key: conversation.Key{UserID: "user"}, limit: 1},
		{name: "invalid limit", ctx: context.Background(), key: conversation.Key{UserID: "user", ConversationID: "conversation"}},
	}
	store := NewStore(provider, provider)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.LoadOrCreate(tt.ctx, tt.key, tt.limit); err == nil {
				t.Fatal("LoadOrCreate() error = nil")
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type contextDBProvider struct {
	db  *gorm.DB
	key struct{}
}

func (p *contextDBProvider) UseDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(p.key).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return p.db.WithContext(ctx)
}

func (p *contextDBProvider) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return callback(context.WithValue(ctx, p.key, tx))
	})
}

func newStoreTestProvider(t *testing.T) (*contextDBProvider, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	return &contextDBProvider{db: db}, mock, func() { _ = sqlDB.Close() }
}

var (
	conversationColumns = []string{"id", "user_id", "conversation_id", "version", "created_at", "updated_at"}
	messageColumns      = []string{"id", "conversation_pk", "turn_version", "ordinal", "run_id", "role", "payload", "created_at"}
)

func expectOwnedConversation(mock sqlmock.Sqlmock, userID, conversationID string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT .*agent_conversations.*user_id.*conversation_id.*LIMIT").
		WithArgs(userID, conversationID)
}

func expectMessages(mock sqlmock.Sqlmock, conversationPK any) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT .*agent_messages.*conversation_pk.*turn_version DESC, ordinal DESC.*LIMIT").
		WithArgs(conversationPK)
}

func expectVersionUpdate(mock sqlmock.Sqlmock, conversationPK, version any) *sqlmock.ExpectedExec {
	return mock.ExpectExec("UPDATE .*agent_conversations.*SET .*version.*WHERE id = .*version =").
		WithArgs(sqlmock.AnyArg(), conversationPK, version)
}

func validAppendRequest() conversation.AppendRequest {
	return conversation.AppendRequest{
		ConversationPK:  11,
		ExpectedVersion: 7,
		RunID:           "run-8",
		Messages: []schema.Message{
			{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("question")}},
			{Role: schema.RoleAssistant, Content: []schema.ContentBlock{schema.TextBlock("answer")}},
		},
	}
}

type transactionManagerFake struct {
	err error
}

func (f transactionManagerFake) Transaction(context.Context, func(context.Context) error) error {
	return f.err
}

func messageJSON(role, text string) string {
	return `{"role":"` + role + `","content":[{"type":"text","text":"` + text + `"}]}`
}

var _ interface {
	UseDB(context.Context) *gorm.DB
	Transaction(context.Context, func(context.Context) error) error
} = (*contextDBProvider)(nil)
