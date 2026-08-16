package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/skills"
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
