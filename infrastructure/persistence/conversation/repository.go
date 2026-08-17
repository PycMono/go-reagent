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
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/domain/repository"
	"gorm.io/gorm"
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

	createErr := repo.provider.UseDB(ctx).Create(conversation).Error
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
	err := repo.provider.UseDB(ctx).
		Where("conversation_id = ?", conversationID).
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
		if err := db.Select("id").
			Where("user_id = ? AND conversation_id = ?", userID, conversationID).
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
	err := repo.provider.UseDB(ctx).
		Where("user_id = ? AND conversation_id = ?", userID, conversationID).
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
