package extension

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Runtime 聚合扩展并在 Start 时逐个注册其工具，全部成功后 freeze
// Registry。启动/停机由装配层显式调用 Start/Stop。
type Runtime struct {
	registry   *toolexec.Registry
	extensions []Extension
	started    []Extension
}

// NewRuntime 校验并聚合扩展：名字去空格、非空、去重，按名称排序保证
// 注册顺序确定。
func NewRuntime(registry *toolexec.Registry, extensions []Extension) (*Runtime, error) {
	extensions = append([]Extension(nil), extensions...)
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
	return &Runtime{registry: registry, extensions: extensions}, nil
}

// Start 逐个注册扩展工具；任一失败即回滚已注册的并关闭全部扩展。
// 全部成功后 Freeze 注册表。
func (runtime *Runtime) Start(ctx context.Context) error {
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

// Stop 倒序关闭已启动的扩展。
func (runtime *Runtime) Stop(ctx context.Context) error {
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
