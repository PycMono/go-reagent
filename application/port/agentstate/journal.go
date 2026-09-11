package agentstate

import "context"

type Phase string

const (
	PhaseReserved     Phase = "reserved"
	PhaseBundled      Phase = "bundled"
	PhaseMaterialized Phase = "materialized"
	PhaseValidated    Phase = "validated"
)

type Preparation struct {
	SchemaVersion      int    `json:"schema_version"`
	TenantID           string `json:"tenant_id"`
	AgentID            string `json:"agent_id"`
	VersionID          string `json:"version_id"`
	ExpectedRowVersion uint64 `json:"expected_row_version"`
	Phase              Phase  `json:"phase"`
	Tag                string `json:"tag"`
	TempSource         string `json:"temp_source"`
	Materialized       string `json:"materialized"`
	BundleCommit       string `json:"bundle_commit"`
	BundleDigest       string `json:"bundle_digest"`
	SpecDigest         string `json:"spec_digest"`
}

type Journal interface {
	Begin(ctx context.Context, tenantID, agentID, versionID string, expectedRowVersion uint64) (Preparation, error)
	Save(context.Context, Preparation) error
	Load(ctx context.Context, tenantID, agentID, versionID string) (Preparation, error)
	List(context.Context) ([]Preparation, error)
	Remove(ctx context.Context, tenantID, agentID, versionID string) error
	Complete(context.Context, Preparation) error
	Discard(context.Context, Preparation) error
}
