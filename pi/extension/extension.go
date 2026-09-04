// Package extension 定义 Agent 的扩展契约：扩展在启动期经 API 注册
// Tool，可实现 Closer 参与停机清理。
package extension

import (
	"context"
	"reflect"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

type Extension interface {
	Name() string
	Register(context.Context, API) error
}

type API interface {
	RegisterTool(ai.Tool) error
}

type Closer interface {
	Close(context.Context) error
}

// Extensions 是一组扩展，便于作为整体在装配层与 Runtime 之间传递。
type Extensions []Extension

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

type api struct {
	registry *toolexec.Registry
	owner    string
}

func (a api) RegisterTool(tool ai.Tool) error {
	return a.registry.Register(a.owner, tool)
}
