package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	agentprofileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

type idFake struct{ values []string }

func (f *idFake) NextID() string {
	value := f.values[0]
	f.values = f.values[1:]
	return value
}

type catalogFake struct {
	profiles    []agentprofileentity.Profile
	defaultCode string
}

func (f catalogFake) List() []agentprofileentity.Profile {
	return append([]agentprofileentity.Profile(nil), f.profiles...)
}

func (f catalogFake) Find(code string) (agentprofileentity.Profile, bool) {
	for _, profile := range f.profiles {
		if profile.Code == strings.TrimSpace(code) {
			return profile, true
		}
	}
	return agentprofileentity.Profile{}, false
}

func (f catalogFake) DefaultCode() string { return f.defaultCode }

func testCatalog() catalogFake {
	return catalogFake{defaultCode: "general", profiles: []agentprofileentity.Profile{
		{Code: "general", Name: "通用助手", Description: "日常问答", Icon: "message-circle", Selectable: true, Welcome: "今天想一起完成什么？"},
		{Code: "writing", Name: "写作助手", Description: "内容写作", Icon: "pen-line", Selectable: true, Welcome: "想写点什么？", Starters: []agentprofileentity.Starter{{Title: "写文案", Prompt: "帮我写："}}, Instructions: "你擅长写作。", Skills: []agentprofileentity.Skill{{Name: "content-writing", Description: "Write content <carefully>.", Location: "profiles/writing/skills/content-writing/SKILL.md"}}},
		{Code: "retired", Name: "旧助手", Description: "旧会话使用", Icon: "message-circle", Selectable: false, Welcome: "继续旧会话", Instructions: "旧助手规则。"},
	}}
}

type managementRepoFake struct {
	created      *conversationentity.Conversation
	foundValue   *conversationentity.Conversation
	found        bool
	findCalls    int
	listQuery    conversationrepo.ListQuery
	listPage     conversationrepo.ListPage
	messageQuery conversationrepo.MessageQuery
	messagePage  conversationrepo.MessagePage
	renameArgs   []string
	deleteArgs   []string
	operationErr error
}

func (f *managementRepoFake) Create(_ context.Context, value *conversationentity.Conversation) error {
	f.created = value
	return f.operationErr
}
func (f *managementRepoFake) FindByUserIDAndConversationID(context.Context, string, string) (*conversationentity.Conversation, bool, error) {
	f.findCalls++
	return f.foundValue, f.found, f.operationErr
}
func (f *managementRepoFake) ListByUserID(_ context.Context, query conversationrepo.ListQuery) (conversationrepo.ListPage, error) {
	f.listQuery = query
	return f.listPage, f.operationErr
}
func (f *managementRepoFake) ListMessages(_ context.Context, query conversationrepo.MessageQuery) (conversationrepo.MessagePage, error) {
	f.messageQuery = query
	return f.messagePage, f.operationErr
}
func (f *managementRepoFake) Rename(_ context.Context, userID, conversationID, name string) error {
	f.renameArgs = []string{userID, conversationID, name}
	return f.operationErr
}
func (f *managementRepoFake) RenameIfUntitled(context.Context, string, string, string) error {
	return f.operationErr
}
func (f *managementRepoFake) Delete(_ context.Context, userID, conversationID string) error {
	f.deleteArgs = []string{userID, conversationID}
	return f.operationErr
}

