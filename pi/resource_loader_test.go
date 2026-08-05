package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/skills"
)

func TestPromptComposerBuildsGenericServicePrompt(t *testing.T) {
	workDir := t.TempDir()
	writeAgentsInstructions(t, workDir, "你是一名售后客服数字员工。使用简洁、自然的中文回复。")
	writeTestSkill(t, workDir, "refund-policy/SKILL.md", "---\nname: refund-policy\ndescription: 处理退款政策问题\n---\nFollow the refund policy.")
	snapshot, err := skills.NewLoader(workDir).Discover(skills.DefaultEnvironment())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	message, report, err := NewPromptComposer(workDir).Build(snapshot)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	content := messageText(t, message)
	core := strings.Index(content, "# Agent Runtime 核心纪律")
	agents := strings.Index(content, "# Agent 定义（来自 AGENTS.md）")
	skills := strings.Index(content, "<available_skills>")
	if core < 0 || agents <= core || skills <= agents {
		t.Fatalf("prompt sections out of order: %q", content)
	}
	for _, want := range []string{
		"必须遵守工作区 AGENTS.md", "必须通过 read 完整读取", "售后客服数字员工",
		"退款政策", ".claw/skills/refund-policy/SKILL.md",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt missing %q: %q", want, content)
		}
	}
	for _, codingOnly := range []string{"研发助手", "修改文件前", "始终使用中文", "```markdown"} {
		if strings.Contains(content, codingOnly) {
			t.Fatalf("prompt contains coding-only instruction %q: %q", codingOnly, content)
		}
	}
	if report.IncludedSkills != 1 {
		t.Fatalf("report = %#v, want one included Skill", report)
	}
}

func TestRunContextFactoryRequiresReadForEveryAgent(t *testing.T) {
	workDir := t.TempDir()
	writeAgentsInstructions(t, workDir, "You are a service Agent.")
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))

	_, err := factory.Create(context.Background(), validWorkspaceRunRequest(), nil)
	if err == nil || err.Error() != "agent runtime: required tool read is not registered" {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestRunContextFactoryRequiresEligibleSkill(t *testing.T) {
	workDir := t.TempDir()
	writeAgentsInstructions(t, workDir, "You are a service Agent.")
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))

	_, err := factory.Create(context.Background(), validWorkspaceRunRequest(), []ai.ToolDefinition{{Name: "read"}})
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "at least one eligible Skill") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestRunContextFactoryPreservesSkillsAndPiInvalidErrors(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "missing")
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))

	_, err := factory.Create(context.Background(), validWorkspaceRunRequest(), []ai.ToolDefinition{{Name: "read"}})
	if !errors.Is(err, skills.ErrInvalid) {
		t.Fatalf("Create() error = %v, want skills.ErrInvalid", err)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create() error = %v, want pi.ErrInvalid", err)
	}
}

func validWorkspaceRunRequest() agent.RunRequest {
	return agent.RunRequest{Input: ai.Message{
		Role:    ai.RoleUser,
		Content: []ai.ContentBlock{ai.TextBlock("hello")},
	}}
}

func TestRunContextFactoryImplementsAgentContract(t *testing.T) {
	var _ agent.ContextFactory = NewRunContextFactory(
		NewPromptComposer(t.TempDir()), skills.NewLoader(t.TempDir()),
	)
}

func TestPromptComposerRequiresAgentsFile(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		want    string
	}{
		{name: "missing", prepare: func(*testing.T, string) {}, want: "AGENTS.md is required"},
		{
			name: "empty",
			prepare: func(t *testing.T, workDir string) {
				writeAgentsInstructions(t, workDir, "  \n\t")
			},
			want: "AGENTS.md must not be empty",
		},
		{
			name: "invalid utf8",
			prepare: func(t *testing.T, workDir string) {
				if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte{0xff}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "AGENTS.md must be valid UTF-8",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			test.prepare(t, workDir)
			_, _, err := NewPromptComposer(workDir).Build(nil)
			if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func writeAgentsInstructions(t *testing.T, workDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTestSkill(t *testing.T, workDir, relativePath, content string) {
	t.Helper()
	path := filepath.Join(workDir, ".claw", "skills", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
