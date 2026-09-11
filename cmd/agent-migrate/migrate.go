package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/domain/repository/agent"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"gorm.io/gorm"
)

func readOwnership(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("ownership mapping exceeds 16 MiB")
	}
	var rows []struct {
		ConversationID string `json:"conversation_id"`
		TenantID       string `json:"tenant_id"`
	}
	if err := agentversion.DecodeStrict(raw, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !identity.ValidID(row.ConversationID) || !identity.ValidID(row.TenantID) || out[row.ConversationID] != "" {
			return nil, errors.New("invalid or duplicate ownership mapping")
		}
		out[row.ConversationID] = row.TenantID
	}
	return out, nil
}

type legacyRow struct {
	ID                                string
	TenantID, AgentID, AgentVersionID *string
	ProfileCode, ConversationType     string
}
type binding struct {
	old                    legacyRow
	tenant, agent, version string
}

func migrate(ctx context.Context, db *gorm.DB, repository agent.Repository, bundles *agentbundle.Store, mode, tenant string, single bool, owners map[string]string, dry bool, out io.Writer) error {
	if single && (!identity.ValidID(tenant) || len(owners) > 0) {
		return errors.New("single-tenant confirmation requires tenant-id and cannot be combined with a mapping")
	}
	hasColumn := func(name string) (bool, error) {
		var n int64
		err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'agent_conversations' AND column_name = ?", name).Scan(&n).Error
		return n == 1, err
	}
	hasTenant, err := hasColumn("tenant_id")
	if err != nil {
		return err
	}
	if !hasTenant {
		return errors.New("apply migration 0007 before Agent maintenance")
	}
	profile := "'' AS profile_code"
	hasProfile, err := hasColumn("profile_code")
	if err != nil {
		return err
	}
	if hasProfile {
		profile = "profile_code"
	}
	changes := []binding{}
	seen := map[string]bool{}
	verified := map[string]bool{}
	count := 0
	verify := func(t, a, v string) error {
		key := t + "\x00" + a + "\x00" + v
		if verified[key] {
			return nil
		}
		version, err := repository.FindVersion(ctx, t, a, v)
		if err != nil {
			return err
		}
		if _, err := agentruntime.ParseVersion(version); err != nil {
			return err
		}
		if err := bundles.Verify(ctx, t, a, agentbundle.BundleRef{Commit: version.BundleCommit, Tag: version.BundleTag, Digest: version.BundleDigest}); err != nil {
			return err
		}
		verified[key] = true
		return nil
	}
	cursor := ""
	for {
		var rows []legacyRow
		if err := db.WithContext(ctx).Table("agent_conversations").Select("id,tenant_id,agent_id,agent_version_id,conversation_type,"+profile).Where("id > ?", cursor).Order("id").Limit(200).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			count++
			seen[row.ID] = true
			cursor = row.ID
			owner := owners[row.ID]
			if single {
				owner = tenant
			}
			if row.TenantID != nil {
				if !identity.ValidID(*row.TenantID) || (owner != "" && owner != *row.TenantID) {
					return fmt.Errorf("conversation %s has conflicting tenant ownership", row.ID)
				}
				owner = *row.TenantID
			}
			complete := row.TenantID != nil && row.AgentID != nil && row.AgentVersionID != nil
			if complete {
				if err := verify(owner, *row.AgentID, *row.AgentVersionID); err != nil {
					return fmt.Errorf("conversation %s: %w", row.ID, err)
				}
				continue
			}
			if mode == "verify" {
				return fmt.Errorf("conversation %s has incomplete Agent binding", row.ID)
			}
			if owner == "" {
				return fmt.Errorf("conversation %s lacks host-confirmed ownership; provide a mapping or explicit single-tenant confirmation", row.ID)
			}
			if row.ConversationType != "chat" {
				return fmt.Errorf("conversation %s has unresolved non-chat ownership", row.ID)
			}
			a, err := repository.FindBootstrap(ctx, owner, row.ProfileCode)
			if err != nil {
				return fmt.Errorf("conversation %s has no tenant bootstrap for profile %s: %w", row.ID, row.ProfileCode, err)
			}
			if a.ActiveVersionID == nil {
				return fmt.Errorf("Agent %s has no completed initial version", a.ID)
			}
			if row.AgentID != nil && *row.AgentID != a.ID {
				return fmt.Errorf("conversation %s has conflicting Agent binding", row.ID)
			}
			initial, err := repository.ListVersions(ctx, owner, a.ID, 2, 1)
			if err != nil {
				return err
			}
			if len(initial.Items) != 1 || initial.Items[0].Number != 1 {
				return fmt.Errorf("Agent %s lacks initial version", a.ID)
			}
			v := initial.Items[0].ID
			if row.AgentVersionID != nil {
				v = *row.AgentVersionID
			}
			if err := verify(owner, a.ID, v); err != nil {
				return err
			}
			changes = append(changes, binding{row, owner, a.ID, v})
		}
	}
	for id := range owners {
		if !seen[id] {
			return fmt.Errorf("ownership map contains unknown conversation %s", id)
		}
	}
	// Verify all immutable versions, including those not currently selected by a
	// conversation. Recovery must not silently fall back to template files.
	cursor = ""
	repositories := map[string]bool{}
	for {
		var rows []struct{ ID, TenantID, AgentID string }
		if err := db.WithContext(ctx).Table("agent_versions").Select("id,tenant_id,agent_id").Where("id > ?", cursor).Order("id").Limit(200).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			cursor = row.ID
			if err := verify(row.TenantID, row.AgentID, row.ID); err != nil {
				return fmt.Errorf("version %s: %w", row.ID, err)
			}
			key := row.TenantID + "\x00" + row.AgentID
			if !repositories[key] {
				if err := bundles.VerifyRepository(ctx, row.TenantID, row.AgentID); err != nil {
					return err
				}
				repositories[key] = true
			}
		}
	}
	var broken int64
	if err := db.Raw("SELECT COUNT(*) FROM agents a LEFT JOIN agent_versions v ON v.tenant_id=a.tenant_id AND v.agent_id=a.id AND v.id=a.active_version_id WHERE a.active_version_id IS NOT NULL AND v.id IS NULL").Scan(&broken).Error; err != nil {
		return err
	}
	if broken > 0 {
		return errors.New("Agent active version points to a missing or foreign version")
	}
	var trainingTable int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='agent_training_sessions'").Scan(&trainingTable).Error; err != nil {
		return err
	}
	if trainingTable == 1 {
		if err := db.Raw("SELECT COUNT(*) FROM agent_training_sessions s LEFT JOIN agents a ON a.tenant_id=s.tenant_id AND a.id=s.agent_id WHERE (s.status IN ('active','validating','ready') AND (a.active_training_session_id IS NULL OR a.active_training_session_id<>s.id)) OR (s.status NOT IN ('active','validating','ready') AND a.active_training_session_id=s.id)").Scan(&broken).Error; err != nil {
			return err
		}
		if broken > 0 {
			return errors.New("training state and active pointer disagree")
		}
		cursor = ""
		for {
			var rows []struct{ ID, TenantID, AgentID, CandidateHead string }
			if err := db.WithContext(ctx).Table("agent_training_sessions").Select("id,tenant_id,agent_id,candidate_head").Where("id > ?", cursor).Order("id").Limit(200).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				cursor = row.ID
				if err := bundles.VerifyCandidateHead(ctx, agentbundle.Candidate{TenantID: row.TenantID, AgentID: row.AgentID, TrainingID: row.ID, Head: row.CandidateHead}); err != nil {
					return fmt.Errorf("training %s: %w", row.ID, err)
				}
			}
		}
	}
	if mode == "backfill" && !dry {
		for _, change := range changes {
			old := change.old
			r := db.WithContext(ctx).Table("agent_conversations").Where("id = ? AND tenant_id <=> ? AND agent_id <=> ? AND agent_version_id <=> ?", old.ID, old.TenantID, old.AgentID, old.AgentVersionID).Updates(map[string]any{"tenant_id": change.tenant, "agent_id": change.agent, "agent_version_id": change.version, "follow_latest": true})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return fmt.Errorf("conversation %s changed during backfill; stop writers before retry", old.ID)
			}
		}
	}
	fmt.Fprintf(out, "verified %d conversations, %d versions; %d bindings %s\n", count, len(verified), len(changes), map[bool]string{true: "planned", false: "applied"}[mode != "backfill" || dry])
	return nil
}
