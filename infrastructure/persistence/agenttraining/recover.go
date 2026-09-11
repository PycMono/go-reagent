package agenttraining

import (
	"context"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
)

// RecoverySessions is only used by the process startup maintenance path, before
// HTTP is served. Ordinary requests always use tenant/admin-scoped methods.
func (r *Repository) RecoverySessions(ctx context.Context, after string) ([]training.Session, error) {
	var rows []row
	err := r.provider.UseDB(ctx).Table("agent_training_sessions").Where("status IN ? AND id > ?", []string{"active", "validating", "ready"}, after).Order("id").Limit(100).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]training.Session, len(rows))
	for i, row := range rows {
		items[i], err = row.entity()
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
