package agenttraining

import (
	"encoding/json"
	ce "github.com/PycMono/go-reagent/common/errors"
	agent "github.com/PycMono/go-reagent/domain/entity/agent"
	"gorm.io/gorm"
	"time"
)

func (t *tx) FindVersion(id string) (agent.Version, error) {
	var v agent.Version
	var r struct {
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
	err := t.db.Table("agent_versions").Where("tenant_id = ? AND agent_id = ? AND id = ?", t.tenant, t.agent, id).Take(&r).Error
	if err != nil {
		return v, err
	}
	v = agent.Version{ID: r.ID, TenantID: r.TenantID, AgentID: r.AgentID, Number: r.Version, BundleCommit: r.BundleCommit, BundleTag: r.BundleTag, BundleDigest: r.BundleDigest, ModelConfig: json.RawMessage(r.ModelConfigJSON), ToolPolicy: json.RawMessage(r.ToolPolicyJSON), RuntimeConfig: json.RawMessage(r.RuntimeConfigJSON), SpecDigest: r.SpecDigest, Validation: r.ValidationJSON, SourceTrainingSessionID: r.SourceTrainingSessionID, PublishedBy: r.PublishedBy, PublishedAt: r.PublishedAt, ChangeSummary: r.ChangeSummary}
	return v, nil
}
func (t *tx) CommitPublication(v agent.Version, expected uint64) error {
	if v.TenantID != t.tenant || v.AgentID != t.agent || v.SourceTrainingSessionID == nil {
		return ce.ErrInvalidParam
	}
	var count int64
	if err := t.db.Table("agent_training_sessions").Where("tenant_id = ? AND agent_id = ? AND id = ? AND status = 'ready'", t.tenant, t.agent, *v.SourceTrainingSessionID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ce.ErrConflict
	}
	record := map[string]any{"id": v.ID, "tenant_id": v.TenantID, "agent_id": v.AgentID, "version": v.Number, "bundle_commit": v.BundleCommit, "bundle_tag": v.BundleTag, "bundle_digest": v.BundleDigest, "model_config_json": []byte(v.ModelConfig), "tool_policy_json": []byte(v.ToolPolicy), "runtime_config_json": []byte(v.RuntimeConfig), "spec_digest": v.SpecDigest, "validation_json": []byte(v.Validation), "source_training_session_id": v.SourceTrainingSessionID, "published_by": v.PublishedBy, "published_at": v.PublishedAt, "change_summary": v.ChangeSummary}
	if err := t.db.Table("agent_versions").Create(record).Error; err != nil {
		return err
	}
	r := t.db.Table("agents").Where("tenant_id = ? AND id = ? AND row_version = ? AND status = 'enabled' AND active_training_session_id = ?", t.tenant, t.agent, expected, *v.SourceTrainingSessionID).Updates(map[string]any{"active_version_id": v.ID, "active_training_session_id": nil, "row_version": gorm.Expr("row_version + 1")})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ce.ErrConflict
	}
	return nil
}
