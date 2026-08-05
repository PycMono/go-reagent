package transport

import (
	"context"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type namedRegisterReporter struct {
	name string
	got  *[]string
}

func (r *namedRegisterReporter) Report(context.Context, agent.AgentEvent) {
	*r.got = append(*r.got, r.name)
}

func TestModuleSortsReversedReporterGroupByOrderThenName(t *testing.T) {
	var got []string
	registration := func(name string) agent.ReporterRegistration {
		return agent.ReporterRegistration{Name: name, Order: 50, Reporter: &namedRegisterReporter{name: name, got: &got}}
	}
	var reporter agent.Reporter
	app := fxtest.New(t,
		fx.Supply(&config.Config{}),
		Module,
		fx.Provide(
			fx.Annotate(func() agent.ReporterRegistration { return registration("zeta") }, fx.ResultTags(`group:"reporters"`)),
			fx.Annotate(func() agent.ReporterRegistration { return registration("alpha") }, fx.ResultTags(`group:"reporters"`)),
		),
		fx.Populate(&reporter),
	)
	app.RequireStart()
	defer app.RequireStop()

	reporter.Report(context.Background(), agent.NewThinkingEvent())
	if !slices.Equal(got, []string{"alpha", "zeta"}) {
		t.Fatalf("reporter sequence = %v, want alpha,zeta", got)
	}
}
