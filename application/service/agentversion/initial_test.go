package agentversion

import (
	"context"
	"errors"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"testing"
	"time"
)

type verifyStore struct {
	bundle.Store
	err    error
	called bool
}

func (s *verifyStore) Verify(context.Context, string, string, bundle.BundleRef) error {
	s.called = true
	return s.err
}
func TestValidatorFailsClosedAndBindsEvidence(t *testing.T) {
	s, _ := ParseSnapshot([]byte(validSnapshotJSON))
	ref := bundle.BundleRef{Commit: "head", Tag: "versions/v", Digest: "sha256:" + stringRepeat("a", 64)}
	store := &verifyStore{}
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	smoke := false
	v, err := NewValidator(store, func(context.Context, Snapshot) error { return nil }, func(context.Context, string, Snapshot) error { smoke = true; return nil }, func(context.Context) (time.Time, error) { return now, nil }, ValidationPolicy{Version: "v1", Environment: "isolated-v1", Timeout: time.Second, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	r, err := v.Validate(context.Background(), "t", "a", ref, "/validation", s)
	if err != nil || !r.Passed || !smoke || !store.called || r.Head != ref.Commit || r.ValidUntil != now.Add(time.Minute) {
		t.Fatalf("%+v %v", r, err)
	}
	store.err = errors.New("tampered")
	smoke = false
	if _, err = v.Validate(context.Background(), "t", "a", ref, "/validation", s); err == nil || smoke {
		t.Fatal("tampered bundle accepted")
	}
	if _, err = NewValidator(store, nil, nil, nil, ValidationPolicy{}); err == nil {
		t.Fatal("missing validation dependencies accepted")
	}
}
func stringRepeat(s string, n int) string {
	r := ""
	for i := 0; i < n; i++ {
		r += s
	}
	return r
}

type captureFake struct{ calls int }

func (c *captureFake) CaptureAgentSnapshot(string, string, []string) (Snapshot, error) {
	c.calls++
	return ParseSnapshot([]byte(validSnapshotJSON))
}
func TestInitialBuilderRejectsMissingDependencies(t *testing.T) {
	if _, err := NewInitialBuilder(nil, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("accepted missing dependencies")
	}
}

func (s *verifyStore) VerifyValidationWorkspace(context.Context, string, string, string, bundle.BundleRef) error {
	return s.err
}
