package agenttraining

import (
	"context"
	ce "github.com/PycMono/go-reagent/common/errors"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"slices"
)

func (r *Repository) HasRun(ctx context.Context, s repo.Scope, run string) (bool, error) {
	session, err := r.Find(ctx, s)
	if err != nil {
		return false, err
	}
	var count int64
	err = r.provider.UseDB(ctx).Model(&conversation.Message{}).Where("conversation_id = ? AND run_id = ? AND ordinal = 0", session.ConversationID, run).Count(&count).Error
	return count > 0, err
}
func (r *Repository) History(ctx context.Context, s repo.Scope, before uint64, limit int) ([]*conversation.Message, error) {
	session, err := r.Find(ctx, s)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	q := r.provider.UseDB(ctx).Where("conversation_id = ?", session.ConversationID)
	if before > 0 {
		q = q.Where("turn_version < ?", before)
	}
	var messages []*conversation.Message
	err = q.Order("turn_version DESC, ordinal DESC").Limit(limit).Find(&messages).Error
	slices.Reverse(messages)
	return messages, err
}
func (t *tx) turnSession(s repo.Scope, run string) (training.Session, conversation.Conversation, error) {
	session, err := t.SessionForUpdate(s)
	if err != nil {
		return session, conversation.Conversation{}, err
	}
	if run == "" || len(run) > 128 || session.Operation.ID != run || session.Operation.Kind != "run" || !session.Running() {
		return session, conversation.Conversation{}, ce.ErrConflict
	}
	var c conversation.Conversation
	err = t.db.Omit("ProfileCode").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND agent_id = ? AND user_id = ? AND conversation_type = 'training'", session.ConversationID, s.TenantID, s.AgentID, s.AdminUserID).Take(&c).Error
	return session, c, err
}
func (t *tx) BeginTurn(s repo.Scope, run string, input conversation.Message) (uint64, error) {
	_, c, err := t.turnSession(s, run)
	if err != nil {
		return 0, err
	}
	if c.Version == math.MaxUint64 || input.Role != conversation.RoleUser {
		return 0, ce.ErrInvalidParam
	}
	var count int64
	if err = t.db.Model(&conversation.Message{}).Where("conversation_id = ? AND run_id = ?", c.ID, run).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, ce.ErrConflict
	}
	input.ID = t.ids.NextID()
	input.ConversationID = c.ID
	input.RunID = run
	input.TurnVersion = c.Version + 1
	input.Ordinal = 0
	result := t.db.Model(&conversation.Conversation{}).Where("id = ? AND version = ?", c.ID, c.Version).Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("UTC_TIMESTAMP(6)")})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, ce.ErrConflict
	}
	if err = t.db.Create(&input).Error; err != nil {
		return 0, err
	}
	return input.TurnVersion, nil
}
func (t *tx) FinishTurn(s repo.Scope, run string, version uint64, outputs []*conversation.Message, invocations []*conversation.ModelInvocation) error {
	_, c, err := t.turnSession(s, run)
	if err != nil {
		return err
	}
	if c.Version != version {
		return ce.ErrConflict
	}
	var count int64
	if err = t.db.Model(&conversation.Message{}).Where("conversation_id = ? AND run_id = ? AND turn_version = ? AND ordinal = 0", c.ID, run, version).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ce.ErrConflict
	}
	if err = t.db.Model(&conversation.Message{}).Where("conversation_id = ? AND turn_version = ? AND ordinal > 0", c.ID, version).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ce.ErrConflict
	}
	for i, m := range outputs {
		if m == nil || m.Role == conversation.RoleUser {
			return ce.ErrInvalidParam
		}
		m.ID = t.ids.NextID()
		m.ConversationID = c.ID
		m.RunID = run
		m.TurnVersion = version
		m.Ordinal = uint32(i + 1)
	}
	for i, v := range invocations {
		if v == nil {
			return ce.ErrInvalidParam
		}
		v.ID = t.ids.NextID()
		v.ConversationID = c.ID
		v.RunID = run
		v.TurnVersion = version
		v.Sequence = uint32(i)
	}
	if len(outputs) > 0 {
		if err = t.db.Create(&outputs).Error; err != nil {
			return err
		}
	}
	if len(invocations) > 0 {
		return t.db.Create(&invocations).Error
	}
	return nil
}
