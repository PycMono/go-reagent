package conversation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

const (
	maxManagementPageSize = 100
	untitledConversation  = "Untitled Chat"
)

type conversationListRow struct {
	ID             string    `gorm:"column:id"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Name           string    `gorm:"column:name"`
	ProfileCode    string    `gorm:"column:profile_code"`
	Version        uint64    `gorm:"column:version"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
	MessageTotal   int64     `gorm:"column:message_total"`
}

func (repo *Repo) ListByUserID(ctx context.Context, query conversationrepo.ListQuery) (conversationrepo.ListPage, error) {
	page := conversationrepo.ListPage{}
	if err := repo.validateContext(ctx); err != nil {
		return page, err
	}
	query.UserID = strings.TrimSpace(query.UserID)
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.ProfileCode = strings.TrimSpace(query.ProfileCode)
	if query.UserID == "" {
		return page, errors.New("mysql conversation management: user ID is required")
	}
	if query.Limit < 1 || query.Limit > maxManagementPageSize {
		return page, errors.New("mysql conversation management: limit must be between 1 and 100")
	}
	if query.Cursor != nil && (query.Cursor.ID == "" || query.Cursor.UpdatedAt.IsZero()) {
		return page, errors.New("mysql conversation management: invalid conversation cursor")
	}

	var rows []conversationListRow
	db := repo.provider.UseDB(ctx).
		Table("agent_conversations AS conversations").
		Select("conversations.id, conversations.user_id, conversations.conversation_id, conversations.name, conversations.profile_code, conversations.version, conversations.created_at, conversations.updated_at, COUNT(messages.id) AS message_total").
		Joins("LEFT JOIN agent_messages AS messages ON messages.conversation_id = conversations.id").
		Where("conversations.user_id = ?", query.UserID)
	if query.Keyword != "" {
		db = db.Where("conversations.name LIKE ?", "%"+query.Keyword+"%")
	}
	if query.ProfileCode != "" {
		db = db.Where("conversations.profile_code = ?", query.ProfileCode)
	}
	if query.Cursor != nil {
		db = db.Where(
			"conversations.updated_at < ? OR (conversations.updated_at = ? AND conversations.id < ?)",
			query.Cursor.UpdatedAt, query.Cursor.UpdatedAt, query.Cursor.ID,
		)
	}
	err := db.
		Group("conversations.id, conversations.user_id, conversations.conversation_id, conversations.name, conversations.profile_code, conversations.version, conversations.created_at, conversations.updated_at").
		Order("conversations.updated_at DESC, conversations.id DESC").
		Limit(query.Limit + 1).
		Scan(&rows).Error
	if err != nil {
		return page, commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation management: list conversations: %w", err))
	}
	if len(rows) > query.Limit {
		page.HasMore = true
		rows = rows[:query.Limit]
	}
	page.Items = make([]*conversationentity.ListItem, len(rows))
	for index := range rows {
		row := rows[index]
		page.Items[index] = &conversationentity.ListItem{
			Conversation: &conversationentity.Conversation{
				ID: row.ID, UserID: row.UserID, ConversationID: row.ConversationID,
				Name: row.Name, ProfileCode: row.ProfileCode, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			},
			MessageTotal: row.MessageTotal,
		}
	}
	return page, nil
}

func (repo *Repo) ListMessages(ctx context.Context, query conversationrepo.MessageQuery) (conversationrepo.MessagePage, error) {
	page := conversationrepo.MessagePage{}
	if err := repo.validateContext(ctx); err != nil {
		return page, err
	}
	query.UserID = strings.TrimSpace(query.UserID)
	query.ConversationID = strings.TrimSpace(query.ConversationID)
	if query.UserID == "" {
		return page, errors.New("mysql conversation management: user ID is required")
	}
	if query.ConversationID == "" {
		return page, errors.New("mysql conversation management: conversation ID is required")
	}
	if query.Limit < 1 || query.Limit > maxManagementPageSize {
		return page, errors.New("mysql conversation management: limit must be between 1 and 100")
	}

	var messages []*conversationentity.Message
	db := repo.provider.UseDB(ctx).
		Table("agent_messages AS messages").
		Select("messages.*").
		Joins("JOIN agent_conversations AS conversations ON conversations.id = messages.conversation_id").
		Where("conversations.user_id = ? AND conversations.conversation_id = ?", query.UserID, query.ConversationID)
	if query.Cursor != nil {
		if query.Cursor.TurnVersion == 0 {
			return page, errors.New("mysql conversation management: invalid message cursor")
		}
		db = db.Where(
			"messages.turn_version < ? OR (messages.turn_version = ? AND messages.ordinal < ?)",
			query.Cursor.TurnVersion, query.Cursor.TurnVersion, query.Cursor.Ordinal,
		)
	}
	err := db.
		Order("messages.turn_version DESC, messages.ordinal DESC").
		Limit(query.Limit + 1).
		Find(&messages).Error
	if err != nil {
		return page, commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation management: list messages: %w", err))
	}
	if len(messages) > query.Limit {
		page.HasMore = true
		messages = messages[:query.Limit]
	}
	slices.Reverse(messages)
	page.Items = messages
	return page, nil
}

func (repo *Repo) Rename(ctx context.Context, userID, conversationID, name string) error {
	if err := repo.validateManagementIdentity(ctx, userID, conversationID); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("mysql conversation management: name is required")
	}
	result := repo.provider.UseDB(ctx).
		Model(&conversationentity.Conversation{}).
		Where("user_id = ? AND conversation_id = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID)).
		Update("name", name)
	if result.Error != nil {
		return commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation management: rename conversation: %w", result.Error))
	}
	if result.RowsAffected != 1 {
		return commonerrors.ErrNotFound
	}
	return nil
}

func (repo *Repo) RenameIfUntitled(ctx context.Context, userID, conversationID, name string) error {
	if err := repo.validateManagementIdentity(ctx, userID, conversationID); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("mysql conversation management: name is required")
	}
	result := repo.provider.UseDB(ctx).
		Model(&conversationentity.Conversation{}).
		Where("user_id = ? AND conversation_id = ? AND name = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID), untitledConversation).
		Update("name", name)
	if result.Error != nil {
		return commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation management: set initial conversation name: %w", result.Error))
	}
	return nil
}

func (repo *Repo) Delete(ctx context.Context, userID, conversationID string) error {
	if err := repo.validateManagementIdentity(ctx, userID, conversationID); err != nil {
		return err
	}
	result := repo.provider.UseDB(ctx).
		Where("user_id = ? AND conversation_id = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID)).
		Delete(&conversationentity.Conversation{})
	if result.Error != nil {
		return commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation management: delete conversation: %w", result.Error))
	}
	if result.RowsAffected != 1 {
		return commonerrors.ErrNotFound
	}
	return nil
}

func (repo *Repo) validateManagementIdentity(ctx context.Context, userID, conversationID string) error {
	if err := repo.validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("mysql conversation management: user ID is required")
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("mysql conversation management: conversation ID is required")
	}
	return nil
}
