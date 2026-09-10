package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	agententity "github.com/PycMono/go-reagent/domain/entity/agent"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	"gorm.io/gorm"
)

const maxListLimit = 100

const agentColumns = "id, tenant_id, name, description, status, presentation_json, template_code, bootstrap_key, active_version_id, active_training_session_id, row_version, created_by, created_at, updated_at"
const versionColumns = "id, tenant_id, agent_id, version, bundle_commit, bundle_tag, bundle_digest, model_config_json, tool_policy_json, runtime_config_json, spec_digest, validation_json, source_training_session_id, published_by, published_at, change_summary"
const presentationOrder = "CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.presentation_json,'$.order')), '0') AS SIGNED)"

type Repository struct {
	provider     sqlsdk.Provider
	transactions transaction.Manager
}

func NewRepository(provider sqlsdk.Provider, transactions transaction.Manager) *Repository {
	return &Repository{provider: provider, transactions: transactions}
}

type agentRow struct {
	ID, TenantID, Name, Description, Status                string
	PresentationJSON                                       []byte
	TemplateCode                                           string
	BootstrapKey, ActiveVersionID, ActiveTrainingSessionID *string
	RowVersion                                             uint64
	CreatedBy                                              string
	CreatedAt, UpdatedAt                                   time.Time
}

type versionRow struct {
	ID, TenantID, AgentID                              string
	Version                                            uint64
	BundleCommit, BundleTag, BundleDigest              string
	ModelConfigJSON, ToolPolicyJSON, RuntimeConfigJSON []byte
	SpecDigest                                         string
	ValidationJSON                                     []byte
	SourceTrainingSessionID                            *string
	PublishedBy                                        string
	PublishedAt                                        time.Time
	ChangeSummary                                      string
}

func (r *Repository) Find(ctx context.Context, tenantID, agentID string) (agententity.Agent, error) {
	if err := r.validate(ctx); err != nil {
		return agententity.Agent{}, err
	}
	var row agentRow
	err := r.provider.UseDB(ctx).Raw("SELECT "+agentColumns+" FROM agents WHERE tenant_id = ? AND id = ? LIMIT 1", tenantID, agentID).Scan(&row).Error
	if err != nil {
		return agententity.Agent{}, fmt.Errorf("mysql agent: find: %w", err)
	}
	if row.ID == "" {
		return agententity.Agent{}, commonerrors.ErrNotFound
	}
	return row.entity()
}

func (r *Repository) FindBootstrap(ctx context.Context, tenantID, bootstrapKey string) (agententity.Agent, error) {
	if err := r.validate(ctx); err != nil {
		return agententity.Agent{}, err
	}
	var row agentRow
	err := r.provider.UseDB(ctx).Raw("SELECT "+agentColumns+" FROM agents WHERE tenant_id = ? AND bootstrap_key = ? LIMIT 1", tenantID, bootstrapKey).Scan(&row).Error
	if err != nil {
		return agententity.Agent{}, fmt.Errorf("mysql agent: find bootstrap: %w", err)
	}
	if row.ID == "" {
		return agententity.Agent{}, commonerrors.ErrNotFound
	}
	return row.entity()
}

func (r *Repository) FindVersion(ctx context.Context, tenantID, agentID, versionID string) (agententity.Version, error) {
	if err := r.validate(ctx); err != nil {
		return agententity.Version{}, err
	}
	var row versionRow
	err := r.provider.UseDB(ctx).Raw("SELECT "+versionColumns+" FROM agent_versions WHERE tenant_id = ? AND agent_id = ? AND id = ? LIMIT 1", tenantID, agentID, versionID).Scan(&row).Error
	if err != nil {
		return agententity.Version{}, fmt.Errorf("mysql agent: find version: %w", err)
	}
	if row.ID == "" {
		return agententity.Version{}, commonerrors.ErrNotFound
	}
	return row.entity(), nil
}

