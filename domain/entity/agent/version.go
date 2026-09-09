package agent

import (
	"encoding/json"
	"time"
)

type Version struct {
	ID                      string
	TenantID                string
	AgentID                 string
	Number                  uint64
	BundleCommit            string
	BundleTag               string
	BundleDigest            string
	ModelConfig             json.RawMessage
	ToolPolicy              json.RawMessage
	RuntimeConfig           json.RawMessage
	SpecDigest              string
	Validation              json.RawMessage
	SourceTrainingSessionID *string
	PublishedBy             string
	PublishedAt             time.Time
	ChangeSummary           string
}
