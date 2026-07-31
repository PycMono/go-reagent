package engine

import (
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
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

	Registrations []ReporterRegistration `group:"reporters"`
}

func newTerminalReporterRegistration() ReporterRegistration {
	return ReporterRegistration{Name: "terminal", Order: 100, Reporter: NewTerminalReporter()}
}

func newRegisteredReporter(params reporterParams) Reporter {
	return NewMultiReporter(params.Registrations)
}

func newRegisteredToolScheduler(registry tools.Registry) *ToolScheduler {
	return NewToolScheduler(registry, defaultMaxParallelTools)
}

func newRegisteredAgentLoop(llmProvider provider.LLMProvider, scheduler *ToolScheduler) *AgentLoop {
	return NewAgentLoop(llmProvider, scheduler, true)
}
