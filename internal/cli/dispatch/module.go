package dispatch

import (
	"github.com/PycMono/go-reagent/agent"
	"go.uber.org/fx"
)

// Module provides terminal and optional WeCom reporting.
var Module = fx.Options(
	fx.Provide(
		fx.Annotate(
			newTerminalReporterRegistration,
			fx.ResultTags(`group:"reporters"`),
		),
		fx.Annotate(
			NewReporterRegistrations,
			fx.ResultTags(`group:"reporters,flatten"`),
		),
		newRegisteredReporter,
	),
)

type reporterParams struct {
	fx.In

	Registrations []agent.ReporterRegistration `group:"reporters"`
}

func newTerminalReporterRegistration() agent.ReporterRegistration {
	return agent.ReporterRegistration{Name: "terminal", Order: 100, Reporter: NewTerminalReporter()}
}

func newRegisteredReporter(params reporterParams) agent.Reporter {
	return agent.NewMultiReporter(params.Registrations)
}
