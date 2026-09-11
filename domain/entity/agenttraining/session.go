package agenttraining

import (
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	Active     Status = "active"
	Validating Status = "validating"
	Ready      Status = "ready"
	Published  Status = "published"
	Cancelled  Status = "cancelled"
	Stale      Status = "stale"
	Expired    Status = "expired"
)

var ErrTransition = errors.New("invalid training transition")

type Session struct {
	ID, TenantID, AgentID, AdminUserID string
	ConversationID, BaseVersionID      string
	CandidateHead                      string
	CandidateConfig                    json.RawMessage
	CandidatePartial                   bool
	Status                             Status
	RowVersion                         uint64
	ExpiresAt                          time.Time
	Validation                         json.RawMessage
	Operation                          Operation
	ResultVersionID                    *string
	CreatedAt, UpdatedAt               time.Time
}

func (s Session) Terminal() bool {
	return s.Status == Published || s.Status == Cancelled || s.Status == Stale || s.Status == Expired
}
func (s Session) Running() bool                { return s.Operation.State == OperationRunning }
func (s Session) IsExpired(now time.Time) bool { return !now.Before(s.ExpiresAt) }
func (s *Session) Transition(next Status) error {
	if s.Terminal() {
		return ErrTransition
	}
	valid := false
	switch s.Status {
	case Active:
		valid = next == Active || next == Validating || next == Cancelled || next == Stale || next == Expired
	case Validating:
		valid = next == Active || next == Ready || next == Cancelled || next == Stale || next == Expired
	case Ready:
		valid = next == Active || next == Validating || next == Published || next == Cancelled || next == Stale || next == Expired
	}
	if !valid {
		return ErrTransition
	}
	if next == Ready && (s.CandidatePartial || len(s.Validation) == 0) {
		return ErrTransition
	}
	if (next == Cancelled || next == Expired || next == Stale) && s.Running() {
		return ErrTransition
	}
	s.Status = next
	if next != Ready && next != Published {
		s.Validation = nil
	}
	return nil
}
func (s *Session) Publish(confirmed bool, versionID string) error {
	if !confirmed || versionID == "" || s.Status != Ready || s.CandidatePartial {
		return ErrTransition
	}
	if err := s.Transition(Published); err != nil {
		return err
	}
	s.ResultVersionID = &versionID
	return nil
}
