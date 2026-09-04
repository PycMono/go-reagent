package mcp

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
)

func TestNewRejectsStdioHostPathOverride(t *testing.T) {
	_, err := New([]ServerOptions{{
		Name:       "test",
		Transport:  "stdio",
		Command:    "true",
		Env:        map[string]string{"PATH": "/tmp"},
		AllowTools: []string{"read"},
	}}, tools.Root(t.TempDir()), sandbox.NewHostRunner())
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("New() error = %v, want PATH override rejection", err)
	}
}
