package pi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/PycMono/go-reagent/pi/ai"
)

const staticToolOwner = "pi:static"

type toolEntry struct {
	definition   ai.ToolDefinition
	tool         ai.Tool
	validateArgs func(json.RawMessage) error
	owner        string
}

type toolRegistry struct {
	mu     sync.RWMutex
	tools  map[string]toolEntry
	frozen bool
}

func newToolRegistry(tools []ai.Tool) (*toolRegistry, error) {
	registry := &toolRegistry{tools: make(map[string]toolEntry, len(tools))}
	for _, tool := range tools {
		if err := registry.register(staticToolOwner, tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *toolRegistry) register(owner string, tool ai.Tool) error {
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
	entry := toolEntry{definition: definition, tool: tool, validateArgs: validateArgs, owner: owner}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return errors.New("tool registry is frozen")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = entry
	return nil
}

func (r *toolRegistry) rollback(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, entry := range r.tools {
		if entry.owner == owner {
			delete(r.tools, name)
		}
	}
}

func (r *toolRegistry) freeze() {
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

func (r *toolRegistry) definitions() []ai.ToolDefinition {
	r.mu.RLock()
	definitions := make([]ai.ToolDefinition, 0, len(r.tools))
	for _, entry := range r.tools {
		definitions = append(definitions, entry.definition)
	}
	r.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *toolRegistry) lookup(name string) (toolEntry, bool) {
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()
	return entry, ok
}
