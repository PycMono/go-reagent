package agent

import (
	"context"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"testing"
	"time"
)

func TestPublicationRejectsExpiredEvidenceBeforeSQL(t *testing.T) {
	r, mock := newRepositoryTest(t)
	evidence := agentversion.ValidationReport{Passed: true, ValidUntil: time.Time{}}
	if err := r.ActivateVersion(context.Background(), "t", "a", "v", 1, evidence); err == nil {
		t.Fatal("expired activation accepted")
	}
	v := initialVersion()
	if err := r.CommitModelRelease(context.Background(), "t", "a", 1, "base", v, evidence); err == nil {
		t.Fatal("expired release accepted")
	}
	assertExpectations(t, mock)
}
