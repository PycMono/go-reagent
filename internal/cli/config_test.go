package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/ai"
	cliapp "github.com/PycMono/go-reagent/internal/cli/app"
	"github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
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

func TestNewPromptUsesEnvironmentOverrideAndDefault(t *testing.T) {
	t.Setenv("AGENT_PROMPT", "custom prompt")
	if got := NewPrompt(); got != cliapp.Prompt("custom prompt") {
		t.Fatalf("NewPrompt() = %q", got)
	}

	t.Setenv("AGENT_PROMPT", "")
	if got := string(NewPrompt()); !strings.Contains(got, "ping.go") || !strings.Contains(got, "git 提交") {
		t.Fatalf("default prompt = %q", got)
	}
}

func TestRegisterProvidesProcessValues(t *testing.T) {
	path := writeRuntimeConfig(t)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("AGENT_PROMPT", "registered prompt")
	var (
		cfg      *reagent.Config
		platform ai.PlatformConfig
		workDir  workspace.WorkDir
		prompt   cliapp.Prompt
	)
	app := fxtest.New(t,
		fx.Provide(NewConfig, NewPlatform, NewWorkDir, NewPrompt),
		fx.Populate(&cfg, &platform, &workDir, &prompt),
	)
	app.RequireStart()
	defer app.RequireStop()

	if cfg == nil || platform.ID != "test-platform" || workDir == "" || prompt != cliapp.Prompt("registered prompt") {
		t.Fatalf("registered values = (%#v, %#v, %q, %q)", cfg, platform, workDir, prompt)
	}
}

func writeRuntimeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"currentPlatform":"test-platform",
		"platforms":[{
			"id":"test-platform",
			"protocol":"openai",
			"baseURL":"https://example.com/v1/",
			"apiKey":"test-key",
			"model":"test-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
