package transport

import (
	"context"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type namedRegisterReporter struct {
	name string
	got  *[]string
}

func (r *namedRegisterReporter) Report(context.Context, pi.AgentEvent) {
	*r.got = append(*r.got, r.name)
}

func TestRegisterSortsReversedReporterGroupByOrderThenName(t *testing.T) {
	var got []string
	registration := func(name string) pi.ReporterRegistration {
		return pi.ReporterRegistration{Name: name, Order: 50, Reporter: &namedRegisterReporter{name: name, got: &got}}
	}
	var reporter pi.Reporter
	app := fxtest.New(t,
		fx.Supply(&config.Config{}),
		Register,
		fx.Provide(
			fx.Annotate(func() pi.ReporterRegistration { return registration("zeta") }, fx.ResultTags(`group:"reporters"`)),
			fx.Annotate(func() pi.ReporterRegistration { return registration("alpha") }, fx.ResultTags(`group:"reporters"`)),
		),
		fx.Populate(&reporter),
	)
	app.RequireStart()
	defer app.RequireStop()

	reporter.Report(context.Background(), pi.NewThinkingEvent())
	if !slices.Equal(got, []string{"alpha", "zeta"}) {
		t.Fatalf("reporter sequence = %v, want alpha,zeta", got)
	}
}
