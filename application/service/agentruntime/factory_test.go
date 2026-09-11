package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/pi/ai/providers"
)

func versionFixture(t *testing.T) (*config.Config, agent.Version) {
	t.Helper()
	cfg := &config.Config{CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid/", APIKey: "test", Model: "model", Pricing: &providers.Pricing{}}}}
	s, err := cfg.CaptureAgentSnapshot("p", "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := agent.Version{ID: "v", TenantID: "t", AgentID: "a", BundleCommit: "h", BundleDigest: "sha256:" + strings.Repeat("a", 64)}
	v.ModelConfig, _ = json.Marshal(s.Model)
	v.ToolPolicy, _ = json.Marshal(s.Tools)
	v.RuntimeConfig, _ = json.Marshal(s.Runtime)
	v.SpecDigest, err = agentversion.SpecDigest(v.BundleDigest, s)
	if err != nil {
		t.Fatal(err)
	}
	v.Validation = []byte(`{"digest_version":1}`)
	return cfg, v
}
func TestRuntimeOptionsUseSavedVersionAndRejectTampering(t *testing.T) {
	cfg, v := versionFixture(t)
	cfg.Platforms[0].Model = "new-default"
	f := &PIFactory{Config: cfg}
	r := Request{Key: Key{Kind: "chat", TenantID: "t", AgentID: "a", ConversationID: "c", VersionID: "v", SpecDigest: v.SpecDigest}, Version: v}
	opts, err := f.optionsFor(r, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if opts.Platform.Model != "model" || opts.AllowWrite || opts.WorkspacePolicy.WriteMode != "restricted" {
		t.Fatal("snapshot or write boundary changed")
	}
	r.Version.ModelConfig = []byte(strings.ReplaceAll(string(v.ModelConfig), `"model"`, `"tampered"`))
	if _, err := f.optionsFor(r, t.TempDir()); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
}
