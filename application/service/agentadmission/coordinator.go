// Package agentadmission serializes platform mutations without blocking chat.
package agentadmission

import (
	"context"
	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"sync"
)

type Coordinator struct {
	mu    sync.Mutex
	slots map[string]string
}

func New() *Coordinator { return &Coordinator{slots: map[string]string{}} }

// Reserve claims an in-process mutation slot. The database operation record
// separately protects recovery; this reservation never outlives process restart.
func (c *Coordinator) Reserve(ctx context.Context, tenant, agent, operation string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !identity.ValidID(tenant) || !identity.ValidID(agent) || !identity.ValidID(operation) {
		return nil, commonerrors.ErrInvalidParam
	}
	key := tenant + "\x00" + agent
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.slots[key]; ok {
		return nil, commonerrors.ErrConflict
	}
	c.slots[key] = operation
	var once sync.Once
	return func() { once.Do(func() { c.mu.Lock(); delete(c.slots, key); c.mu.Unlock() }) }, nil
}
