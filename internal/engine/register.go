package engine

import (
	"github.com/PycMono/go-reagent/agent"
	"go.uber.org/fx"
)

// Register provides reporting and the complete Agent runtime stack.
var Register = fx.Options(
	fx.Provide(
		fx.Annotate(
			newTerminalReporterRegistration,
			fx.ResultTags(`group:"reporters"`),
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
