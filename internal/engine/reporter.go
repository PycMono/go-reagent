package engine

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/PycMono/go-reagent/internal/schema"
)

// Reporter receives user-facing Agent lifecycle events.
type Reporter interface {
	Report(context.Context, schema.AgentEvent)
}

// ReporterRegistration describes one deterministic Reporter subscriber.
type ReporterRegistration struct {
	Name     string
	Order    int
	Reporter Reporter
}

type multiReporter struct {
	registrations []ReporterRegistration
}

// NewMultiReporter broadcasts events in Order then Name order.
func NewMultiReporter(registrations []ReporterRegistration) Reporter {
	filtered := append([]ReporterRegistration(nil), registrations...)
	slices.SortFunc(filtered, func(a, b ReporterRegistration) int {
		if order := cmp.Compare(a.Order, b.Order); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return &multiReporter{registrations: filtered}
}

func (r *multiReporter) Report(ctx context.Context, event schema.AgentEvent) {
	for _, registration := range r.registrations {
		if strings.TrimSpace(registration.Name) == "" || registration.Reporter == nil {
			continue
		}
		reportSafely(ctx, registration.Reporter, event)
	}
}

func reportSafely(ctx context.Context, reporter Reporter, event schema.AgentEvent) {
	defer func() {
		_ = recover()
	}()
	reporter.Report(ctx, event)
}
