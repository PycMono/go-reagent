package conversation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/domain/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repo is the MySQL implementation of IConversationRepository.
type Repo struct {
	provider     sqlsdk.Provider
	transactions transaction.Manager
	idService    repository.IIDService
}

func NewConversationRepo(
	provider sqlsdk.Provider,
	transactions transaction.Manager,
	idService repository.IIDService,
) *Repo {
	return &Repo{provider: provider, transactions: transactions, idService: idService}
}

func (repo *Repo) FindByUserIDAndConversationID(
	ctx context.Context,
	userID string,
	conversationID string,
) (*conversationentity.Conversation, bool, error) {
	if err := repo.validateContext(ctx); err != nil {
		return nil, false, err
	}
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	if userID == "" {
		return nil, false, errors.New("mysql conversation: user ID is required")
	}
	if conversationID == "" {
		return nil, false, errors.New("mysql conversation: conversation ID is required")
	}

	conversation, found, err := repo.findOwnedConversation(ctx, userID, conversationID)
	if err != nil || !found {
		return nil, found, err
	}
	return &conversation, true, nil
}

func (repo *Repo) Create(ctx context.Context, conversation *conversationentity.Conversation) error {
	if err := repo.validateContext(ctx); err != nil {
		return err
	}
	if conversation == nil {
		return errors.New("mysql conversation: conversation is required")
	}
	if principal, ok := identity.FromContext(ctx); ok {
		if principal.UserID != strings.TrimSpace(conversation.UserID) {
			return commonerrors.ErrNotFound
		}
		return repo.CreateBound(ctx, conversation)
	}
	conversation.UserID = strings.TrimSpace(conversation.UserID)
	conversation.ConversationID = strings.TrimSpace(conversation.ConversationID)
	conversation.ProfileCode = strings.TrimSpace(conversation.ProfileCode)
	if conversation.UserID == "" {
		return errors.New("mysql conversation: user ID is required")
	}
	if conversation.ConversationID == "" {
		return errors.New("mysql conversation: conversation ID is required")
	}
	if conversation.ProfileCode == "" {
		conversation.ProfileCode = "general"
	}
	if conversation.ID == "" {
		conversation.ID = repo.idService.NextID()
	}

	createErr := repo.provider.UseDB(ctx).
		Omit("TenantID", "ConversationType", "AgentID", "AgentVersionID", "FollowLatest").
		Create(conversation).Error
	if createErr == nil {
		return nil
	}
	existing, found, reloadErr := repo.findOwnedConversation(ctx, conversation.UserID, conversation.ConversationID)
	if reloadErr != nil || !found {
		return errors.Join(createErr, fmt.Errorf("mysql conversation: reload after create failure: %w", reloadErr))
	}
	*conversation = existing
	return nil
}

func (repo *Repo) CreateBound(ctx context.Context, conversation *conversationentity.Conversation) error {
	if err := repo.validateContext(ctx); err != nil {
		return err
	}
	if conversation == nil {
		return errors.New("mysql conversation: conversation is required")
	}
	principal, err := identity.Require(ctx)
	if err != nil {
		return err
	}
	conversation.UserID = strings.TrimSpace(conversation.UserID)
	conversation.ConversationID = strings.TrimSpace(conversation.ConversationID)
	conversation.AgentID = strings.TrimSpace(conversation.AgentID)
	conversation.AgentVersionID = strings.TrimSpace(conversation.AgentVersionID)
	if conversation.UserID != principal.UserID || conversation.TenantID != principal.TenantID {
		return commonerrors.ErrNotFound
	}
	if conversation.ConversationID == "" || conversation.AgentID == "" || conversation.AgentVersionID == "" {
		return errors.New("mysql conversation: bound conversation, agent, and version IDs are required")
	}
	if conversation.ID == "" {
		conversation.ID = repo.idService.NextID()
	}
	if conversation.ProfileCode == "" {
		conversation.ProfileCode = "general"
	}
	conversation.ConversationType = "chat"
	return repo.transactions.Transaction(ctx, func(txCtx context.Context) error {
		var owner struct {
			ID              string
			ActiveVersionID *string
			Status          string
		}
		db := repo.provider.UseDB(txCtx)
		if err := db.Table("agents").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, active_version_id, status").
			Where("tenant_id = ? AND id = ?", principal.TenantID, conversation.AgentID).
			Take(&owner).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonerrors.ErrNotFound
			}
			return err
		}
		if owner.Status != "enabled" || owner.ActiveVersionID == nil || *owner.ActiveVersionID != conversation.AgentVersionID {
			return commonerrors.ErrConflict
		}
		if err := db.Create(conversation).Error; err != nil {
			return fmt.Errorf("insert bound conversation: %w", err)
		}
		return nil
	})
}

func (repo *Repo) CommitAgentVersion(ctx context.Context, userID, conversationID, expectedAgentVersionID, targetVersionID string) error {
	if err := repo.validateContext(ctx); err != nil {
		return err
	}
	principal, err := identity.Require(ctx)
	if err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	if principal.UserID != userID {
		return commonerrors.ErrNotFound
	}
	current, found, err := repo.findOwnedConversation(ctx, userID, conversationID)
	if err != nil {
		return err
	}
	if !found {
		return commonerrors.ErrNotFound
	}
	return repo.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := repo.provider.UseDB(txCtx)
		var owner struct {
			ActiveVersionID *string
			Status          string
		}
		if err := db.Table("agents").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("active_version_id, status").
			Where("tenant_id = ? AND id = ?", principal.TenantID, current.AgentID).
			Take(&owner).Error; err != nil {
			return commonerrors.ErrNotFound
		}
		if owner.Status != "enabled" {
			return commonerrors.ErrConflict
		}
		var locked conversationentity.Conversation
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat' AND agent_id = ? AND agent_version_id = ?", current.ID, principal.TenantID, userID, conversationID, current.AgentID, expectedAgentVersionID).
			Take(&locked).Error; err != nil {
			return commonerrors.ErrConflict
		}
		if !locked.FollowLatest {
			return nil
		}
		if owner.ActiveVersionID == nil || *owner.ActiveVersionID != targetVersionID {
			return commonerrors.ErrConflict
		}
		result := db.Model(&conversationentity.Conversation{}).
			Where("id = ? AND tenant_id = ? AND agent_version_id = ?", locked.ID, principal.TenantID, expectedAgentVersionID).
			Update("agent_version_id", targetVersionID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonerrors.ErrConflict
		}
		return nil
	})
}

