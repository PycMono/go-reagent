package pi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"go.uber.org/fx"
)

type extensionRuntimeParams struct {
	fx.In

	Lifecycle  fx.Lifecycle
	Registry   *toolRegistry
	Extensions []Extension `group:"agent_extensions"`
}

type extensionRuntime struct {
	registry   *toolRegistry
	extensions []Extension
	started    []Extension
}

func newExtensionRuntime(params extensionRuntimeParams) (*extensionRuntime, error) {
	extensions := append([]Extension(nil), params.Extensions...)
	seen := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		if isNilExtension(extension) {
			return nil, errors.New("extension must not be nil")
		}
		name := strings.TrimSpace(extension.Name())
		if name == "" {
			return nil, errors.New("extension name must not be empty")
		}
		if extension.Name() != name {
			return nil, fmt.Errorf("extension name %q must not contain surrounding whitespace", extension.Name())
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("extension %q is already registered", name)
		}
		seen[name] = struct{}{}
	}
	sort.Slice(extensions, func(i, j int) bool { return extensions[i].Name() < extensions[j].Name() })

	runtime := &extensionRuntime{registry: params.Registry, extensions: extensions}
	params.Lifecycle.Append(fx.Hook{OnStart: runtime.start, OnStop: runtime.stop})
	return runtime, nil
}

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

func (runtime *extensionRuntime) start(ctx context.Context) error {
	for _, extension := range runtime.extensions {
		name := extension.Name()
		api := extensionAPI{registry: runtime.registry, owner: name}
		if err := extension.Register(ctx, api); err != nil {
			runtime.registry.rollback(name)
			cleanupErr := closeExtension(ctx, extension)
			for index := len(runtime.started) - 1; index >= 0; index-- {
				started := runtime.started[index]
				runtime.registry.rollback(started.Name())
				cleanupErr = errors.Join(cleanupErr, closeExtension(ctx, started))
			}
			runtime.started = nil
			return errors.Join(fmt.Errorf("start extension %q: %w", name, err), cleanupErr)
		}
		runtime.started = append(runtime.started, extension)
	}
	runtime.registry.freeze()
	return nil
}

func (runtime *extensionRuntime) stop(ctx context.Context) error {
	var joined error
	for index := len(runtime.started) - 1; index >= 0; index-- {
		extension := runtime.started[index]
		joined = errors.Join(joined, closeExtension(ctx, extension))
	}
	runtime.started = nil
	return joined
}

func closeExtension(ctx context.Context, extension Extension) error {
	closer, ok := extension.(ExtensionCloser)
	if !ok {
		return nil
	}
	if err := closer.Close(ctx); err != nil {
		return fmt.Errorf("close extension %q: %w", extension.Name(), err)
	}
	return nil
}
