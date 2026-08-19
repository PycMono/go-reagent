package chat

import (
	"github.com/PycMono/go-reagent/pi/ai"
	"go.uber.org/fx"
)

var Register = fx.Options(fx.Provide(
	newSystemClock,
	fx.Annotate(newCurrentTimeTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
	fx.Annotate(newCalculateTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
))
