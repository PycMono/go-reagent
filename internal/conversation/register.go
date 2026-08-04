package conversation

import (
	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime engine.AgentRuntime, store Store, cfg *config.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}

var Register = fx.Options(fx.Provide(newRegisteredRunner))
