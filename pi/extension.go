package pi

import (
	"context"
	"reflect"

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

// isNilExtension 报告扩展接口是否为空或装有一个类型化 nil 值。
func isNilExtension(extension Extension) bool {
	if extension == nil {
		return true
	}
	value := reflect.ValueOf(extension)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type extensionAPI struct {
	registry *toolRegistry
	owner    string
}

func (api extensionAPI) RegisterTool(tool ai.Tool) error {
	return api.registry.register(api.owner, tool)
}
