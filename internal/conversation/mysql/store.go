package mysql

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/internal/conversation"
	"github.com/PycMono/go-reagent/internal/schema"
	"gorm.io/gorm"
)

type DBProvider interface {
	UseDB(context.Context) *gorm.DB
}

type TransactionManager interface {
	Transaction(context.Context, func(context.Context) error) error
}

type Store struct {
	provider     DBProvider
	transactions TransactionManager
}

func NewStore(provider DBProvider, transactions TransactionManager) conversation.Store {
	return &Store{provider: provider, transactions: transactions}
}

func (s *Store) LoadOrCreate(ctx context.Context, key conversation.Key, limit int) (conversation.Snapshot, error) {
	if ctx == nil {
		return conversation.Snapshot{}, errors.New("mysql conversation: context is required")
	}
	if s == nil || s.provider == nil || s.transactions == nil {
		return conversation.Snapshot{}, errors.New("mysql conversation: database provider and transaction manager are required")
	}
	if err := ctx.Err(); err != nil {
		return conversation.Snapshot{}, fmt.Errorf("mysql conversation: load canceled: %w", err)
	}
	key.UserID = strings.TrimSpace(key.UserID)
	key.ConversationID = strings.TrimSpace(key.ConversationID)
	if key.UserID == "" {
		return conversation.Snapshot{}, errors.New("mysql conversation: user ID is required")
	}
	if key.ConversationID == "" {
		return conversation.Snapshot{}, errors.New("mysql conversation: conversation ID is required")
	}
	if limit < 1 {
		return conversation.Snapshot{}, errors.New("mysql conversation: history limit must be positive")
	}

	row, err := s.loadOwnedConversation(ctx, key)
	if err == nil {
		return s.loadSnapshot(ctx, row, limit)
	}
	if !errors.Is(err, conversation.ErrNotFound) {
		return conversation.Snapshot{}, err
	}

	created := conversationRow{UserID: key.UserID, ConversationID: key.ConversationID}
	createErr := s.provider.UseDB(ctx).Create(&created).Error
	if createErr == nil {
		return conversation.Snapshot{ConversationPK: created.ID, Version: created.Version}, nil
	}

	winner, reloadErr := s.loadOwnedConversation(ctx, key)
	if reloadErr != nil {
		return conversation.Snapshot{}, errors.Join(
			createErr,
			fmt.Errorf("mysql conversation: reload after create failure: %w", reloadErr),
		)
	}
	return s.loadSnapshot(ctx, winner, limit)
}

func (s *Store) loadOwnedConversation(ctx context.Context, key conversation.Key) (conversationRow, error) {
	var row conversationRow
	err := s.provider.UseDB(ctx).
		Where("user_id = ? AND conversation_id = ?", key.UserID, key.ConversationID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversationRow{}, fmt.Errorf("mysql conversation: %w", conversation.ErrNotFound)
	}
	if err != nil {
		return conversationRow{}, fmt.Errorf("mysql conversation: load owned conversation: %w", err)
	}
	return row, nil
}

func (s *Store) loadSnapshot(ctx context.Context, row conversationRow, limit int) (conversation.Snapshot, error) {
	var rows []messageRow
	err := s.provider.UseDB(ctx).
		Where("conversation_pk = ?", row.ID).
		Order("turn_version DESC, ordinal DESC").
		Limit(limit + 1).
		Find(&rows).Error
	if err != nil {
		return conversation.Snapshot{}, fmt.Errorf("mysql conversation: load messages: %w", err)
	}

	window := safeWindow(rows, limit)
	messages := make([]schema.Message, 0, len(window))
	for index := range window {
		message, err := decodeMessage(window[index])
		if err != nil {
			return conversation.Snapshot{}, fmt.Errorf("mysql conversation: decode message %d: %w", window[index].ID, err)
		}
		messages = append(messages, message)
	}
	return conversation.Snapshot{
		ConversationPK: row.ID,
		Version:        row.Version,
		Messages:       messages,
	}, nil
}

func (s *Store) AppendTurn(ctx context.Context, request conversation.AppendRequest) error {
	if ctx == nil {
		return errors.New("mysql conversation: context is required")
	}
	if s == nil || s.provider == nil || s.transactions == nil {
		return errors.New("mysql conversation: database provider and transaction manager are required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mysql conversation: append canceled: %w", err)
	}
	if request.ConversationPK == 0 {
		return errors.New("mysql conversation: conversation primary key is required")
	}
	if request.ExpectedVersion == math.MaxUint64 {
		return errors.New("mysql conversation: expected version is too large")
	}
	if len(request.Messages) == 0 {
		return errors.New("mysql conversation: append messages are required")
	}

	turnVersion := request.ExpectedVersion + 1
	rows := make([]messageRow, len(request.Messages))
	var runID *string
	if request.RunID != "" {
		value := request.RunID
		runID = &value
	}
	for index := range request.Messages {
		row, err := encodeMessage(request.Messages[index])
		if err != nil {
			return fmt.Errorf("mysql conversation: encode appended message %d: %w", index, err)
		}
		row.ConversationPK = request.ConversationPK
		row.TurnVersion = turnVersion
		row.Ordinal = uint32(index)
		row.RunID = runID
		rows[index] = row
	}

	err := s.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := s.provider.UseDB(txCtx)
		result := db.Model(&conversationRow{}).
			Where("id = ? AND version = ?", request.ConversationPK, request.ExpectedVersion).
			Updates(map[string]any{
				"version":    gorm.Expr("version + 1"),
				"updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return conversation.ErrConflict
		}
		if err := db.Create(&rows).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mysql conversation: append transaction: %w", err)
	}
	return nil
}
