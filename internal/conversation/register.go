package conversation

import (
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime agent.Runner, store Store, cfg *config.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}

var Register = fx.Options(fx.Provide(newRegisteredRunner))