func TestServiceCreatesConversationWithDistinctIDs(t *testing.T) {
	repo := &managementRepoFake{}
	service := NewService(repo, &idFake{values: []string{"internal-1", "public-1"}}, nil, testCatalog())
	got, err := service.CreateConversation(context.Background(), "visitor-1", dto.CreateConversationDTO{ProfileCode: "writing"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.created.ID != "internal-1" || repo.created.ConversationID != "public-1" ||
		repo.created.UserID != "visitor-1" || repo.created.Name != UntitledChat || repo.created.ProfileCode != "writing" ||
		got.ID != "public-1" || got.Name != UntitledChat || got.ProfileCode != "writing" {
		t.Fatalf("created = %#v, response = %#v", repo.created, got)
	}
}

func TestServiceRejectsMissingUnknownAndUnselectableProfiles(t *testing.T) {
	service := NewService(&managementRepoFake{}, &idFake{}, nil, testCatalog())
	for _, code := range []string{"", "missing", "retired"} {
		_, err := service.CreateConversation(context.Background(), "visitor-1", dto.CreateConversationDTO{ProfileCode: code})
		if !errors.Is(err, commonerrors.ErrInvalidParam) {
			t.Errorf("CreateConversation(profile=%q) error = %v", code, err)
		}
	}
}

func TestServiceListsPublicAgentProfilesWithoutRuntimeContent(t *testing.T) {
	service := NewService(&managementRepoFake{}, &idFake{}, nil, testCatalog())
	got := service.ListAgentProfiles()
	if got.DefaultProfile != "general" || len(got.Items) != 3 || got.Items[1].Code != "writing" ||
		len(got.Items[1].Starters) != 1 || got.Items[1].Starters[0].Prompt != "帮我写：" {
		t.Fatalf("profiles = %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "你擅长写作") || strings.Contains(string(encoded), "content-writing") {
		t.Fatalf("public Profile response leaked runtime content: %s", encoded)
	}
}

func TestServiceListsConversationsAndBuildsNextCursor(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 5, 20, 0, time.UTC)
	repo := &managementRepoFake{listPage: conversationrepo.ListPage{
		Items: []*conversationentity.ListItem{{Conversation: &conversationentity.Conversation{
			ID: "internal-1", ConversationID: "chat-1", Name: "One", ProfileCode: "writing", UpdatedAt: now,
		}, MessageTotal: 4}},
		HasMore: true,
	}}
	service := NewService(repo, &idFake{}, nil, testCatalog())
	got, err := service.ListConversations(context.Background(), "visitor-1", dto.ListConversationsQuery{Keyword: " chat ", ProfileCode: "retired"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.listQuery.UserID != "visitor-1" || repo.listQuery.Keyword != "chat" || repo.listQuery.ProfileCode != "retired" || repo.listQuery.Limit != 20 ||
		len(got.Items) != 1 || got.Items[0].MessageTotal != 4 || got.Items[0].ProfileCode != "writing" || got.NextCursor == "" {
		t.Fatalf("query = %#v, page = %#v", repo.listQuery, got)
	}
	cursor, err := decodeConversationCursor(got.NextCursor)
	if err != nil || cursor.ID != "internal-1" || !cursor.UpdatedAt.Equal(now) {
		t.Fatalf("next cursor = %#v, %v", cursor, err)
	}
}

func TestServiceMapsDetailedMessages(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 5, 20, 0, time.UTC)
	repo := &managementRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat-1"}, messagePage: conversationrepo.MessagePage{
		Items: []*conversationentity.Message{{
			ID: "message-1", TurnVersion: 3, Ordinal: 2, RunID: "run-1", Role: conversationentity.RoleAssistant,
			Payload: conversationentity.MessagePayload{
				Content:    []conversationentity.ContentBlock{{Type: conversationentity.ContentTypeText, Text: "done"}},
				ToolCalls:  []conversationentity.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
				ToolCallID: "call-0", ToolName: "read", IsError: true,
			}, CreatedAt: now,
		}}, HasMore: true,
	}}
	service := NewService(repo, &idFake{}, nil, testCatalog())
	got, err := service.ListMessages(context.Background(), "visitor-1", "chat-1", dto.ListMessagesQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if repo.messageQuery.Limit != 20 || len(got.Items) != 1 || got.NextCursor == "" {
		t.Fatalf("query = %#v, page = %#v", repo.messageQuery, got)
	}
	message := got.Items[0]
	if message.ID != "message-1" || message.Role != "assistant" || message.Content[0].Text != "done" ||
		message.ToolCalls[0].ID != "call-1" || string(message.ToolCalls[0].Arguments) != `{"path":"README.md"}` ||
		message.ToolCallID != "call-0" || message.ToolName != "read" || !message.IsError || message.Ordinal != 2 {
		t.Fatalf("message = %#v", message)
	}
}

func TestServiceReturnsNotFoundForUnownedMessageHistory(t *testing.T) {
	repo := &managementRepoFake{}
	service := NewService(repo, &idFake{}, nil, testCatalog())
	_, err := service.ListMessages(context.Background(), "visitor-1", "chat-1", dto.ListMessagesQuery{})
	if !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("ListMessages() error = %v, want ErrNotFound", err)
	}
	if repo.findCalls != 1 || repo.messageQuery.ConversationID != "" {
		t.Fatalf("find calls/query = %d / %#v", repo.findCalls, repo.messageQuery)
	}
}

func TestServiceValidatesAndForwardsRenameDelete(t *testing.T) {
	repo := &managementRepoFake{}
	service := NewService(repo, &idFake{}, nil, testCatalog())
	if err := service.RenameConversation(context.Background(), "visitor-1", "chat-1", dto.RenameConversationDTO{Name: "  New name  "}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(repo.renameArgs, ",") != "visitor-1,chat-1,New name" {
		t.Fatalf("rename args = %#v", repo.renameArgs)
	}
	for _, name := range []string{"   ", strings.Repeat("界", 256)} {
		if err := service.RenameConversation(context.Background(), "visitor-1", "chat-1", dto.RenameConversationDTO{Name: name}); !errors.Is(err, commonerrors.ErrInvalidParam) {
			t.Errorf("RenameConversation(%q) error = %v", name, err)
		}
	}
	if err := service.DeleteConversation(context.Background(), "visitor-1", "chat-1"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(repo.deleteArgs, ",") != "visitor-1,chat-1" {
		t.Fatalf("delete args = %#v", repo.deleteArgs)
	}
}

func TestServiceRejectsInvalidIdentityAndCursor(t *testing.T) {
	service := NewService(&managementRepoFake{}, &idFake{}, nil, testCatalog())
	if _, err := service.CreateConversation(context.Background(), " ", dto.CreateConversationDTO{ProfileCode: "general"}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := service.ListConversations(context.Background(), "visitor", dto.ListConversationsQuery{Cursor: "bad"}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if _, err := service.ListMessages(context.Background(), "visitor", " ", dto.ListMessagesQuery{}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if _, err := service.ListConversations(context.Background(), "visitor", dto.ListConversationsQuery{ProfileCode: "missing"}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("ListConversations(unknown Profile) error = %v", err)
	}
}
