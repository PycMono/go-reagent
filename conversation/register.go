package conversation

import (
	"github.com/PycMono/go-reagent/config"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

func newRegisteredRunner(runtime pi.Runner, repository conversationrepo.IConversationRepository, cfg *config.Config) Runner {
	return NewRunner(runtime, repository, cfg.Conversation.HistoryMessageLimit, cfg.Agent.Limits)
}

var Register = fx.Options(fx.Provide(newRegisteredRunner))
