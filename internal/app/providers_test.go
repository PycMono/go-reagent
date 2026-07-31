package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentconfig "github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx/fxtest"
)

func TestNewConfigLoadsTrimmedConfigurationPath(t *testing.T) {
	path := writeRuntimeConfig(t)
	t.Setenv("CONFIG_PATH", "  "+path+"  ")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "test-platform" || current.Model != "test-model" {
		t.Fatalf("current platform = %#v", current)
	}
}

func TestNewWorkDirUsesCurrentDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got, err := NewWorkDir()
	if err != nil {
		t.Fatalf("NewWorkDir() error = %v", err)
	}
	if string(got) != want {
		t.Fatalf("WorkDir = %q, want %q", got, want)
	}
}

func TestNewLLMProviderBuildsCurrentPlatform(t *testing.T) {
	cfg := &agentconfig.Config{
		CurrentPlatform: "test-platform",
		Platforms: []agentconfig.PlatformConfig{{
			ID:       "test-platform",
			Protocol: agentconfig.ProtocolOpenAI,
			BaseURL:  "https://example.com/v1/",
			APIKey:   "test-key",
			Model:    "test-model",
		}},
	}

	llmProvider, err := NewLLMProvider(cfg)
	if err != nil {
		t.Fatalf("NewLLMProvider() error = %v", err)
	}
	if llmProvider == nil {
		t.Fatal("NewLLMProvider() = nil")
	}
}

func TestNewLLMProviderRejectsMissingCurrentPlatform(t *testing.T) {
	_, err := NewLLMProvider(&agentconfig.Config{CurrentPlatform: "missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("NewLLMProvider() error = %v, want missing platform", err)
	}
}

func TestNewRegistryRegistersToolsAndClosesThemOnStop(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	registry, err := NewRegistry(lifecycle, WorkDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	lifecycle.RequireStart()

	definitions := registry.GetAvailableTools()
	wantNames := []string{"apply_patch", "edit_file", "exec", "process", "read_file", "write_file"}
	if len(definitions) != len(wantNames) {
		t.Fatalf("definitions = %#v", definitions)
	}
	for index, want := range wantNames {
		if definitions[index].Name != want {
			t.Fatalf("definitions[%d].Name = %q, want %q", index, definitions[index].Name, want)
		}
	}
	lifecycle.RequireStop()

	result := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "read-after-stop",
		Name:      "read_file",
		Arguments: []byte(`{"path":"a.txt"}`),
	})
	if !result.IsError {
		t.Fatalf("result after Stop = %#v, want closed-resource error", result)
	}
	execResult := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "exec-after-stop",
		Name:      "exec",
		Arguments: []byte(`{"command":"true"}`),
	})
	if !execResult.IsError {
		t.Fatalf("exec result after Stop = %#v, want closed-resource error", execResult)
	}
	processResult := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "process-after-stop",
		Name:      "process",
		Arguments: []byte(`{"action":"list"}`),
	})
	if !processResult.IsError {
		t.Fatalf("process result after Stop = %#v, want closed-resource error", processResult)
	}
}

func TestNewReporterSendsWeComEventWhenConfigured(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := NewReporter(&agentconfig.Config{
		Bot: agentconfig.BotConfig{
			WeCom: agentconfig.WeComConfig{WebhookURL: server.URL},
		},
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}
	reporter.OnThinking(context.Background())

	select {
	case <-received:
	default:
		t.Fatal("configured WeCom Reporter did not send lifecycle event")
	}
}

func TestNewReporterAllowsDisabledWeCom(t *testing.T) {
	reporter, err := NewReporter(&agentconfig.Config{})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}
	if reporter == nil {
		t.Fatal("NewReporter() = nil")
	}
}

func TestNewPromptUsesEnvironmentOverrideAndDefault(t *testing.T) {
	t.Setenv("AGENT_PROMPT", "custom prompt")
	if got := NewPrompt(); got != Prompt("custom prompt") {
		t.Fatalf("NewPrompt() = %q", got)
	}

	t.Setenv("AGENT_PROMPT", "")
	if got := string(NewPrompt()); !strings.Contains(got, "ping.go") || !strings.Contains(got, "git 提交") {
		t.Fatalf("default prompt = %q", got)
	}
}

func writeRuntimeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	document := `{
		"currentPlatform":"test-platform",
		"platforms":[{
			"id":"test-platform",
			"protocol":"openai",
			"baseURL":"https://example.com/v1/",
			"apiKey":"test-key",
			"model":"test-model"
		}]
	}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
