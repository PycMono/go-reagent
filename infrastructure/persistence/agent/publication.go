package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	entity "github.com/PycMono/go-reagent/domain/entity/agent"
	dbtime "github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validPublicationEvidence(e agentversion.ValidationReport) bool {
	return e.Passed && e.DigestVersion == 1 && e.ReviewMode == "manual" && e.Head != "" && e.SpecDigest != "" && e.BundleDigest != "" && e.ValidatorVersion != "" && e.ValidationPolicyDigest != "" && !e.ValidatedAt.IsZero() && e.ValidUntil.After(e.ValidatedAt)
}
func (r *Repository) ActivateVersion(ctx context.Context, tenant, agentID, versionID string, expected uint64, e agentversion.ValidationReport) error {
	if err := r.validate(ctx); err != nil {
		return err
	}
	if !validPublicationEvidence(e) {
		return errors.New("invalid activation evidence")
	}
	return r.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := r.provider.UseDB(txCtx)
		if err := lockPublicationAgent(db, tenant, agentID, expected, ""); err != nil {
			return err
		}
		v, err := r.FindVersion(txCtx, tenant, agentID, versionID)
		if err != nil {
			return err
		}
		if v.BundleCommit != e.Head || v.BundleDigest != e.BundleDigest || v.SpecDigest != e.SpecDigest {
			return errors.New("activation evidence mismatch")
		}
		if err = checkEvidenceClock(db, e); err != nil {
			return err
		}
		result := db.Table("agents").Where("tenant_id = ? AND id = ? AND row_version = ?", tenant, agentID, expected).Updates(map[string]any{"active_version_id": versionID, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonerrors.ErrConflict
		}
		return nil
	})
}
func (r *Repository) CommitModelRelease(ctx context.Context, tenant, agentID string, expected uint64, baseID string, v entity.Version, e agentversion.ValidationReport) error {
	if err := r.validate(ctx); err != nil {
		return err
	}
	if !validPublicationEvidence(e) || v.TenantID != tenant || v.AgentID != agentID || v.SourceTrainingSessionID != nil || v.Number < 2 || v.BundleCommit != e.Head || v.BundleDigest != e.BundleDigest || v.SpecDigest != e.SpecDigest {
		return errors.New("invalid model release evidence")
	}
	return r.transactions.Transaction(ctx, func(txCtx context.Context) error {
		db := r.provider.UseDB(txCtx)
		if err := lockPublicationAgent(db, tenant, agentID, expected, baseID); err != nil {
			return err
		}
		base, err := r.FindVersion(txCtx, tenant, agentID, baseID)
		if err != nil {
			return err
		}
		if string(v.ToolPolicy) != string(base.ToolPolicy) || string(v.RuntimeConfig) != string(base.RuntimeConfig) || v.BundleCommit != base.BundleCommit || v.BundleDigest != base.BundleDigest {
			return errors.New("model release changed non-model fields")
		}
		if _, err = agentversion.SnapshotFromVersion(v); err != nil {
			return err
		}
		var latest uint64
		if err = db.Table("agent_versions").Select("COALESCE(MAX(version), 0)").Where("tenant_id = ? AND agent_id = ?", tenant, agentID).Scan(&latest).Error; err != nil {
			return err
		}
		if v.Number != latest+1 {
			return commonerrors.ErrConflict
		}
		if err = checkEvidenceClock(db, e); err != nil {
			return err
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if string(raw) != string(v.Validation) {
			return errors.New("saved evidence mismatch")
		}
		if err = db.Table("agent_versions").Create(versionRecord(v)).Error; err != nil {
			return err
		}
		result := db.Table("agents").Where("tenant_id = ? AND id = ? AND row_version = ? AND active_version_id = ?", tenant, agentID, expected, baseID).Updates(map[string]any{"active_version_id": v.ID, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonerrors.ErrConflict
		}
		return nil
	})
}
func lockPublicationAgent(db *gorm.DB, tenant, agentID string, expected uint64, baseID string) error {
	var row agentRow
	err := db.Table("agents").Clauses(clause.Locking{Strength: "UPDATE"}).Select(agentColumns).Where("tenant_id = ? AND id = ?", tenant, agentID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonerrors.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.Status != "enabled" || row.RowVersion != expected || (baseID != "" && (row.ActiveVersionID == nil || *row.ActiveVersionID != baseID)) {
		return commonerrors.ErrConflict
	}
	if row.ActiveTrainingSessionID != nil {
		var operation struct{ OperationJSON []byte }
		if err = db.Table("agent_training_sessions").Clauses(clause.Locking{Strength: "UPDATE"}).Select("operation_json").Where("tenant_id = ? AND agent_id = ? AND id = ?", tenant, agentID, *row.ActiveTrainingSessionID).Take(&operation).Error; err != nil {
			return err
		}
		if len(operation.OperationJSON) > 0 && string(operation.OperationJSON) != "null" {
			var saved struct {
				State string `json:"state"`
			}
			if json.Unmarshal(operation.OperationJSON, &saved) != nil || saved.State == "" || saved.State == "running" {
				return commonerrors.ErrConflict
			}
		}
	}
	return nil
}
func checkEvidenceClock(db *gorm.DB, e agentversion.ValidationReport) error {
	now, err := dbtime.UTCNow(db)
	if err != nil {
		return err
	}
	if now.Before(e.ValidatedAt) || !now.Before(e.ValidUntil) {
		return errors.New("publication evidence expired")
	}
	return nil
}
