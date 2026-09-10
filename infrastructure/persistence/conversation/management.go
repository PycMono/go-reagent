package conversation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

const (
	maxManagementPageSize = 100
	untitledConversation  = "Untitled Chat"
)

type conversationListRow struct {
	ID               string    `gorm:"column:id"`
	TenantID         string    `gorm:"column:tenant_id"`
	UserID           string    `gorm:"column:user_id"`
	ConversationID   string    `gorm:"column:conversation_id"`
	ConversationType string    `gorm:"column:conversation_type"`
	AgentID          string    `gorm:"column:agent_id"`
	AgentVersionID   string    `gorm:"column:agent_version_id"`
	FollowLatest     bool      `gorm:"column:follow_latest"`
	Name             string    `gorm:"column:name"`
	ProfileCode      string    `gorm:"column:profile_code"`
	Version          uint64    `gorm:"column:version"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
	MessageTotal     int64     `gorm:"column:message_total"`
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
	principal, scoped, err := managementPrincipal(ctx, query.UserID)
	if err != nil {
		return page, err
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
		Select("conversations.id, conversations.tenant_id, conversations.user_id, conversations.conversation_id, conversations.conversation_type, conversations.agent_id, conversations.agent_version_id, conversations.follow_latest, conversations.name, conversations.profile_code, conversations.version, conversations.created_at, conversations.updated_at, COUNT(messages.id) AS message_total").
		Joins("LEFT JOIN agent_messages AS messages ON messages.conversation_id = conversations.id").
		Where("conversations.user_id = ?", query.UserID)
	if scoped {
		db = db.Where("conversations.tenant_id = ? AND BINARY conversations.user_id = BINARY ? AND conversations.conversation_type = 'chat'", principal.TenantID, principal.UserID)
	}
	if query.Keyword != "" {
		db = db.Where("conversations.name LIKE ?", "%"+query.Keyword+"%")
	}
	if query.ProfileCode != "" {
		db = db.Where("conversations.profile_code = ?", query.ProfileCode)
	}
	if query.AgentID != "" {
		db = db.Where("conversations.agent_id = ?", strings.TrimSpace(query.AgentID))
	}
	if query.Cursor != nil {
		db = db.Where(
			"conversations.updated_at < ? OR (conversations.updated_at = ? AND conversations.id < ?)",
			query.Cursor.UpdatedAt, query.Cursor.UpdatedAt, query.Cursor.ID,
		)
	}
	err = db.
		Group("conversations.id, conversations.tenant_id, conversations.user_id, conversations.conversation_id, conversations.conversation_type, conversations.agent_id, conversations.agent_version_id, conversations.follow_latest, conversations.name, conversations.profile_code, conversations.version, conversations.created_at, conversations.updated_at").
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
				ID: row.ID, TenantID: row.TenantID, UserID: row.UserID, ConversationID: row.ConversationID,
				ConversationType: row.ConversationType, AgentID: row.AgentID, AgentVersionID: row.AgentVersionID, FollowLatest: row.FollowLatest,
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
	principal, scoped, err := managementPrincipal(ctx, query.UserID)
	if err != nil {
		return page, err
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
	if scoped {
		db = db.Where("conversations.tenant_id = ? AND BINARY conversations.user_id = BINARY ? AND BINARY conversations.conversation_id = BINARY ? AND conversations.conversation_type = 'chat'", principal.TenantID, principal.UserID, query.ConversationID)
	}
	if query.Cursor != nil {
		if query.Cursor.TurnVersion == 0 {
			return page, errors.New("mysql conversation management: invalid message cursor")
		}
		db = db.Where(
			"messages.turn_version < ? OR (messages.turn_version = ? AND messages.ordinal < ?)",
			query.Cursor.TurnVersion, query.Cursor.TurnVersion, query.Cursor.Ordinal,
		)
	}
	err = db.
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
	db := repo.provider.UseDB(ctx).Model(&conversationentity.Conversation{}).
		Where("user_id = ? AND conversation_id = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID))
	if principal, scoped, _ := managementPrincipal(ctx, userID); scoped {
		db = db.Where("tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat'", principal.TenantID, principal.UserID, strings.TrimSpace(conversationID))
	}
	result := db.
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
	db := repo.provider.UseDB(ctx).Model(&conversationentity.Conversation{}).
		Where("user_id = ? AND conversation_id = ? AND name = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID), untitledConversation)
	if principal, scoped, _ := managementPrincipal(ctx, userID); scoped {
		db = db.Where("tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat'", principal.TenantID, principal.UserID, strings.TrimSpace(conversationID))
	}
	result := db.
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
	db := repo.provider.UseDB(ctx).Where("user_id = ? AND conversation_id = ?", strings.TrimSpace(userID), strings.TrimSpace(conversationID))
	if principal, scoped, _ := managementPrincipal(ctx, userID); scoped {
		db = db.Where("tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat'", principal.TenantID, principal.UserID, strings.TrimSpace(conversationID))
	}
	result := db.
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
	if _, _, err := managementPrincipal(ctx, strings.TrimSpace(userID)); err != nil {
		return err
	}
	return nil
}

func managementPrincipal(ctx context.Context, userID string) (identity.Principal, bool, error) {
	principal, scoped := identity.FromContext(ctx)
	if scoped && principal.UserID != strings.TrimSpace(userID) {
		return identity.Principal{}, true, commonerrors.ErrNotFound
	}
	return principal, scoped, nil
}
