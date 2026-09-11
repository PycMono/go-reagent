package agentbundle

import (
	"context"
	"errors"
	"time"
)

var ErrRecoveryRequired = errors.New("candidate recovery required before further writes")

type Candidate struct{ TenantID, AgentID, TrainingID, Head string }
type CheckpointMetadata struct {
	RunID   string    `json:"run_id"`
	ActorID string    `json:"actor_id"`
	At      time.Time `json:"at"`
	Partial bool      `json:"partial"`
}
type Checkpoint struct {
	ID       string             `json:"id"`
	Head     string             `json:"head"`
	Metadata CheckpointMetadata `json:"metadata"`
}
type Diff struct {
	Text         string `json:"text"`
	Truncated    bool   `json:"truncated"`
	ChangedPaths int    `json:"changed_paths"`
}

// CandidateStore operations require the caller to hold the durable operation slot
// and confirm that all author processes have stopped before filesystem access.
type CandidateStore interface {
	CreateCandidate(context.Context, string, string, string, BundleRef) (string, error)
	Checkpoint(context.Context, Candidate, CheckpointMetadata) (Checkpoint, error)
	ListCheckpoints(context.Context, Candidate, string, int) ([]Checkpoint, string, error)
	Restore(context.Context, Candidate, string) (Checkpoint, error)
	Diff(context.Context, Candidate, string, int) (Diff, error)
}

// Only an application-authorized, database-unreferenced preparation may be removed.
type PreparationArtifacts struct {
	TenantID, AgentID, VersionID string
	Ref                          BundleRef
}
type PreparationCleaner interface {
	CleanupPreparation(context.Context, PreparationArtifacts) error
}
