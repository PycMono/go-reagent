package agentruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
)

type testRuntime struct{ stopped atomic.Int32 }

func (*testRuntime) Run(context.Context, pi.RunRequest, pi.EventListener) (pi.RunResult, error) {
	return pi.RunResult{}, nil
}
func (*testRuntime) Start(context.Context) error  { return nil }
func (r *testRuntime) Stop(context.Context) error { r.stopped.Add(1); return nil }

type testFactory struct {
	created atomic.Int32
	fail    bool
}

func (f *testFactory) Create(context.Context, Request) (ManagedRuntime, error) {
	f.created.Add(1)
	if f.fail {
		return nil, errors.New("factory failed")
	}
	return &testRuntime{}, nil
}

func request(kind, tenant, conversation string) Request {
	r := Request{Key: Key{Kind: kind, TenantID: tenant, AgentID: "a", ConversationID: conversation, VersionID: "v", SpecDigest: "sha256:x"}}
	r.Version.ID = "v"
	r.Version.AgentID = "a"
	r.Version.TenantID = tenant
	r.Version.SpecDigest = "sha256:x"
	return r
}
func smallOptions() Options {
	return Options{MaxInstances: 4, MaxInstancesPerTenant: 3, ValidationReservedInstances: 1, ValidationReservedInstancesPerTenant: 1, IdleTTL: time.Hour, AcquireTimeout: 30 * time.Millisecond}
}

func TestLeaseReuseAndExclusiveConversation(t *testing.T) {
	f := &testFactory{}
	m, err := NewManager(f, smallOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	a, err := m.Acquire(context.Background(), request("chat", "t", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(context.Background(), request("chat", "t", "c")); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent same conversation: %v", err)
	}
	a.Release()
	a.Release()
	b, err := m.Acquire(context.Background(), request("chat", "t", "c"))
	if err != nil {
		t.Fatal(err)
	}
	b.Release()
	if f.created.Load() != 1 {
		t.Fatal("did not reuse instance")
	}
}

func TestValidationReservationAndTenantFairness(t *testing.T) {
	f := &testFactory{}
	m, _ := NewManager(f, smallOptions())
	defer m.Close(context.Background())
	a, err := m.Acquire(context.Background(), request("chat", "t", "1"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b, err := m.Acquire(context.Background(), request("training", "t", "2"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Release()
	if _, err := m.Acquire(context.Background(), request("chat", "t", "3")); !errors.Is(err, ErrBusy) {
		t.Fatalf("tenant cap: %v", err)
	}
	other, err := m.Acquire(context.Background(), request("chat", "other", "3"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	validation, err := m.Acquire(context.Background(), request("validation", "t", "4"))
	if err != nil {
		t.Fatalf("validation crowded out: %v", err)
	}
	defer validation.Release()
}

func TestFailedCreationAndCanceledWaitReleaseReservations(t *testing.T) {
	f := &testFactory{fail: true}
	m, _ := NewManager(f, smallOptions())
	defer m.Close(context.Background())
	for range 6 {
		if _, err := m.Acquire(context.Background(), request("chat", "t", "c")); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("leaked reservation: %v", err)
		}
	}
	f.fail = false
	a, err := m.Acquire(context.Background(), request("chat", "t", "c"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Acquire(ctx, request("chat", "t", "c")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire %v", err)
	}
}
