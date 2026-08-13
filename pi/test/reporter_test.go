package test

import (
	"context"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/pi"
)

type reporterFunc func(context.Context, pi.AgentEvent)

func (f reporterFunc) Report(ctx context.Context, event pi.AgentEvent) {
	f(ctx, event)
}

func TestMultiReporterSortsAndIsolatesPanic(t *testing.T) {
	var got []string
	reporter := pi.NewMultiReporter([]pi.ReporterRegistration{
		{Name: "z", Order: 20, Reporter: reporterFunc(func(context.Context, pi.AgentEvent) { got = append(got, "z") })},
		{Name: "panic", Order: 10, Reporter: reporterFunc(func(context.Context, pi.AgentEvent) { panic("boom") })},
		{Name: "a", Order: 20, Reporter: reporterFunc(func(context.Context, pi.AgentEvent) { got = append(got, "a") })},
	})
	reporter.Report(context.Background(), pi.NewThinkingEvent())
	if want := []string{"a", "z"}; !slices.Equal(got, want) {
		t.Fatalf("reported to = %v, want %v", got, want)
	}
}
