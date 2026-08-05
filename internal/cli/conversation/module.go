package conversation

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime agent.Runner, store Store, cfg *config.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}

var Module = fx.Options(fx.Provide(newRegisteredRunner))
