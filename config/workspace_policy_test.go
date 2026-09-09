package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
)

func TestWorkspacePolicyConfigCopiesPrefixes(t *testing.T) {
	p := WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch"}}
	got := p.PI()
	p.WritablePrefixes[0] = "unsafe"
	if got.WriteMode != pi.WorkspaceWriteRestricted || !reflect.DeepEqual(got.WritablePrefixes, []string{".tmp", "scratch"}) {
		t.Fatalf("policy alias or mapping error: %+v", got)
	}
}

func TestLoadWorkspacePolicyFormats(t *testing.T) {
	for _, tt := range []struct {
		name, extension, agent string
		wantMode               pi.WorkspaceWriteMode
		wantPrefixes           []string
	}{
		{name: "JSON all", extension: ".json", agent: `"workspace_policy":{"write_mode":"all","writable_prefixes":[]}`, wantMode: pi.WorkspaceWriteAll},
		{name: "YAML restricted", extension: ".yaml", agent: "workspace_policy:\n    write_mode: restricted\n    writable_prefixes: [.tmp, scratch]", wantMode: pi.WorkspaceWriteRestricted, wantPrefixes: []string{".tmp", "scratch"}},
		{name: "TOML restricted", extension: ".toml", agent: "[agent.workspace_policy]\nwrite_mode = \"restricted\"\nwritable_prefixes = [\".tmp\", \"scratch\"]", wantMode: pi.WorkspaceWriteRestricted, wantPrefixes: []string{".tmp", "scratch"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			path := writeConfigFile(t, "config"+tt.extension, workspacePolicyDocument(tt.extension, workspace, tt.agent))
			cfg, err := Load(path, WithAllowProcessCWD())
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Agent.WorkspacePolicy == nil {
				t.Fatal("workspace policy was not decoded")
			}
			got := cfg.Agent.WorkspacePolicy.PI()
			if got.WriteMode != tt.wantMode || !reflect.DeepEqual(got.WritablePrefixes, tt.wantPrefixes) {
				t.Fatalf("policy = %+v, want mode %q prefixes %v", got, tt.wantMode, tt.wantPrefixes)
			}
		})
	}
}

func TestLoadWorkspacePolicyRejectsInvalidValues(t *testing.T) {
	for _, tt := range []struct {
		name, policy string
	}{
		{name: "empty mode", policy: `{"write_mode":"","writable_prefixes":[]}`},
		{name: "unknown mode", policy: `{"write_mode":"unknown","writable_prefixes":[]}`},
		{name: "absolute prefix", policy: `{"write_mode":"restricted","writable_prefixes":["/tmp"]}`},
		{name: "parent prefix", policy: `{"write_mode":"restricted","writable_prefixes":["../out"]}`},
		{name: "all with prefix", policy: `{"write_mode":"all","writable_prefixes":["scratch"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			document := workspacePolicyDocument(".json", workspace, `"workspace_policy":`+tt.policy)
			if _, err := Load(writeConfig(t, document), WithAllowProcessCWD()); err == nil || !strings.Contains(err.Error(), "workspace policy") {
				t.Fatalf("Load() error = %v, want workspace policy validation error", err)
			}
		})
	}
}

func TestLoadWorkspacePolicyValidationDoesNotCreatePrefixes(t *testing.T) {
	workspace := t.TempDir()
	document := workspacePolicyDocument(".json", workspace, `"workspace_policy":{"write_mode":"restricted","writable_prefixes":[".tmp","scratch"]}`)
	if _, err := Load(writeConfig(t, document), WithAllowProcessCWD()); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{".tmp", "scratch"} {
		if _, err := os.Stat(filepath.Join(workspace, prefix)); !os.IsNotExist(err) {
			t.Fatalf("validation created %q: %v", prefix, err)
		}
	}
}

func workspacePolicyDocument(extension, workspace, agentPolicy string) string {
	switch extension {
	case ".yaml":
		return fmt.Sprintf("currentPlatform: x\nplatforms:\n  - id: x\n    protocol: openai\n    baseURL: https://x.test/\n    apiKey: k\n    model: m\n    pricing: {}\nagent:\n  workspace_dir: %q\n  %s\nredis:\n  addr: [127.0.0.1:6379]\n", workspace, agentPolicy)
	case ".toml":
		return fmt.Sprintf("currentPlatform = \"x\"\n[[platforms]]\nid = \"x\"\nprotocol = \"openai\"\nbaseURL = \"https://x.test/\"\napiKey = \"k\"\nmodel = \"m\"\n[platforms.pricing]\n[agent]\nworkspace_dir = %q\n%s\n[redis]\naddr = [\"127.0.0.1:6379\"]\n", workspace, agentPolicy)
	default:
		return fmt.Sprintf(`{"currentPlatform":"x","platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{}}],"agent":{"workspace_dir":%q,%s},"redis":{"addr":["127.0.0.1:6379"]}}`, workspace, agentPolicy)
	}
}