func (r *Repository) List(ctx context.Context, query agentrepo.ListQuery) (agentrepo.ListPage, error) {
	if err := r.validate(ctx); err != nil {
		return agentrepo.ListPage{}, err
	}
	limit := boundedLimit(query.Limit)
	join := "JOIN"
	if query.IncludeArchived {
		join = "LEFT JOIN"
	}
	where := []string{"a.tenant_id = ?"}
	args := []any{query.TenantID}
	if !query.IncludeArchived {
		where = append(where, "a.status = 'enabled'")
	}
	if query.Keyword != "" {
		pattern := "%" + escapeLike(query.Keyword) + "%"
		where = append(where, "(a.name LIKE ? ESCAPE '\\\\' OR a.description LIKE ? ESCAPE '\\\\')")
		args = append(args, pattern, pattern)
	}
	if query.Cursor != "" {
		order, id, err := parseCursor(query.Cursor)
		if err != nil {
			return agentrepo.ListPage{}, err
		}
		where = append(where, "("+presentationOrder+" > ? OR ("+presentationOrder+" = ? AND a.id > ?))")
		args = append(args, order, order, id)
	}
	args = append(args, limit+1)
	statement := "SELECT " + agentColumnsWithAlias() + " FROM agents AS a " + join + " agent_versions AS v ON v.tenant_id = a.tenant_id AND v.agent_id = a.id AND v.id = a.active_version_id WHERE " + strings.Join(where, " AND ") + " ORDER BY " + presentationOrder + ", a.id LIMIT ?"
	var rows []agentRow
	if err := r.provider.UseDB(ctx).Raw(statement, args...).Scan(&rows).Error; err != nil {
		return agentrepo.ListPage{}, fmt.Errorf("mysql agent: list: %w", err)
	}
	page := agentrepo.ListPage{Items: make([]agententity.Agent, 0, min(len(rows), limit))}
	for _, row := range rows[:min(len(rows), limit)] {
		item, err := row.entity()
		if err != nil {
			return agentrepo.ListPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if len(rows) > limit && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = formatCursor(last.Presentation.Order, last.ID)
	}
	var defaultID string
	defaultSQL := "SELECT a.id FROM agents AS a JOIN agent_versions AS v ON v.tenant_id = a.tenant_id AND v.agent_id = a.id AND v.id = a.active_version_id WHERE a.tenant_id = ? AND a.status = 'enabled' ORDER BY " + presentationOrder + ", a.id LIMIT 1"
	if err := r.provider.UseDB(ctx).Raw(defaultSQL, query.TenantID).Scan(&defaultID).Error; err != nil {
		return agentrepo.ListPage{}, fmt.Errorf("mysql agent: find default: %w", err)
	}
	if defaultID != "" {
		page.DefaultAgentID = &defaultID
	}
	return page, nil
}

func (r *Repository) ListVersions(ctx context.Context, tenantID, agentID string, before uint64, limit int) ([]agententity.Version, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	limit = boundedLimit(limit)
	where := "tenant_id = ? AND agent_id = ?"
	args := []any{tenantID, agentID}
	if before > 0 {
		where += " AND version < ?"
		args = append(args, before)
	}
	args = append(args, limit+1)
	var rows []versionRow
	if err := r.provider.UseDB(ctx).Raw("SELECT "+versionColumns+" FROM agent_versions WHERE "+where+" ORDER BY version DESC LIMIT ?", args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("mysql agent: list versions: %w", err)
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	items := make([]agententity.Version, len(rows))
	for i := range rows {
		items[i] = rows[i].entity()
	}
	return items, nil
}

func (r *Repository) ReserveDraft(ctx context.Context, agent agententity.Agent) (agententity.Agent, error) {
	if err := r.validate(ctx); err != nil {
		return agententity.Agent{}, err
	}
	presentation, err := json.Marshal(agent.Presentation)
	if err != nil {
		return agententity.Agent{}, err
	}
	row := map[string]any{"id": agent.ID, "tenant_id": agent.TenantID, "name": agent.Name, "description": agent.Description, "status": agent.Status, "presentation_json": presentation, "template_code": agent.TemplateCode, "bootstrap_key": agent.BootstrapKey, "active_version_id": nil, "active_training_session_id": nil, "row_version": agent.RowVersion, "created_by": agent.CreatedBy}
	if err := r.provider.UseDB(ctx).Table("agents").Create(row).Error; err != nil {
		return agententity.Agent{}, fmt.Errorf("mysql agent: reserve draft: %w", err)
	}
	return r.Find(ctx, agent.TenantID, agent.ID)
}

func (r *Repository) CommitInitial(ctx context.Context, agent agententity.Agent, version agententity.Version, expected uint64) error {
	if err := r.validate(ctx); err != nil {
		return err
	}
	if version.TenantID != agent.TenantID || version.AgentID != agent.ID || version.Number != 1 {
		return fmt.Errorf("mysql agent: invalid initial version ownership")
	}
	return r.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := r.provider.UseDB(txCtx)
		if err := db.Table("agent_versions").Create(versionRecord(version)).Error; err != nil {
			return fmt.Errorf("insert initial version: %w", err)
		}
		result := db.Table("agents").Where("tenant_id = ? AND id = ? AND status = 'enabled' AND active_version_id IS NULL AND row_version = ?", agent.TenantID, agent.ID, expected).Updates(map[string]any{"active_version_id": version.ID, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonerrors.ErrConflict
		}
		return nil
	})
}

func (r *Repository) UpdatePresentation(ctx context.Context, agent agententity.Agent, expected uint64) error {
	if err := r.validate(ctx); err != nil {
		return err
	}
	presentation, err := json.Marshal(agent.Presentation)
	if err != nil {
		return err
	}
	query := r.provider.UseDB(ctx).Table("agents").Where("tenant_id = ? AND id = ? AND row_version = ?", agent.TenantID, agent.ID, expected)
	if agent.Status == "archived" {
		query = query.Where("active_training_session_id IS NULL")
	}
	result := query.Updates(map[string]any{"name": agent.Name, "description": agent.Description, "presentation_json": presentation, "status": agent.Status, "row_version": gorm.Expr("row_version + 1")})
	if result.Error != nil {
		return fmt.Errorf("mysql agent: update presentation: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return commonerrors.ErrConflict
	}
	return nil
}

func (r *Repository) validate(ctx context.Context) error {
	if ctx == nil {
		return errors.New("mysql agent: context is required")
	}
	if r == nil || r.provider == nil || r.transactions == nil {
		return errors.New("mysql agent: provider and transaction manager are required")
	}
	return ctx.Err()
}
func boundedLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}
func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}
func formatCursor(order int, id string) string { return strconv.Itoa(order) + ":" + id }
func parseCursor(cursor string) (int, string, error) {
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", errors.New("mysql agent: invalid cursor")
	}
	order, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", errors.New("mysql agent: invalid cursor")
	}
	return order, parts[1], nil
}
func agentColumnsWithAlias() string {
	parts := strings.Split(agentColumns, ", ")
	for i := range parts {
		parts[i] = "a." + parts[i]
	}
	return strings.Join(parts, ", ")
}

