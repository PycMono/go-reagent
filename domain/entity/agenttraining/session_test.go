package agenttraining

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTerminalStatesHaveNoOutgoingTransitions(t *testing.T) {
	for _, status := range []Status{Published, Cancelled, Stale, Expired} {
		for _, next := range []Status{Active, Validating, Ready, Published, Cancelled, Stale, Expired} {
			s := Session{Status: status}
			if s.Transition(next) == nil {
				t.Fatalf("%s -> %s accepted", status, next)
			}
		}
	}
}
func TestEditingClearsEvidenceAndPartialCannotBecomeReady(t *testing.T) {
	s := Session{Status: Ready, Validation: json.RawMessage(`{}`)}
	if err := s.Transition(Active); err != nil || len(s.Validation) != 0 {
		t.Fatalf("edit: %#v %v", s, err)
	}
	s.Status = Validating
	s.Validation = json.RawMessage(`{}`)
	s.CandidatePartial = true
	if s.Transition(Ready) == nil {
		t.Fatal("partial candidate ready")
	}
}
func TestExpiryDoesNotReleaseRunningSlot(t *testing.T) {
	s := Session{Status: Active, ExpiresAt: time.Unix(1, 0), Operation: Operation{State: OperationRunning}}
	if !s.IsExpired(time.Unix(1, 0)) {
		t.Fatal("exact expiry admitted")
	}
	if s.Transition(Expired) == nil || s.Terminal() {
		t.Fatal("live writer released")
	}
	s.Operation.State = OperationInterrupted
	if err := s.Transition(Expired); err != nil {
		t.Fatal(err)
	}
}
func TestPublicationRequiresHumanAndReady(t *testing.T) {
	for _, status := range []Status{Active, Ready} {
		s := Session{Status: status}
		if s.Publish(false, "v1") == nil {
			t.Fatal("unconfirmed publication")
		}
		err := s.Publish(true, "v1")
		if (status == Ready) != (err == nil) {
			t.Fatalf("%s: %v", status, err)
		}
	}
}
