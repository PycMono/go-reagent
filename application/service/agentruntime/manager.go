package agentruntime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/pi"
)

var ErrBusy = errors.New("agent runtime capacity busy")
var ErrClosed = errors.New("agent runtime manager closed")

type Key struct{ Kind, TenantID, AgentID, ConversationID, VersionID, OperationID, SpecDigest string }
type Request struct {
	Key     Key
	Version agent.Version
	// CandidateRoot is server-owned and is never bound from an HTTP request.
	CandidateRoot string
}
type ManagedRuntime interface {
	pi.Runner
	Start(context.Context) error
	Stop(context.Context) error
}
type Factory interface {
	Create(context.Context, Request) (ManagedRuntime, error)
}
type Lease struct {
	Runner  pi.Runner
	Release func()
	Stop    func() error
}
type Options struct {
	MaxInstances, MaxInstancesPerTenant                               int
	ValidationReservedInstances, ValidationReservedInstancesPerTenant int
	IdleTTL, AcquireTimeout                                           time.Duration
}

func DefaultOptions() Options { return Options{32, 8, 2, 1, 15 * time.Minute, 5 * time.Second} }
func (o Options) Validate() error {
	if o.MaxInstances < 2 || o.MaxInstancesPerTenant < 2 || o.MaxInstancesPerTenant > o.MaxInstances ||
		o.ValidationReservedInstances < 1 || o.ValidationReservedInstances >= o.MaxInstances ||
		o.ValidationReservedInstancesPerTenant < 1 || o.ValidationReservedInstancesPerTenant >= o.MaxInstancesPerTenant ||
		o.ValidationReservedInstancesPerTenant > o.ValidationReservedInstances || o.IdleTTL <= 0 || o.AcquireTimeout <= 0 {
		return errors.New("invalid agent runtime limits")
	}
	return nil
}

type entry struct {
	key                                    Key
	runtime                                ManagedRuntime
	leased, creating, closing, quarantined bool
	stopErr                                error
	idleSince                              time.Time
	timer                                  *time.Timer
}
type waiter struct{ request Request }
type Manager struct {
	mu       sync.Mutex
	factory  Factory
	options  Options
	entries  map[Key]*entry
	queue    []*waiter
	changed  chan struct{}
	closed   bool
	lifetime context.Context
	stop     context.CancelFunc
}

func NewManager(factory Factory, options Options) (*Manager, error) {
	if factory == nil {
		return nil, errors.New("runtime factory required")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{factory: factory, options: options, entries: make(map[Key]*entry), changed: make(chan struct{}), lifetime: ctx, stop: cancel}, nil
}
func (m *Manager) signal() { close(m.changed); m.changed = make(chan struct{}) }
func validRequest(r Request) bool {
	k := r.Key
	return (k.Kind == "chat" || k.Kind == "training" || k.Kind == "preview" || k.Kind == "validation") && identity.ValidID(k.TenantID) && identity.ValidID(k.AgentID) &&
		identity.ValidID(k.ConversationID) && identity.ValidID(k.VersionID) && k.SpecDigest != "" && r.Version.ID == k.VersionID &&
		r.Version.AgentID == k.AgentID && r.Version.TenantID == k.TenantID && r.Version.SpecDigest == k.SpecDigest &&
		(k.Kind == "chat" || identity.ValidID(k.OperationID))
}
func (m *Manager) Acquire(ctx context.Context, r Request) (*Lease, error) {
	if !validRequest(r) {
		return nil, errors.New("runtime key does not match version")
	}
	return m.acquire(ctx, r, false)
}

// ReserveValidation uses the same quotas/FIFO as model instances. Structural
// checks and isolated interpreters need capacity but do not need a model client.
func (m *Manager) ReserveValidation(ctx context.Context, tenant, agentID, operation string) (func(), error) {
	if !identity.ValidID(tenant) || !identity.ValidID(agentID) || !identity.ValidID(operation) {
		return nil, errors.New("invalid validation scope")
	}
	r := Request{Key: Key{Kind: "validation", TenantID: tenant, AgentID: agentID, ConversationID: operation, OperationID: operation}}
	lease, err := m.acquire(ctx, r, true)
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}

func (m *Manager) acquire(ctx context.Context, r Request, reservation bool) (*Lease, error) {
	waitCtx, cancel := context.WithTimeout(ctx, m.options.AcquireTimeout)
	defer cancel()
	w := &waiter{request: r}
	m.mu.Lock()
	m.queue = append(m.queue, w)
	m.signal()
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		for i, v := range m.queue {
			if v == w {
				m.queue = append(m.queue[:i], m.queue[i+1:]...)
				break
			}
		}
		m.signal()
		m.mu.Unlock()
	}()
	for {
		if err := waitCtx.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrBusy
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrClosed
		}
		if m.next() == w {
			if e := m.entries[r.Key]; e != nil && m.reusable(e) {
				if time.Since(e.idleSince) < m.options.IdleTTL {
					if e.timer != nil {
						e.timer.Stop()
						e.timer = nil
					}
					e.leased = true
					lease := m.lease(e)
					m.mu.Unlock()
					return lease, nil
				}
				e.closing = true
				m.mu.Unlock()
				m.closeEntry(e)
				continue
			}
			if m.fits(r.Key) {
				e := &entry{key: r.Key, creating: true, leased: true}
				m.entries[r.Key] = e
				if reservation {
					e.creating = false
					lease := m.lease(e)
					m.mu.Unlock()
					return &Lease{Release: func() { _ = lease.Stop() }, Stop: lease.Stop}, nil
				}
				m.mu.Unlock()
				return m.create(ctx, e, r)
			}
			if e := m.evictable(r.Key); e != nil {
				e.closing = true
				m.mu.Unlock()
				m.closeEntry(e)
				continue
			}
		}
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-changed:
		case <-waitCtx.Done():
		}
	}
}
func (m *Manager) reusable(e *entry) bool {
	return !e.leased && !e.creating && !e.closing && !e.quarantined
}
func (m *Manager) occupiedConversation(k Key) bool {
	for _, e := range m.entries {
		if e.key.TenantID == k.TenantID && e.key.ConversationID == k.ConversationID && e.key.Kind == k.Kind && (e.leased || e.creating || e.closing || e.quarantined) {
			return true
		}
	}
	return false
}
func (m *Manager) next() *waiter {
	for _, w := range m.queue {
		k := w.request.Key
		if m.occupiedConversation(k) {
			continue
		}
		e := m.entries[k]
		if (e == nil || !m.reusable(e)) && !m.fits(k) && m.evictable(k) == nil {
			continue
		}
		return w
	}
	return nil
}
func (m *Manager) fits(k Key) bool {
	total, tenant, ordinary, tenantOrdinary := 0, 0, 0, 0
	for _, e := range m.entries {
		total++
		if e.key.TenantID == k.TenantID {
			tenant++
		}
		if e.key.Kind != "validation" {
			ordinary++
			if e.key.TenantID == k.TenantID {
				tenantOrdinary++
			}
		}
	}
	if total >= m.options.MaxInstances || tenant >= m.options.MaxInstancesPerTenant {
		return false
	}
	return k.Kind == "validation" || (ordinary < m.options.MaxInstances-m.options.ValidationReservedInstances && tenantOrdinary < m.options.MaxInstancesPerTenant-m.options.ValidationReservedInstancesPerTenant)
}
func (m *Manager) evictable(k Key) *entry {
	var oldest *entry
	// If this tenant's quota is exhausted, evicting another tenant cannot help.
	tenant, ordinary := 0, 0
	for _, e := range m.entries {
		if e.key.TenantID == k.TenantID {
			tenant++
			if e.key.Kind != "validation" {
				ordinary++
			}
		}
	}
	ownOnly := tenant >= m.options.MaxInstancesPerTenant || (k.Kind != "validation" && ordinary >= m.options.MaxInstancesPerTenant-m.options.ValidationReservedInstancesPerTenant)
	for _, e := range m.entries {
		if !m.reusable(e) || (ownOnly && e.key.TenantID != k.TenantID) {
			continue
		}
		if oldest == nil || e.idleSince.Before(oldest.idleSince) {
			oldest = e
		}
	}
	return oldest
}
func (m *Manager) lease(e *entry) *Lease {
	var once sync.Once
	var stopErr error
	return &Lease{Runner: e.runtime, Stop: func() error {
		once.Do(func() {
			m.mu.Lock()
			e.leased = false
			e.closing = true
			m.mu.Unlock()
			stopErr = m.closeEntry(e)
		})
		return stopErr
	}, Release: func() {
		once.Do(func() {
			m.mu.Lock()
			e.leased = false
			e.idleSince = time.Now()
			if !m.closed {
				idleSince := e.idleSince
				e.timer = time.AfterFunc(m.options.IdleTTL, func() { m.expire(e, idleSince) })
			}
			m.signal()
			m.mu.Unlock()
		})
	}}
}

