package chat

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/conversation"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/domain/repository"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

const (
	UntitledChat = "Untitled Chat"
	defaultLimit = 20
	maxLimit     = 100
)

type Service struct {
	repository conversationrepo.IConversationManagementRepository
	ids        repository.IIDService
	runner     conversation.Runner

	activeMu sync.Mutex
	active   map[string]*activeRunEntry
}

func NewService(repository conversationrepo.IConversationManagementRepository, ids repository.IIDService, runner conversation.Runner) *Service {
	return &Service{repository: repository, ids: ids, runner: runner, active: make(map[string]*activeRunEntry)}
}

func (s *Service) CreateConversation(ctx context.Context, userID string) (*vo.ConversationVO, error) {
	if !validIdentity(userID) || s == nil || s.repository == nil || s.ids == nil {
		return nil, commonerrors.ErrInvalidParam
	}
	conversation := &conversationentity.Conversation{
		ID:             s.ids.NextID(),
		UserID:         strings.TrimSpace(userID),
		ConversationID: s.ids.NextID(),
		Name:           UntitledChat,
	}
	if err := s.repository.Create(ctx, conversation); err != nil {
		return nil, err
	}
	return conversationVO(conversation, 0), nil
}

func (s *Service) ListConversations(ctx context.Context, userID string, query dto.ListConversationsQuery) (*vo.ConversationPageVO, error) {
	if !validIdentity(userID) {
		return nil, commonerrors.ErrInvalidParam
	}
	limit, err := normalizeLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	var cursor *conversationrepo.ListCursor
	if query.Cursor != "" {
		cursor, err = decodeConversationCursor(query.Cursor)
		if err != nil {
			return nil, commonerrors.ErrInvalidParam.Wrap(err)
		}
	}
	page, err := s.repository.ListByUserID(ctx, conversationrepo.ListQuery{
		UserID: strings.TrimSpace(userID), Keyword: strings.TrimSpace(query.Keyword), Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := &vo.ConversationPageVO{Items: make([]*vo.ConversationVO, 0, len(page.Items))}
	for _, item := range page.Items {
		if item == nil || item.Conversation == nil {
			continue
		}
		result.Items = append(result.Items, conversationVO(item.Conversation, item.MessageTotal))
	}
	if page.HasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		if last != nil && last.Conversation != nil {
			result.NextCursor = encodeConversationCursor(conversationrepo.ListCursor{
				UpdatedAt: last.Conversation.UpdatedAt, ID: last.Conversation.ID,
			})
		}
	}
	return result, nil
}

func (s *Service) ListMessages(ctx context.Context, userID, conversationID string, query dto.ListMessagesQuery) (*vo.MessagePageVO, error) {
	if !validIdentity(userID) || !validIdentity(conversationID) {
		return nil, commonerrors.ErrInvalidParam
	}
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	_, found, err := s.repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, commonerrors.ErrNotFound
	}
	limit, err := normalizeLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	var cursor *conversationrepo.MessageCursor
	if query.Cursor != "" {
		cursor, err = decodeMessageCursor(query.Cursor)
		if err != nil {
			return nil, commonerrors.ErrInvalidParam.Wrap(err)
		}
	}
	page, err := s.repository.ListMessages(ctx, conversationrepo.MessageQuery{
		UserID: userID, ConversationID: conversationID, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := &vo.MessagePageVO{Items: make([]*vo.MessageVO, 0, len(page.Items))}
	for _, item := range page.Items {
		if item != nil {
			result.Items = append(result.Items, messageVO(item))
		}
	}
	if page.HasMore && len(page.Items) > 0 {
		oldest := page.Items[0]
		if oldest != nil {
			result.NextCursor = encodeMessageCursor(conversationrepo.MessageCursor{
				TurnVersion: oldest.TurnVersion, Ordinal: oldest.Ordinal,
			})
		}
	}
	return result, nil
}

func (s *Service) RenameConversation(ctx context.Context, userID, conversationID string, param dto.RenameConversationDTO) error {
	if !validIdentity(userID) || !validIdentity(conversationID) {
		return commonerrors.ErrInvalidParam
	}
	name := strings.TrimSpace(param.Name)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		return commonerrors.ErrInvalidParam
	}
	return s.repository.Rename(ctx, strings.TrimSpace(userID), strings.TrimSpace(conversationID), name)
}

func (s *Service) DeleteConversation(ctx context.Context, userID, conversationID string) error {
	if !validIdentity(userID) || !validIdentity(conversationID) {
		return commonerrors.ErrInvalidParam
	}
	return s.repository.Delete(ctx, strings.TrimSpace(userID), strings.TrimSpace(conversationID))
}

func normalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 1 || limit > maxLimit {
		return 0, commonerrors.ErrInvalidParam
	}
	return limit, nil
}

func validIdentity(value string) bool { return strings.TrimSpace(value) != "" }

func conversationVO(value *conversationentity.Conversation, total int64) *vo.ConversationVO {
	return &vo.ConversationVO{
		ID: value.ConversationID, Name: value.Name, MessageTotal: total,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func messageVO(value *conversationentity.Message) *vo.MessageVO {
	result := &vo.MessageVO{
		ID: value.ID, TurnVersion: value.TurnVersion, Ordinal: value.Ordinal, RunID: value.RunID,
		Role: string(value.Role), ToolCallID: value.Payload.ToolCallID, ToolName: value.Payload.ToolName,
		IsError: value.Payload.IsError, CreatedAt: value.CreatedAt,
		Content:   make([]vo.ContentBlockVO, 0, len(value.Payload.Content)),
		ToolCalls: make([]vo.ToolCallVO, 0, len(value.Payload.ToolCalls)),
	}
	for _, block := range value.Payload.Content {
		result.Content = append(result.Content, vo.ContentBlockVO{Type: string(block.Type), Text: block.Text})
	}
	for _, call := range value.Payload.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, vo.ToolCallVO{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return result
}
