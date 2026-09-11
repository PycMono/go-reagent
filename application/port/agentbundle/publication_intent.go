package agentbundle

import "context"

type PublicationIntent struct {
	TenantID           string `json:"tenant_id"`
	AgentID            string `json:"agent_id"`
	VersionID          string `json:"version_id"`
	BaseVersionID      string `json:"base_version_id"`
	Tag                string `json:"tag"`
	BundleDigest       string `json:"bundle_digest"`
	SpecDigest         string `json:"spec_digest"`
	Phase              string `json:"phase"`
	Version            uint64 `json:"version"`
	ExpectedRowVersion uint64 `json:"expected_row_version"`
}
type IntentStore interface {
	Prepare(context.Context, PublicationIntent) error
	List(context.Context, string, string) ([]PublicationIntent, error)
	Remove(context.Context, PublicationIntent) error
}
type VersionCloner interface {
	CloneVersion(context.Context, string, string, string, BundleRef) (BundleRef, error)
}
