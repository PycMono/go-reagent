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
	if kind != "chat" {
		r.Key.OperationID = "op"
	}
	r.Version.ID = "v"
	r.Version.AgentID = "a"
	r.Version.TenantID = tenant
	r.Version.SpecDigest = "sha256:x"
	return r
}

func TestNextUsesEarliestEligibleWaiter(t *testing.T) {
	m, _ := NewManager(&testFactory{}, smallOptions())
	defer m.Close(context.Background())
	chat := &waiter{request: request("chat", "t", "chat")}
	validation := &waiter{request: request("validation", "t", "validation")}
	m.queue = []*waiter{chat, validation}
	if got := m.next(); got != chat {
		t.Fatalf("next = %p, want earliest eligible %p", got, chat)
	}
}

func TestNonChatRequestRequiresOperationID(t *testing.T) {
	r := request("validation", "t", "c")
	r.Key.OperationID = ""
	if validRequest(r) {
		t.Fatal("validation request without operation ID accepted")
	}
}

func TestValidationReservationSharesCapacityWithoutStartingModel(t *testing.T) {
	f := &testFactory{}
	m, err := NewManager(f, smallOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	release, err := m.ReserveValidation(context.Background(), "t", "a", "validation-op")
	if err != nil {
		t.Fatal(err)
	}
	if f.created.Load() != 0 {
		t.Fatal("validation started an unnecessary model runtime")
	}
	m.mu.Lock()
	count := len(m.entries)
	m.mu.Unlock()
	if count != 1 {
		t.Fatal("validation not counted")
	}
	release()
	release()
	m.mu.Lock()
	count = len(m.entries)
	m.mu.Unlock()
	if count != 0 {
		t.Fatal("validation capacity leaked")
	}
}

func TestIdleRuntimeStopsAfterTTL(t *testing.T) {
	runtime := &testRuntime{}
	factory := &blockingFactory{entered: make(chan struct{}, 1), unblock: make(chan struct{}, 1), runtime: runtime}
	factory.unblock <- struct{}{}
	opts := smallOptions()
	opts.IdleTTL = 10 * time.Millisecond
	m, _ := NewManager(factory, opts)
	lease, err := m.Acquire(context.Background(), request("chat", "t", "idle"))
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	deadline := time.Now().Add(time.Second)
	for runtime.stopped.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runtime.stopped.Load() != 1 {
		t.Fatal("idle runtime was not stopped")
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
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

type blockingFactory struct {
	entered chan struct{}
	unblock chan struct{}
	runtime *testRuntime
}

func (f *blockingFactory) Create(ctx context.Context, _ Request) (ManagedRuntime, error) {
	f.entered <- struct{}{}
	select {
	case <-f.unblock:
		return f.runtime, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestCreatingRuntimeReservesCapacityAndCloseCancelsIt(t *testing.T) {
	f := &blockingFactory{entered: make(chan struct{}, 1), unblock: make(chan struct{}), runtime: &testRuntime{}}
	opts := smallOptions()
	opts.MaxInstancesPerTenant = 2
	m, _ := NewManager(f, opts)
	result := make(chan error, 1)
	go func() { _, err := m.Acquire(context.Background(), request("chat", "t", "first")); result <- err }()
	<-f.entered
	if _, err := m.Acquire(context.Background(), request("chat", "t", "second")); !errors.Is(err, ErrBusy) {
		t.Fatalf("creation not counted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("factory not canceled: %v", err)
	}
}

type brokenRuntime struct{ testRuntime }

func (*brokenRuntime) Stop(context.Context) error { return errors.New("process still alive") }

type brokenFactory struct{}

func (brokenFactory) Create(context.Context, Request) (ManagedRuntime, error) {
	return &brokenRuntime{}, nil
}
func TestCloseReportsQuarantinedProcess(t *testing.T) {
	m, _ := NewManager(brokenFactory{}, smallOptions())
	lease, err := m.Acquire(context.Background(), request("chat", "t", "c"))
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if err := m.Close(context.Background()); err == nil {
		t.Fatal("stop failure lost")
	}
	if err := m.Close(context.Background()); err == nil {
		t.Fatal("quarantined process silently forgotten")
	}
}
