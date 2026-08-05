package engine

import (
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"go.uber.org/fx"
)

const defaultMaxParallelTools = 4

// Register provides reporting and the complete Agent runtime stack.
var Register = fx.Options(
	fx.Provide(
		fx.Annotate(
			newTerminalReporterRegistration,
			fx.ResultTags(`group:"reporters"`),
		),
		newRegisteredReporter,
		newRegisteredScheduler,
		newRegisteredLoop,
		fx.Annotate(agent.New, fx.As(fx.Self()), fx.As(new(agent.Runner))),
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

func newRegisteredScheduler(registry agent.Registry) *agent.Scheduler {
	return agent.NewScheduler(registry, defaultMaxParallelTools)
}

func newRegisteredLoop(llmProvider ai.Client, scheduler *agent.Scheduler) *agent.Loop {
	return agent.NewLoop(llmProvider, scheduler, true)
}
