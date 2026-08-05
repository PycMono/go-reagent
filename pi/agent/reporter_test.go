package agent_test

import (
	"context"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
)

type reporterFunc func(context.Context, agent.AgentEvent)

func (f reporterFunc) Report(ctx context.Context, event agent.AgentEvent) {
	f(ctx, event)
}

func TestMultiReporterSortsAndIsolatesPanic(t *testing.T) {
	var got []string
	reporter := agent.NewMultiReporter([]agent.ReporterRegistration{
		{Name: "z", Order: 20, Reporter: reporterFunc(func(context.Context, agent.AgentEvent) { got = append(got, "z") })},
		{Name: "panic", Order: 10, Reporter: reporterFunc(func(context.Context, agent.AgentEvent) { panic("boom") })},
		{Name: "a", Order: 20, Reporter: reporterFunc(func(context.Context, agent.AgentEvent) { got = append(got, "a") })},
	})
	reporter.Report(context.Background(), agent.NewThinkingEvent())
	if want := []string{"a", "z"}; !slices.Equal(got, want) {
		t.Fatalf("reported to = %v, want %v", got, want)
	}
}
