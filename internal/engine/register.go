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
		newRegisteredToolScheduler,
		newRegisteredAgentLoop,
		NewAgentRuntime,
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

func newRegisteredToolScheduler(registry agent.Registry) *ToolScheduler {
	return NewToolScheduler(registry, defaultMaxParallelTools)
}

func newRegisteredAgentLoop(llmProvider ai.Client, scheduler *ToolScheduler) *AgentLoop {
	return NewAgentLoop(llmProvider, scheduler, true)
}
