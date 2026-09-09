package mcp

import (
	"path/filepath"
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

func TestNewDoesNotBypassRestrictedOrDisabledStdioRunner(t *testing.T) {
	root := tools.Root(t.TempDir())
	for _, policy := range []sandbox.Policy{
		{Backend: "host", WriteMode: "restricted", TmpDir: filepath.Join(string(root), ".tmp")},
		{Backend: "disabled", WriteMode: "all"},
	} {
		_, err := New([]ServerOptions{{
			Name: "test", Transport: "stdio", Command: "true",
			Env: map[string]string{"HOME": "/override"}, AllowTools: []string{"read"},
		}}, root, &recordingRunner{policy: policy})
		if err == nil {
			t.Fatalf("policy %#v bypassed Runner", policy)
		}
	}
}