func (m *Manager) expire(e *entry, idleSince time.Time) {
	select {
	case <-m.lifetime.Done():
		return
	default:
	}
	m.mu.Lock()
	if m.closed || m.entries[e.key] != e || !m.reusable(e) || !e.idleSince.Equal(idleSince) {
		m.mu.Unlock()
		return
	}
	e.closing = true
	e.timer = nil
	m.mu.Unlock()
	m.closeEntry(e)
}
func (m *Manager) create(ctx context.Context, e *entry, r Request) (*Lease, error) {
	createCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	detach := context.AfterFunc(m.lifetime, cancel)
	defer detach()
	runtime, err := m.factory.Create(createCtx, r)
	if err == nil && runtime == nil {
		err = errors.New("runtime factory returned nil")
	}
	if err == nil {
		err = runtime.Start(createCtx)
	}
	m.mu.Lock()
	e.runtime = runtime
	e.creating = false
	if err == nil && createCtx.Err() != nil {
		err = createCtx.Err()
	}
	if err == nil && m.closed {
		err = ErrClosed
	}
	if err == nil {
		lease := m.lease(e)
		m.signal()
		m.mu.Unlock()
		return lease, nil
	}
	e.leased = false
	e.closing = true
	m.mu.Unlock()
	cleanupErr := m.closeEntry(e)
	return nil, errors.Join(err, cleanupErr)
}
func (m *Manager) closeEntry(e *entry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if e.runtime != nil {
		err = e.runtime.Stop(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.entries, e.key)
	} else {
		e.closing = false
		e.quarantined = true
		e.stopErr = err
	}
	m.signal()
	return err
}

// Close cancels queued/creating work and waits for current leases to be returned.
// A failed Stop retains its occupied slot; it is never silently reused.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.stop()
	for _, e := range m.entries {
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
	}
	m.signal()
	m.mu.Unlock()
	for {
		m.mu.Lock()
		var candidate *entry
		for _, e := range m.entries {
			if m.reusable(e) {
				candidate = e
				e.closing = true
				break
			}
		}
		if candidate != nil {
			m.mu.Unlock()
			m.closeEntry(candidate)
			continue
		}
		active := false
		var cleanupErrors error
		for _, e := range m.entries {
			cleanupErrors = errors.Join(cleanupErrors, e.stopErr)
			if e.leased || e.creating || e.closing {
				active = true
			}
		}
		changed := m.changed
		m.mu.Unlock()
		if !active {
			return cleanupErrors
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.Join(cleanupErrors, ctx.Err())
		}
	}
}
