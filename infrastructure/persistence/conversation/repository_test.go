package conversation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRepositoryFindByUserIDAndConversationIDLoadsMetadataOnly(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	now := time.Now()
	expectOwnedConversation(mock, "user-1", "conversation-1").
		WillReturnRows(sqlmock.NewRows(conversationColumns).
			AddRow("conversation-pk-7", "user-1", "conversation-1", "writing", 3, now, now))

	repository := newTestRepository(provider)
	conversation, found, err := repository.FindByUserIDAndConversationID(context.Background(), "user-1", "conversation-1")
	if err != nil || !found {
		t.Fatalf("FindByUserIDAndConversationID() = %#v, %v, %v", conversation, found, err)
	}
	if conversation.ID != "conversation-pk-7" || conversation.UserID != "user-1" ||
		conversation.ConversationID != "conversation-1" || conversation.ProfileCode != "writing" || conversation.Version != 3 {
		t.Fatalf("conversation = %#v", conversation)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryListMessagesLoadsBoundedHistory(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	now := time.Now()
	expectMessages(mock, "conversation-pk-7").
		WillReturnRows(sqlmock.NewRows(messageColumns).
			AddRow("message-2", "conversation-pk-7", 1, 1, "run-1", "assistant", messageJSON("answer"), now).
			AddRow("message-1", "conversation-pk-7", 1, 0, "run-1", "user", messageJSON("question"), now))

	messages, err := newTestRepository(provider).ListMessagesByConversationID(context.Background(), "conversation-pk-7", 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []*conversationentity.Message{
		{ID: "message-1", ConversationID: "conversation-pk-7", TurnVersion: 1, Ordinal: 0, RunID: "run-1", Role: conversationentity.RoleUser, Payload: messagePayload("question"), CreatedAt: now},
		{ID: "message-2", ConversationID: "conversation-pk-7", TurnVersion: 1, Ordinal: 1, RunID: "run-1", Role: conversationentity.RoleAssistant, Payload: messagePayload("answer"), CreatedAt: now},
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages = %#v, want %#v", messages, want)
	}
}

func TestRepositoryFindReturnsNotFoundWithoutError(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	expectOwnedConversation(mock, "user", "missing").WillReturnError(gorm.ErrRecordNotFound)

	conversation, found, err := newTestRepository(provider).
		FindByUserIDAndConversationID(context.Background(), "user", "missing")
	if err != nil || found || conversation != nil {
		t.Fatalf("FindByUserIDAndConversationID() = %#v, %v, %v", conversation, found, err)
	}
}

func TestRepositoryCreateAssignsStringID(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	mock.ExpectExec("INSERT INTO .*agent_conversations").WillReturnResult(sqlmock.NewResult(0, 1))

	conversation := &conversationentity.Conversation{UserID: "user", ConversationID: "conversation"}
	if err := newTestRepository(provider).Create(context.Background(), conversation); err != nil {
		t.Fatal(err)
	}
	if conversation.ID != "generated-1" {
		t.Fatalf("created conversation ID = %q, want generated-1", conversation.ID)
	}
	if conversation.ProfileCode != "general" {
		t.Fatalf("created conversation ProfileCode = %q, want general", conversation.ProfileCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryAppendTurnUsesOwnedConversationAndExpectedVersion(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*id.*agent_conversations.*user_id.*conversation_id.*LIMIT").
		WithArgs("user", "conversation").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("conversation-pk-11"))
	expectVersionUpdate(mock, "conversation-pk-11", 7).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO .*agent_messages").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("INSERT INTO .*agent_model_invocations").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	messages := validMessages()
	invocations := []*conversationentity.ModelInvocation{{
		RunID: "run-8", Sequence: 1, Phase: conversationentity.InvocationPhaseAction,
		PlatformID: "test", Model: "test-model",
	}}
	err := newTestRepository(provider).AppendTurn(context.Background(), "user", "conversation", 7, messages, invocations)
	if err != nil {
		t.Fatal(err)
	}
	if messages[0].ID != "generated-1" || messages[1].ID != "generated-2" ||
		messages[0].ConversationID != "conversation-pk-11" || messages[1].TurnVersion != 8 {
		t.Fatalf("messages = %#v", messages)
	}
	if invocations[0].ID != "generated-3" || invocations[0].ConversationID != "conversation-pk-11" || invocations[0].TurnVersion != 8 {
		t.Fatalf("invocations = %#v", invocations)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryAppendTurnReturnsCommonConflict(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*id.*agent_conversations.*user_id.*conversation_id.*LIMIT").
		WithArgs("user", "conversation").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("conversation-pk-11"))
	expectVersionUpdate(mock, "conversation-pk-11", 7).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := newTestRepository(provider).AppendTurn(context.Background(), "user", "conversation", 7, validMessages(), nil)
	if !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("AppendTurn() error = %v, want ErrConflict", err)
	}
}

func TestRepositoryValidatesExplicitIdentityAndEntities(t *testing.T) {
	provider, _, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	repository := newTestRepository(provider)
	if _, _, err := repository.FindByUserIDAndConversationID(context.Background(), "", "conversation"); err == nil {
		t.Fatal("empty user ID accepted")
	}
	if err := repository.Create(context.Background(), nil); err == nil {
		t.Fatal("nil conversation accepted")
	}
	if err := repository.AppendTurn(context.Background(), "user", "conversation", 0, nil, nil); err == nil {
		t.Fatal("empty messages accepted")
	}
}

type contextDBProvider struct {
	db  *gorm.DB
	key struct{}
}

func (provider *contextDBProvider) UseDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(provider.key).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return provider.db.WithContext(ctx)
}

func (provider *contextDBProvider) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return provider.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return callback(context.WithValue(ctx, provider.key, tx))
	})
}

func (provider *contextDBProvider) IsInTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(provider.key).(*gorm.DB)
	return ok
}

