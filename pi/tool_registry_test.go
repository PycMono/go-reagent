package pi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

type registryTestTool string

func (tool registryTestTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: string(tool),
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
	}
}

func (registryTestTool) Execute(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
}

type nilRegistryTestTool struct{}

func (*nilRegistryTestTool) Definition() ai.ToolDefinition {
	panic("Definition must not be called for a typed-nil tool")
}

func (*nilRegistryTestTool) Execute(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
	panic("Execute must not be called for a typed-nil tool")
}

func TestToolRegistryRollsBackOneOwnerAndFreezes(t *testing.T) {
	registry, err := newToolRegistry([]ai.Tool{registryTestTool("read")})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.register("mcp:exa", registryTestTool("web_search_exa")); err != nil {
		t.Fatal(err)
	}
	registry.rollback("mcp:exa")
	definitions := registry.definitions()
	if len(definitions) != 1 || definitions[0].Name != "read" {
		t.Fatalf("definitions = %#v", definitions)
	}
	registry.freeze()
	if err := registry.register("mcp:exa", registryTestTool("web_fetch_exa")); err == nil {
		t.Fatal("register after freeze succeeded")
	}
}

func TestToolRegistrySortsDefinitions(t *testing.T) {
	registry, err := newToolRegistry([]ai.Tool{registryTestTool("zeta"), registryTestTool("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.definitions()
	names := []string{definitions[0].Name, definitions[1].Name}
	if !slices.Equal(names, []string{"alpha", "zeta"}) {
		t.Fatalf("definition names = %v", names)
	}
}

func TestToolRegistryRejectsDuplicateAcrossOwners(t *testing.T) {
	registry, err := newToolRegistry([]ai.Tool{registryTestTool("read")})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.register("mcp:exa", registryTestTool("read")); err == nil || !strings.Contains(err.Error(), `tool "read"`) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestToolRegistryRejectsTypedNil(t *testing.T) {
	var tool *nilRegistryTestTool
	_, err := newToolRegistry([]ai.Tool{tool})
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("typed-nil error = %v", err)
	}
}
