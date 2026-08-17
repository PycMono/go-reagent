package conversation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

func TestManagementRepoListsOwnedConversationsWithMessageTotals(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	now := time.Date(2026, 8, 14, 10, 5, 20, 0, time.UTC)
	mock.ExpectQuery("SELECT .*message_total.*agent_conversations AS conversations.*LEFT JOIN agent_messages AS messages.*conversations.user_id.*conversations.name LIKE.*GROUP BY.*ORDER BY conversations.updated_at DESC, conversations.id DESC.*LIMIT").
		WithArgs("visitor-1", "%chat%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "conversation_id", "name", "profile_code", "version", "created_at", "updated_at", "message_total",
		}).AddRow("internal-1", "visitor-1", "chat-1", "Chat one", "writing", 2, now, now, 4))

	page, err := newTestRepository(provider).ListByUserID(context.Background(), conversationrepo.ListQuery{
		UserID: "visitor-1", Keyword: " chat ", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}
	item := page.Items[0]
	if item.Conversation.ID != "internal-1" || item.Conversation.Name != "Chat one" || item.Conversation.ProfileCode != "writing" || item.MessageTotal != 4 {
		t.Fatalf("item = %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestManagementRepoListUsesExtraRowForHasMore(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	now := time.Date(2026, 8, 14, 10, 5, 20, 0, time.UTC)
	mock.ExpectQuery("SELECT .*message_total.*agent_conversations AS conversations.*ORDER BY conversations.updated_at DESC, conversations.id DESC.*LIMIT").
		WithArgs("visitor-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "conversation_id", "name", "profile_code", "version", "created_at", "updated_at", "message_total",
		}).
			AddRow("internal-3", "visitor-1", "chat-3", "Three", "general", 0, now, now, 0).
			AddRow("internal-2", "visitor-1", "chat-2", "Two", "general", 0, now, now, 0).
			AddRow("internal-1", "visitor-1", "chat-1", "One", "general", 0, now, now, 0))

	page, err := newTestRepository(provider).ListByUserID(context.Background(), conversationrepo.ListQuery{
		UserID: "visitor-1", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Items) != 2 || page.Items[1].Conversation.ConversationID != "chat-2" {
		t.Fatalf("page = %#v", page)
	}
}

func TestManagementRepoFiltersConversationsByProfile(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*profile_code.*agent_conversations AS conversations.*conversations.user_id.*conversations.profile_code.*ORDER BY conversations.updated_at DESC, conversations.id DESC.*LIMIT").
		WithArgs("visitor-1", "writing").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "conversation_id", "name", "profile_code", "version", "created_at", "updated_at", "message_total",
		}))

	page, err := newTestRepository(provider).ListByUserID(context.Background(), conversationrepo.ListQuery{
		UserID: "visitor-1", ProfileCode: " writing ", Limit: 20,
	})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("ListByUserID() page/error = %#v / %v", page, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestManagementRepoListsOwnedMessagesInChronologicalOrder(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	now := time.Date(2026, 8, 14, 10, 5, 20, 0, time.UTC)
	mock.ExpectQuery("SELECT messages.*agent_messages AS messages.*agent_conversations.*user_id.*conversation_id.*ORDER BY messages.turn_version DESC, messages.ordinal DESC.*LIMIT").
		WithArgs("visitor-1", "chat-1").
		WillReturnRows(sqlmock.NewRows(messageColumns).
			AddRow("message-2", "internal-1", 1, 1, "run-1", "assistant", messageJSON("answer"), now).
			AddRow("message-1", "internal-1", 1, 0, "run-1", "user", messageJSON("question"), now))

	page, err := newTestRepository(provider).ListMessages(context.Background(), conversationrepo.MessageQuery{
		UserID: "visitor-1", ConversationID: "chat-1", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []*conversationentity.Message{
		{ID: "message-1", ConversationID: "internal-1", TurnVersion: 1, Ordinal: 0, RunID: "run-1", Role: conversationentity.RoleUser, Payload: messagePayload("question"), CreatedAt: now},
		{ID: "message-2", ConversationID: "internal-1", TurnVersion: 1, Ordinal: 1, RunID: "run-1", Role: conversationentity.RoleAssistant, Payload: messagePayload("answer"), CreatedAt: now},
	}
	if page.HasMore || !reflect.DeepEqual(page.Items, want) {
		t.Fatalf("page = %#v, want items %#v", page, want)
	}
}

func TestManagementRepoRenameAndDeleteRequireOwnedRows(t *testing.T) {
	provider, mock, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	repo := newTestRepository(provider)

	mock.ExpectExec("UPDATE .*agent_conversations.*SET .*name.*WHERE user_id = .*conversation_id =").
		WithArgs("Renamed", sqlmock.AnyArg(), "visitor-1", "chat-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.Rename(context.Background(), "visitor-1", "chat-1", "Renamed"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("Rename() error = %v, want ErrNotFound", err)
	}

	mock.ExpectExec("DELETE FROM .*agent_conversations.*user_id = .*conversation_id =").
		WithArgs("visitor-1", "chat-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.Delete(context.Background(), "visitor-1", "chat-1"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestManagementRepoRejectsInvalidLimitsWithoutQuerying(t *testing.T) {
	provider, _, cleanup := newRepositoryTestProvider(t)
	defer cleanup()
	repo := newTestRepository(provider)
	if _, err := repo.ListByUserID(context.Background(), conversationrepo.ListQuery{UserID: "visitor", Limit: 0}); err == nil {
		t.Fatal("zero conversation limit accepted")
	}
	if _, err := repo.ListMessages(context.Background(), conversationrepo.MessageQuery{UserID: "visitor", ConversationID: "chat", Limit: 101}); err == nil {
		t.Fatal("message limit above 100 accepted")
	}
}