func (row agentRow) entity() (agententity.Agent, error) {
	var presentation agententity.Presentation
	if err := json.Unmarshal(row.PresentationJSON, &presentation); err != nil {
		return agententity.Agent{}, fmt.Errorf("mysql agent: decode presentation: %w", err)
	}
	return agententity.Agent{ID: row.ID, TenantID: row.TenantID, Name: row.Name, Description: row.Description, Status: row.Status, Presentation: presentation, TemplateCode: row.TemplateCode, BootstrapKey: row.BootstrapKey, ActiveVersionID: row.ActiveVersionID, ActiveTrainingSessionID: row.ActiveTrainingSessionID, RowVersion: row.RowVersion, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}
func (row versionRow) entity() agententity.Version {
	return agententity.Version{ID: row.ID, TenantID: row.TenantID, AgentID: row.AgentID, Number: row.Version, BundleCommit: row.BundleCommit, BundleTag: row.BundleTag, BundleDigest: row.BundleDigest, ModelConfig: json.RawMessage(row.ModelConfigJSON), ToolPolicy: json.RawMessage(row.ToolPolicyJSON), RuntimeConfig: json.RawMessage(row.RuntimeConfigJSON), SpecDigest: row.SpecDigest, Validation: json.RawMessage(row.ValidationJSON), SourceTrainingSessionID: row.SourceTrainingSessionID, PublishedBy: row.PublishedBy, PublishedAt: row.PublishedAt, ChangeSummary: row.ChangeSummary}
}
func versionRecord(version agententity.Version) map[string]any {
	return map[string]any{"id": version.ID, "tenant_id": version.TenantID, "agent_id": version.AgentID, "version": version.Number, "bundle_commit": version.BundleCommit, "bundle_tag": version.BundleTag, "bundle_digest": version.BundleDigest, "model_config_json": []byte(version.ModelConfig), "tool_policy_json": []byte(version.ToolPolicy), "runtime_config_json": []byte(version.RuntimeConfig), "spec_digest": version.SpecDigest, "validation_json": []byte(version.Validation), "source_training_session_id": version.SourceTrainingSessionID, "published_by": version.PublishedBy, "published_at": version.PublishedAt, "change_summary": version.ChangeSummary}
}

var _ agentrepo.Repository = (*Repository)(nil)
