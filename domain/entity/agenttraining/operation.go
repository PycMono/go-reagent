package agenttraining

import (
	"encoding/json"
	"time"
)

type OperationState string

const (
	OperationRunning     OperationState = "running"
	OperationCompleted   OperationState = "completed"
	OperationInterrupted OperationState = "interrupted"
	OperationFailed      OperationState = "failed"
)

type PreparedVersion struct {
	ID           string `json:"id"`
	Tag          string `json:"tag"`
	BundleDigest string `json:"bundle_digest"`
	SpecDigest   string `json:"spec_digest"`
	Version      uint64 `json:"version"`
}
type Operation struct {
	ID              string           `json:"id"`
	Kind            string           `json:"kind"`
	RequestDigest   string           `json:"request_digest"`
	State           OperationState   `json:"state"`
	StartedAt       time.Time        `json:"started_at"`
	FinishedAt      *time.Time       `json:"finished_at,omitempty"`
	Result          json.RawMessage  `json:"result,omitempty"`
	PreparedVersion *PreparedVersion `json:"prepared_version,omitempty"`
}
