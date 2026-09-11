package agenttraining

import (
	"context"
	"encoding/json"
	"errors"
	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	"github.com/PycMono/go-reagent/application/identity"
	ce "github.com/PycMono/go-reagent/common/errors"
	agent "github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	ids "github.com/PycMono/go-reagent/domain/repository"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	dbtime "github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct {
	provider     sqlsdk.Provider
	transactions transaction.Manager
	ids          ids.IIDService
}

func NewRepository(p sqlsdk.Provider, t transaction.Manager, id ids.IIDService) *Repository {
	return &Repository{p, t, id}
}

var _ repo.Repository = (*Repository)(nil)

type row struct {
	ID, TenantID, AgentID, AdminUserID, ConversationID, BaseVersionID, CandidateHead string
	CandidateConfigJSON                                                              []byte
	CandidatePartial                                                                 bool
	Status                                                                           training.Status
	RowVersion                                                                       uint64
	ExpiresAt                                                                        time.Time
	ValidationJSON, OperationJSON                                                    []byte
	ResultVersionID                                                                  *string
	CreatedAt, UpdatedAt                                                             time.Time
}

func (x row) entity() (training.Session, error) {
	x.ExpiresAt = dbtime.DecodeUTC(x.ExpiresAt)
	x.CreatedAt = dbtime.DecodeUTC(x.CreatedAt)
	x.UpdatedAt = dbtime.DecodeUTC(x.UpdatedAt)
	var op training.Operation
	if err := json.Unmarshal(x.OperationJSON, &op); err != nil {
		return training.Session{}, err
	}
	return training.Session{ID: x.ID, TenantID: x.TenantID, AgentID: x.AgentID, AdminUserID: x.AdminUserID, ConversationID: x.ConversationID, BaseVersionID: x.BaseVersionID, CandidateHead: x.CandidateHead, CandidateConfig: x.CandidateConfigJSON, CandidatePartial: x.CandidatePartial, Status: x.Status, RowVersion: x.RowVersion, ExpiresAt: x.ExpiresAt, Validation: x.ValidationJSON, Operation: op, ResultVersionID: x.ResultVersionID, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}, nil
}
func record(s training.Session) (map[string]any, error) {
	op, err := json.Marshal(s.Operation)
	if err != nil || len(op) > 65536 || len(s.CandidateConfig) > 1<<20 || len(s.Validation) > 1<<20 {
		return nil, ce.ErrInvalidParam
	}
	return map[string]any{"id": s.ID, "tenant_id": s.TenantID, "agent_id": s.AgentID, "admin_user_id": s.AdminUserID, "conversation_id": s.ConversationID, "base_version_id": s.BaseVersionID, "candidate_head": s.CandidateHead, "candidate_config_json": []byte(s.CandidateConfig), "candidate_partial": s.CandidatePartial, "status": s.Status, "row_version": s.RowVersion, "expires_at": s.ExpiresAt.UTC().Format("2006-01-02 15:04:05.999999"), "validation_json": nullableJSON(s.Validation), "operation_json": op, "result_version_id": s.ResultVersionID, "created_at": s.CreatedAt.UTC().Format("2006-01-02 15:04:05.999999"), "updated_at": s.UpdatedAt.UTC().Format("2006-01-02 15:04:05.999999")}, nil
}
func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
func scoped(db *gorm.DB, s repo.Scope) *gorm.DB {
	return db.Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND id = ? AND admin_user_id = ?", s.TenantID, s.AgentID, s.TrainingID, s.AdminUserID)
}
func load(db *gorm.DB) (training.Session, error) {
	var x row
	if err := db.Take(&x).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return training.Session{}, ce.ErrNotFound
		}
		return training.Session{}, err
	}
	return x.entity()
}
func (r *Repository) Find(ctx context.Context, s repo.Scope) (training.Session, error) {
	return load(scoped(r.provider.UseDB(ctx), s))
}

