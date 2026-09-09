package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai/providers"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
)

func testProviderOptions() providers.Options {
	return providers.Options{
		ID: "test", Protocol: providers.ProtocolOpenAI,
		BaseURL: "https://example.invalid/v1", APIKey: "test", Model: "fake",
		Pricing: &providers.Pricing{},
	}
}

func TestNewRequiresExplicitWritePolicy(t *testing.T) {
	cases := []Options{
		{AllowWrite: true},
		{AllowExec: true},
		{MCPServers: []pimcp.ServerOptions{{Name: "stdio", Transport: "stdio", Command: "ignored"}}},
	}
	for _, opts := range cases {
		opts.WorkDir = t.TempDir()
		opts.Platform = testProviderOptions()
		_, err := New(opts)
		if err == nil || !strings.Contains(err.Error(), "workspace policy") {
			t.Fatalf("New(%+v) error = %v", opts, err)
		}
		if _, err := os.Stat(filepath.Join(opts.WorkDir, ".tmp")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed construction changed filesystem: %v", err)
		}
	}
}

func TestNewExplicitPolicyKeepsToolFlagsAndLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		allowWrite bool
		allowExec  bool
		want       []string
		notWant    []string
	}{
		{name: "read only", want: []string{"read"}, notWant: []string{"edit", "write", "apply_patch", "exec", "process"}},
		{name: "write", allowWrite: true, want: []string{"read", "edit", "write", "apply_patch"}, notWant: []string{"exec", "process"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := New(Options{
				WorkDir: t.TempDir(), Platform: testProviderOptions(),
				WorkspacePolicy: WorkspacePolicy{WriteMode: WorkspaceWriteAll},
				AllowWrite:      tt.allowWrite, AllowExec: tt.allowExec,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Stop(context.Background()) })
			names := make(map[string]bool)
			for _, definition := range agent.ToolDefinitions() {
				names[definition.Name] = true
			}
			for _, name := range tt.want {
				if !names[name] {
					t.Errorf("missing tool %q: %v", name, names)
				}
			}
			for _, name := range tt.notWant {
				if names[name] {
					t.Errorf("unexpected tool %q: %v", name, names)
				}
			}
			if err := agent.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if err := agent.Stop(context.Background()); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
		})
	}
}

func TestNormalizeWorkspaceOptionsReturnsCanonicalRoot(t *testing.T) {
	realRoot := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	normalized, needsProcess, err := normalizeWorkspaceOptions(Options{
		WorkDir: linkRoot, Platform: testProviderOptions(),
		WorkspacePolicy: WorkspacePolicy{WriteMode: WorkspaceWriteAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	if needsProcess {
		t.Fatal("read-only options unexpectedly require a process runner")
	}
	if normalized.Root() != canonicalRoot {
		t.Fatalf("normalized root = %q, want canonical %q", normalized.Root(), canonicalRoot)
	}
}

func TestNewConstructionFailureClosesWorkspace(t *testing.T) {
	workDir := t.TempDir()
	_, err := New(Options{
		WorkDir: workDir,
		Platform: providers.Options{
			ID: "test", Protocol: providers.ProtocolOpenAI,
			BaseURL: "https://example.invalid/v1", APIKey: "test", Model: "fake",
		},
		WorkspacePolicy: WorkspacePolicy{WriteMode: WorkspaceWriteAll},
	})
	if err == nil || !strings.Contains(err.Error(), "pricing") {
		t.Fatalf("New() error = %v, want pricing failure", err)
	}
	if err := os.Remove(workDir); err != nil {
		t.Fatalf("workspace remained open after failed construction: %v", err)
	}
}

func TestConstructionCleanupRunsInReverseAndJoinsErrors(t *testing.T) {
	order := []string{}
	firstErr := errors.New("workspace close")
	secondErr := errors.New("supervisor close")
	cleanup := constructionCleanup{
		func() error { order = append(order, "workspace"); return firstErr },
		func() error { order = append(order, "supervisor"); return secondErr },
	}
	err := cleanup.close()
	if strings.Join(order, ",") != "supervisor,workspace" {
		t.Fatalf("cleanup order = %v", order)
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("cleanup error = %v", err)
	}
}

func TestNewStdioTransportDoesNotExpandToolFlags(t *testing.T) {
	agent, err := New(Options{
		WorkDir: t.TempDir(), Platform: testProviderOptions(),
		WorkspacePolicy: WorkspacePolicy{WriteMode: WorkspaceWriteAll},
		MCPServers: []pimcp.ServerOptions{{
			Name: "stdio", Transport: "stdio", Command: "ignored", AllowTools: []string{"example"},
		}},
	})
	if err != nil {
		if strings.Contains(err.Error(), "sandbox runner") {
			t.Skipf("native process sandbox unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })
	names := make(map[string]bool)
	for _, definition := range agent.ToolDefinitions() {
		names[definition.Name] = true
	}
	for _, name := range []string{"edit", "write", "apply_patch", "exec", "process"} {
		if names[name] {
			t.Errorf("stdio MCP unexpectedly enabled tool %q: %v", name, names)
		}
	}
}
