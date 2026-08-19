package pi

import (
	"context"
	"testing"
)

func TestExtensionAPIBindsRegisteredToolToExtensionOwner(t *testing.T) {
	registry, err := newToolRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := extensionAPI{registry: registry, owner: "mcp:exa"}
	if err := api.RegisterTool(registryTestTool("web_search_exa")); err != nil {
		t.Fatal(err)
	}
	registry.rollback("mcp:exa")
	if definitions := registry.definitions(); len(definitions) != 0 {
		t.Fatalf("definitions after owner rollback = %#v", definitions)
	}
}

var _ Extension = extensionContractFake{}

type extensionContractFake struct{}

func (extensionContractFake) Name() string { return "contract" }

func (extensionContractFake) Register(context.Context, ExtensionAPI) error { return nil }