// ActiveSession is for Agent-level expiry reconciliation. Its contents are never
// returned by an admin endpoint; user-facing reads still require creator scope.
func (r *Repository) ActiveSession(ctx context.Context, tenant, agentID string) (training.Session, error) {
	return load(r.provider.UseDB(ctx).Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND status IN ?", tenant, agentID, []string{"active", "validating", "ready"}))
}
func (r *Repository) FindOwned(ctx context.Context, p identity.Principal, id string) (training.Session, error) {
	if err := p.RequireAdmin(); err != nil {
		return training.Session{}, err
	}
	return load(r.provider.UseDB(ctx).Table("agent_training_sessions").Where("tenant_id = ? AND admin_user_id = ? AND id = ?", p.TenantID, p.UserID, id))
}
func (r *Repository) List(ctx context.Context, p identity.Principal, agentID, cursor string, limit int) ([]training.Session, string, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	var rows []row
	err := r.provider.UseDB(ctx).Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND admin_user_id = ? AND id > ?", p.TenantID, agentID, p.UserID, cursor).Order("id").Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	out := make([]training.Session, len(rows))
	for i := range rows {
		out[i], err = rows[i].entity()
		if err != nil {
			return nil, "", err
		}
	}
	return out, next, nil
}

type tx struct {
	db            *gorm.DB
	tenant, agent string
	ids           ids.IIDService
	changed       map[string]training.Session
}

func (r *Repository) WithAgentTx(ctx context.Context, tenant, agentID string, fn func(repo.Tx) error) error {
	return r.transactions.Transaction(ctx, func(c context.Context) error {
		t := &tx{r.provider.UseDB(c), tenant, agentID, r.ids, map[string]training.Session{}}
		if _, err := t.AgentForUpdate(); err != nil {
			return err
		}
		if err := fn(t); err != nil {
			return err
		}
		if len(t.changed) > 0 {
			a, err := t.AgentForUpdate()
			if err != nil {
				return err
			}
			for _, s := range t.changed {
				matches := a.ActiveTrainingSessionID != nil && *a.ActiveTrainingSessionID == s.ID
				if s.Terminal() == matches {
					return ce.ErrConflict
				}
			}
		}
		return nil
	})
}
func (t *tx) NowUTC() (time.Time, error) {
	return dbtime.UTCNow(t.db)
}
func (t *tx) AgentForUpdate() (agent.Agent, error) {
	var a agent.Agent
	var raw struct {
		ID, TenantID, Name, Description, Status, TemplateCode, CreatedBy string
		BootstrapKey, ActiveVersionID, ActiveTrainingSessionID           *string
		RowVersion                                                       uint64
		CreatedAt, UpdatedAt                                             time.Time
		PresentationJSON                                                 []byte
	}
	err := t.db.Table("agents").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", t.tenant, t.agent).Take(&raw).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return a, ce.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a = agent.Agent{ID: raw.ID, TenantID: raw.TenantID, Name: raw.Name, Description: raw.Description, Status: raw.Status, TemplateCode: raw.TemplateCode, CreatedBy: raw.CreatedBy, BootstrapKey: raw.BootstrapKey, ActiveVersionID: raw.ActiveVersionID, ActiveTrainingSessionID: raw.ActiveTrainingSessionID, RowVersion: raw.RowVersion, CreatedAt: raw.CreatedAt, UpdatedAt: raw.UpdatedAt}
	if err = json.Unmarshal(raw.PresentationJSON, &a.Presentation); err != nil {
		return a, err
	}
	return a, nil
}
func (t *tx) SessionForUpdate(s repo.Scope) (training.Session, error) {
	if s.TenantID != t.tenant || s.AgentID != t.agent {
		return training.Session{}, ce.ErrNotFound
	}
	return load(scoped(t.db, s).Clauses(clause.Locking{Strength: "UPDATE"}))
}
func (t *tx) CreateSession(s training.Session, c conversation.Conversation) error {
	if s.TenantID != t.tenant || s.AgentID != t.agent || s.Status != training.Active || s.RowVersion != 0 || s.AdminUserID != c.UserID || c.TenantID != s.TenantID || c.AgentID != s.AgentID || c.AgentVersionID != s.BaseVersionID || c.ID != s.ConversationID || c.ConversationType != "training" || !s.ExpiresAt.After(s.CreatedAt) {
		return ce.ErrInvalidParam
	}
	a, err := t.AgentForUpdate()
	if err != nil {
		return err
	}
	if a.Status != "enabled" || a.ActiveTrainingSessionID != nil || a.ActiveVersionID == nil || *a.ActiveVersionID != s.BaseVersionID {
		return ce.ErrConflict
	}
	var count int64
	if err = t.db.Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND status IN ('active','validating','ready')", t.tenant, t.agent).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ce.ErrConflict
	}
	rec, err := record(s)
	if err != nil {
		return err
	}
	if err = t.db.Omit("ProfileCode").Create(&c).Error; err != nil {
		return err
	}
	if err = t.db.Table("agent_training_sessions").Create(rec).Error; err != nil {
		return err
	}
	t.changed[s.ID] = s
	return nil
}
func (t *tx) SaveSession(s training.Session, expected uint64) error {
	old, err := t.SessionForUpdate(repo.Scope{s.TenantID, s.AgentID, s.ID, s.AdminUserID})
	if err != nil {
		return err
	}
	if old.RowVersion != expected || !old.ExpiresAt.Equal(s.ExpiresAt) || !old.CreatedAt.Equal(s.CreatedAt) || old.ConversationID != s.ConversationID || old.BaseVersionID != s.BaseVersionID || old.Terminal() {
		return ce.ErrConflict
	}
	check := old
	check.Operation = s.Operation
	check.Validation = s.Validation
	check.CandidatePartial = s.CandidatePartial
	if s.Status != old.Status {
		if err = check.Transition(s.Status); err != nil {
			return err
		}
	}
	rec, err := record(s)
	if err != nil {
		return err
	}
	for _, key := range []string{"id", "tenant_id", "agent_id", "admin_user_id", "conversation_id", "base_version_id", "created_at", "expires_at"} {
		delete(rec, key)
	}
	rec["row_version"] = gorm.Expr("row_version + 1")
	rec["updated_at"] = gorm.Expr("UTC_TIMESTAMP(6)")
	result := scoped(t.db, repo.Scope{s.TenantID, s.AgentID, s.ID, s.AdminUserID}).Where("row_version = ?", expected).Updates(rec)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ce.ErrConflict
	}
	t.changed[s.ID] = s
	return nil
}
func (t *tx) SetTrainingPointer(expected string, next *string, version uint64) error {
	q := t.db.Table("agents").Where("tenant_id = ? AND id = ? AND row_version = ?", t.tenant, t.agent, version)
	if expected == "" {
		q = q.Where("active_training_session_id IS NULL")
	} else {
		q = q.Where("active_training_session_id = ?", expected)
	}
	if next != nil {
		var count int64
		if err := t.db.Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND id = ? AND status IN ('active','validating','ready')", t.tenant, t.agent, *next).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ce.ErrConflict
		}
	}
	res := q.Updates(map[string]any{"active_training_session_id": next, "row_version": gorm.Expr("row_version + 1")})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ce.ErrConflict
	}
	return nil
}
