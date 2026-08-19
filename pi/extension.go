package pi

import (
	"context"

	"github.com/PycMono/go-reagent/pi/ai"
)

type Extension interface {
	Name() string
	Register(context.Context, ExtensionAPI) error
}

type ExtensionAPI interface {
	RegisterTool(ai.Tool) error
}

type ExtensionCloser interface {
	Close(context.Context) error
}

type extensionAPI struct {
	registry *toolRegistry
	owner    string
}

func (api extensionAPI) RegisterTool(tool ai.Tool) error {
	return api.registry.register(api.owner, tool)
}
