package extension

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/toolexec"
	"go.uber.org/fx"
)

type Params struct {
	fx.In

	Lifecycle  fx.Lifecycle
	Registry   *toolexec.Registry
	Extensions []Extension `group:"agent_extensions"`
}

type Runtime struct {
	registry   *toolexec.Registry
	extensions []Extension
	started    []Extension
}

func NewRuntime(params Params) (*Runtime, error) {
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

	runtime := &Runtime{registry: params.Registry, extensions: extensions}
	params.Lifecycle.Append(fx.Hook{OnStart: runtime.start, OnStop: runtime.stop})
	return runtime, nil
}

func (runtime *Runtime) start(ctx context.Context) error {
	for _, extension := range runtime.extensions {
		name := extension.Name()
		extAPI := api{registry: runtime.registry, owner: name}
		if err := extension.Register(ctx, extAPI); err != nil {
			runtime.registry.Rollback(name)
			cleanupErr := closeExtension(ctx, extension)
			for index := len(runtime.started) - 1; index >= 0; index-- {
				started := runtime.started[index]
				runtime.registry.Rollback(started.Name())
				cleanupErr = errors.Join(cleanupErr, closeExtension(ctx, started))
			}
			runtime.started = nil
			return errors.Join(fmt.Errorf("start extension %q: %w", name, err), cleanupErr)
		}
		runtime.started = append(runtime.started, extension)
	}
	runtime.registry.Freeze()
	return nil
}

func (runtime *Runtime) stop(ctx context.Context) error {
	var joined error
	for index := len(runtime.started) - 1; index >= 0; index-- {
		extension := runtime.started[index]
		joined = errors.Join(joined, closeExtension(ctx, extension))
	}
	runtime.started = nil
	return joined
}

func closeExtension(ctx context.Context, extension Extension) error {
	closer, ok := extension.(Closer)
	if !ok {
		return nil
	}
	if err := closer.Close(ctx); err != nil {
		return fmt.Errorf("close extension %q: %w", extension.Name(), err)
	}
	return nil
}
