package main

import (
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
)

func TestServerAgentOptionsUsesSafeDefaultsWithoutWriteTools(t *testing.T) {
	opts, err := serverAgentOptions(serverOptionsParams(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if opts.AllowWrite || !opts.AllowExec || !opts.BuiltinSubagent {
		t.Fatalf("capabilities = write:%v exec:%v subagent:%v", opts.AllowWrite, opts.AllowExec, opts.BuiltinSubagent)
	}
	if opts.WorkspacePolicy.WriteMode != pi.WorkspaceWriteRestricted || !reflect.DeepEqual(opts.WorkspacePolicy.WritablePrefixes, []string{".tmp", "scratch"}) {
		t.Fatalf("policy = %+v", opts.WorkspacePolicy)
	}
}

func TestServerAgentOptionsPreservesNarrowerRestrictedPolicy(t *testing.T) {
	policy := &config.WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch/output"}}
	opts, err := serverAgentOptions(serverOptionsParams(t, policy))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts.WorkspacePolicy.WritablePrefixes, []string{".tmp", "scratch/output"}) {
		t.Fatalf("policy widened: %+v", opts.WorkspacePolicy)
	}
}

func TestServerAgentOptionsRejectsUnsafeServicePolicies(t *testing.T) {
	for _, policy := range []*config.WorkspacePolicyConfig{
		{WriteMode: "all"},
		{WriteMode: "restricted", WritablePrefixes: []string{"scratch"}},
		{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "agents"}},
	} {
		if _, err := serverAgentOptions(serverOptionsParams(t, policy)); err == nil {
			t.Fatalf("accepted policy %+v", policy)
		}
	}
}

func serverOptionsParams(t *testing.T, policy *config.WorkspacePolicyConfig) appParams {
	t.Helper()
	return appParams{
		WorkDir: pi.WorkDir(t.TempDir()),
		Config: &config.Config{
			CurrentPlatform: "test",
			Platforms:       []providers.Options{{ID: "test", Protocol: providers.ProtocolOpenAI, APIKey: "test", Model: "fake"}},
			Agent:           config.AgentConfig{WorkspacePolicy: policy},
		},
	}
}
