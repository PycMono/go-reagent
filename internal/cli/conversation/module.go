package conversation

import (
	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime agent.Runner, store Store, cfg *reagent.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}

var Module = fx.Options(fx.Provide(newRegisteredRunner))