func (provider *contextDBProvider) FindDB4TransContext(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(provider.key).(*gorm.DB)
	return tx
}

type sequenceIDService struct{ next int }

func (service *sequenceIDService) NextID() string {
	service.next++
	return "generated-" + string(rune('0'+service.next))
}

func newTestRepository(provider *contextDBProvider) *Repo {
	return NewConversationRepo(provider, provider, &sequenceIDService{})
}

func newRepositoryTestProvider(t *testing.T) (*contextDBProvider, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn: sqlDB, SkipInitializeWithVersion: true,
	}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	return &contextDBProvider{db: db}, mock, func() { _ = sqlDB.Close() }
}

var (
	conversationColumns = []string{"id", "user_id", "conversation_id", "profile_code", "version", "created_at", "updated_at"}
	messageColumns      = []string{"id", "conversation_id", "turn_version", "ordinal", "run_id", "role", "payload", "created_at"}
)

func expectOwnedConversation(mock sqlmock.Sqlmock, userID, conversationID string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT .*agent_conversations.*user_id.*conversation_id.*LIMIT").WithArgs(userID, conversationID)
}

func expectMessages(mock sqlmock.Sqlmock, conversationID any) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT .*agent_messages.*conversation_id.*turn_version DESC, ordinal DESC.*LIMIT").WithArgs(conversationID)
}

func expectVersionUpdate(mock sqlmock.Sqlmock, conversationID, version any) *sqlmock.ExpectedExec {
	return mock.ExpectExec("UPDATE .*agent_conversations.*SET .*version.*WHERE id = .*version =").
		WithArgs(sqlmock.AnyArg(), conversationID, version)
}

func validMessages() []*conversationentity.Message {
	return []*conversationentity.Message{
		{RunID: "run-8", Role: conversationentity.RoleUser, Payload: messagePayload("question")},
		{RunID: "run-8", Role: conversationentity.RoleAssistant, Payload: messagePayload("answer")},
	}
}

func messagePayload(text string) conversationentity.MessagePayload {
	return conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{Type: conversationentity.ContentTypeText, Text: text}}}
}

func messageJSON(text string) string {
	return `{"content":[{"type":"text","text":"` + text + `"}]}`
}

var _ sqlsdk.Provider = (*contextDBProvider)(nil)
var _ transaction.Manager = (*contextDBProvider)(nil)