func (repo *Repo) ListMessagesByConversationID(
	ctx context.Context,
	conversationID string,
	messageLimit int,
) ([]*conversationentity.Message, error) {
	if err := repo.validateContext(ctx); err != nil {
		return nil, err
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, errors.New("mysql conversation: conversation ID is required")
	}
	if messageLimit < 1 {
		return nil, errors.New("mysql conversation: history limit must be positive")
	}

	var messages []*conversationentity.Message
	db := repo.provider.UseDB(ctx)
	if principal, ok := identity.FromContext(ctx); ok {
		db = db.Table("agent_messages AS messages").Select("messages.*").
			Joins("JOIN agent_conversations AS conversations ON conversations.id = messages.conversation_id").
			Where("messages.conversation_id = ? AND conversations.tenant_id = ? AND BINARY conversations.user_id = BINARY ? AND conversations.conversation_type = 'chat'", conversationID, principal.TenantID, principal.UserID)
	} else {
		db = db.Where("conversation_id = ?", conversationID)
	}
	err := db.
		Order("turn_version DESC, ordinal DESC").
		Limit(messageLimit).
		Find(&messages).Error
	if err != nil {
		return nil, commonerrors.ErrInternal.Wrap(fmt.Errorf("mysql conversation: load messages: %w", err))
	}
	slices.Reverse(messages)
	return messages, nil
}

func (repo *Repo) AppendTurn(
	ctx context.Context,
	userID string,
	conversationID string,
	expectedVersion uint64,
	messages []*conversationentity.Message,
	invocations []*conversationentity.ModelInvocation,
) error {
	if err := repo.validateContext(ctx); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	if userID == "" {
		return errors.New("mysql conversation: user ID is required")
	}
	if principal, ok := identity.FromContext(ctx); ok && principal.UserID != userID {
		return commonerrors.ErrNotFound
	}
	if conversationID == "" {
		return errors.New("mysql conversation: conversation ID is required")
	}
	if expectedVersion == math.MaxUint64 {
		return errors.New("mysql conversation: expected version is too large")
	}
	if len(messages) == 0 {
		return errors.New("mysql conversation: append messages are required")
	}

	turnVersion := expectedVersion + 1
	for index, message := range messages {
		if message.ID == "" {
			message.ID = repo.idService.NextID()
		}
		message.TurnVersion = turnVersion
		message.Ordinal = uint32(index)
	}
	for _, invocation := range invocations {
		if invocation.ID == "" {
			invocation.ID = repo.idService.NextID()
		}
		invocation.TurnVersion = turnVersion
	}

	err := repo.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := repo.provider.UseDB(txCtx)
		var conversation conversationentity.Conversation
		lookup := db.Select("id")
		if principal, ok := identity.FromContext(txCtx); ok {
			lookup = lookup.Where("tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat'", principal.TenantID, userID, conversationID)
		} else {
			lookup = lookup.Where("user_id = ? AND conversation_id = ?", userID, conversationID)
		}
		if err := lookup.
			First(&conversation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonerrors.ErrConflict
			}
			return err
		}
		result := db.Model(&conversationentity.Conversation{}).
			Where("id = ? AND version = ?", conversation.ID, expectedVersion).
			Updates(map[string]any{
				"version":    gorm.Expr("version + 1"),
				"updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonerrors.ErrConflict
		}
		for _, message := range messages {
			message.ConversationID = conversation.ID
		}
		if err := db.Create(&messages).Error; err != nil {
			return err
		}
		if len(invocations) > 0 {
			for _, invocation := range invocations {
				invocation.ConversationID = conversation.ID
			}
			if err := db.Create(&invocations).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mysql conversation: append transaction: %w", err)
	}
	return nil
}

func (repo *Repo) findOwnedConversation(ctx context.Context, userID string, conversationID string) (conversationentity.Conversation, bool, error) {
	var conversation conversationentity.Conversation
	db := repo.provider.UseDB(ctx)
	if principal, ok := identity.FromContext(ctx); ok {
		if principal.UserID != userID {
			return conversationentity.Conversation{}, false, commonerrors.ErrNotFound
		}
		db = db.Where("tenant_id = ? AND BINARY user_id = BINARY ? AND BINARY conversation_id = BINARY ? AND conversation_type = 'chat'", principal.TenantID, userID, conversationID)
	} else {
		db = db.Where("user_id = ? AND conversation_id = ?", userID, conversationID)
	}
	err := db.
		First(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversationentity.Conversation{}, false, nil
	}
	if err != nil {
		return conversationentity.Conversation{}, false, fmt.Errorf("mysql conversation: find owned conversation: %w", err)
	}
	return conversation, true, nil
}

func (repo *Repo) validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("mysql conversation: context is required")
	}
	if repo == nil || repo.provider == nil || repo.transactions == nil || repo.idService == nil {
		return errors.New("mysql conversation: database provider, transaction manager, and ID service are required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mysql conversation: operation canceled: %w", err)
	}
	return nil
}
