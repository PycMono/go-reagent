package toolexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/PycMono/go-reagent/pi/ai"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const staticToolOwner = "pi:static"

type entry struct {
	definition   ai.ToolDefinition
	tool         ai.Tool
	validateArgs func(json.RawMessage) error
	owner        string
}

// Registry 持有已注册 Tool 的定义、实现与参数校验器。Freeze 之后拒绝
// 新的注册；扩展注册的工具可按 owner 整体 Rollback。
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]entry
	frozen bool
}

func NewRegistry(tools []ai.Tool) (*Registry, error) {
	registry := &Registry{tools: make(map[string]entry, len(tools))}
	for _, tool := range tools {
		if err := registry.Register(staticToolOwner, tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register 以 owner 名义注册 Tool；owner 用于扩展失败时的整体回滚。
func (r *Registry) Register(owner string, tool ai.Tool) error {
	if ai.IsNilTool(tool) {
		return errors.New("tool must not be nil")
	}
	definition := tool.Definition()
	name := strings.TrimSpace(definition.Name)
	if name == "" {
		return errors.New("tool definition name must not be empty")
	}
	if definition.Name != name {
		return fmt.Errorf("tool definition name %q must not contain surrounding whitespace", definition.Name)
	}
	validateArgs, err := compileSchemaValidator(definition)
	if err != nil {
		return err
	}
	toolEntry := entry{definition: definition, tool: tool, validateArgs: validateArgs, owner: owner}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return errors.New("tool registry is frozen")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = toolEntry
	return nil
}

// Rollback 移除 owner 注册的全部 Tool。
func (r *Registry) Rollback(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, toolEntry := range r.tools {
		if toolEntry.owner == owner {
			delete(r.tools, name)
		}
	}
}

// Freeze 冻结注册表，后续 Register 一律失败。
func (r *Registry) Freeze() {
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

func (r *Registry) Definitions() []ai.ToolDefinition {
	r.mu.RLock()
	definitions := make([]ai.ToolDefinition, 0, len(r.tools))
	for _, toolEntry := range r.tools {
		definitions = append(definitions, toolEntry.definition)
	}
	r.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *Registry) lookup(name string) (entry, bool) {
	r.mu.RLock()
	toolEntry, ok := r.tools[name]
	r.mu.RUnlock()
	return toolEntry, ok
}

// Lookup 返回工具的定义与实现，供装配期校验（如 subagent 绑定）。
func (r *Registry) Lookup(name string) (ai.ToolDefinition, ai.Tool, bool) {
	toolEntry, ok := r.lookup(name)
	return toolEntry.definition, toolEntry.tool, ok
}

func compileSchemaValidator(definition ai.ToolDefinition) (func(json.RawMessage) error, error) {
	schemaJSON, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema for tool %q: %w", definition.Name, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("decode input schema for tool %q: %w", definition.Name, err)
	}
	location := "urn:go-reagent:tool:" + definition.Name
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("register input schema for tool %q: %w", definition.Name, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile input schema for tool %q: %w", definition.Name, err)
	}

	return func(arguments json.RawMessage) error {
		decoder := json.NewDecoder(bytes.NewReader(arguments))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("invalid arguments for tool %q: %w", definition.Name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return fmt.Errorf("invalid trailing arguments for tool %q: %w", definition.Name, err)
			}
			return fmt.Errorf("invalid trailing arguments for tool %q", definition.Name)
		}
		if err := compiled.Validate(value); err != nil {
			return fmt.Errorf("arguments do not match schema for tool %q: %w", definition.Name, err)
		}
		return nil
	}, nil
}
