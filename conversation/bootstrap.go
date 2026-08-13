package conversation

import (
	"github.com/PycMono/go-reagent/config"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime agent.Runner, store conversationrepo.Store, cfg *config.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}

var Module = fx.Options(fx.Provide(newRegisteredRunner))
