package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/skills"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestNewChatWorkDirResolvesConfiguredDirectory(t *testing.T) {
	root := t.TempDir()
	got, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: "  " + root + "  "}})
	if err != nil {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
	gotInfo, err := os.Stat(string(got))
	if err != nil {
		t.Fatalf("stat resolved Workspace: %v", err)
	}
	wantInfo, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat expected Workspace: %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("NewChatWorkDir() = %q, want directory %q", got, root)
	}
}

func TestDefaultChatWorkspacePromptDoesNotLoadRepositoryCodingIdentity(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	workDir, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: workspace}})
	if err != nil {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(string(workDir)), string(workDir))
	got, err := builder.Build(context.Background(), harness.ContextRequest{
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("你好")}},
	}, []ai.ToolDefinition{{Name: "read"}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	systemText, err := ai.TextContent(got.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemText, "通用聊天助手") {
		t.Fatalf("system prompt missing Chat identity: %q", systemText)
	}
	if strings.Contains(systemText, "仓库自带命令行程序") || strings.Contains(systemText, "repository-development") {
		t.Fatalf("system prompt leaked repository Coding identity: %q", systemText)
	}
	wantCatalog := map[string]string{
		"decision-support":     "Use when the user asks to compare, choose, rank, weigh tradeoffs, or recommend between options.",
		"learning-explanation": "Use when the user asks for a concept explanation, tutoring, step-by-step teaching, examples, or practice.",
		"weather-assistance":   "Use when the user asks about weather, temperature, rain, snow, umbrellas, clothing, outdoor activities, or forecasts.",
		"writing-assistance":   "Use when the user asks to draft, rewrite, polish, shorten, or adjust the tone of an email, notice, copy, or report.",
	}
	for name, description := range wantCatalog {
		location := "skills/" + name + "/SKILL.md"
		for _, wanted := range []string{
			"<name>" + name + "</name>",
			"<description>" + description + "</description>",
			"<location>" + location + "</location>",
		} {
			if !strings.Contains(systemText, wanted) {
				t.Fatalf("system prompt missing %q: %q", wanted, systemText)
			}
		}
	}
	for _, bodyOnly := range []string{
		"给出带条件的推荐，并说明主要代价",
		"至少提供一个贴合问题的例子",
		"Tool 失败时说明暂时无法获取",
		"信息充分时直接给成稿",
	} {
		if strings.Contains(systemText, bodyOnly) {
			t.Fatalf("system prompt eagerly loaded Skill body %q", bodyOnly)
		}
	}
}

func TestDefaultChatWorkspaceSkillsHaveNoDiagnostics(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := snapshot.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("Skill diagnostics = %#v", diagnostics)
	}
}

func TestDefaultChatWorkspaceDiscoversOnlyGeneralChatSkills(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}
	summaries := snapshot.Skills()
	names := make([]string, len(summaries))
	for i, summary := range summaries {
		names[i] = summary.Name
	}
	want := []string{"decision-support", "learning-explanation", "weather-assistance", "writing-assistance"}
	if !slices.Equal(names, want) {
		t.Fatalf("skills = %v, want %v", names, want)
	}
	if diagnostics := snapshot.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, summary := range summaries {
		if summary.Location != "skills/"+summary.Name+"/SKILL.md" || summary.Source != skills.SourceWorkspace {
			t.Fatalf("summary = %#v", summary)
		}
	}
}

func TestDefaultChatWorkspaceLoadsAllAgentProfiles(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	profiles := catalog.List()
	codes := make([]string, len(profiles))
	for index, profile := range profiles {
		codes[index] = profile.Code
		if strings.TrimSpace(profile.Instructions) == "" {
			t.Fatalf("profile %q has empty instructions", profile.Code)
		}
	}
	want := []string{"general", "writing", "learning", "health", "legal", "automotive", "workplace", "parenting"}
	if !slices.Equal(codes, want) || catalog.DefaultCode() != "general" {
		t.Fatalf("profiles/default = %v / %q, want %v / general", codes, catalog.DefaultCode(), want)
	}
	for _, code := range want[1:] {
		profile, found := catalog.Find(code)
		if !found || len(profile.Skills) != 1 || !strings.HasPrefix(profile.Skills[0].Location, "profiles/"+code+"/skills/") {
			t.Fatalf("profile %q = %#v, found=%v", code, profile, found)
		}
	}
}

func TestDefaultChatWorkspaceSkillBodiesAreReadableThroughWebRuntime(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	var runtime pi.ToolRuntime
	app := fxtest.New(t,
		agentRegister,
		fx.Supply(
			pi.WorkDir(workspace),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	for _, name := range []string{"decision-support", "learning-explanation", "weather-assistance", "writing-assistance"} {
		relativePath := "skills/" + name + "/SKILL.md"
		arguments, err := json.Marshal(map[string]any{"path": relativePath})
		if err != nil {
			t.Fatal(err)
		}
		result, err := runtime.Execute(context.Background(), ai.ToolCall{
			ID: "read-" + name, Name: "read", Arguments: arguments,
		}, nil)
		if err != nil || result.IsError {
			t.Fatalf("read %s: result = %#v, error = %v", relativePath, result, err)
		}
		got, err := ai.TextContent(result.Content)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Fatalf("read %s returned %q, want full body %q", relativePath, got, want)
		}
	}
}

func TestNewChatWorkDirRejectsInvalidWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "nil config", want: "配置"},
		{name: "missing", cfg: &config.Config{Agent: config.AgentConfig{WorkspaceDir: filepath.Join(t.TempDir(), "missing")}}, want: "检查"},
		{name: "regular file", cfg: &config.Config{Agent: config.AgentConfig{WorkspaceDir: file}}, want: "必须是目录"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewChatWorkDir(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "Agent Workspace") {
				t.Fatalf("NewChatWorkDir() error = %v, want Agent Workspace error containing %q", err, tt.want)
			}
		})
	}
}

func TestNewChatWorkDirRejectsProcessWorkingDirectory(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: workingDir}})
	if err == nil || !strings.Contains(err.Error(), "不能使用进程当前目录") {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
}
