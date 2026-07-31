package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestPromptComposerBuildsCoreAgentsAndSkillCatalogInOrder(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("Use project conventions."), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, workDir, "review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\nbody-secret-must-not-leak")
	snapshot := newSkillSnapshot([]SkillSummary{{
		Name:        "review",
		Description: "Review changes",
		Location:    ".claw/skills/review/SKILL.md",
		Version:     "sha256:0123456789abcdef",
		Source:      SkillSourceClaw,
	}}, nil)

	message, report := NewPromptComposer(workDir).Build(snapshot)
	if message.Role != schema.RoleSystem {
		t.Fatalf("Role = %q, want %q", message.Role, schema.RoleSystem)
	}
	core := strings.Index(message.Content, "# 核心身份")
	agents := strings.Index(message.Content, "# 项目专属指南")
	skills := strings.Index(message.Content, "<available_skills>")
	if core < 0 || agents <= core || skills <= agents {
		t.Fatalf("prompt sections out of order: %q", message.Content)
	}
	for _, want := range []string{
		"go-reagent", "Thinking", "修改文件前", "始终使用中文回复",
		"Use project conventions.", "Review changes", ".claw/skills/review/SKILL.md",
		"sha256:0123456789abcdef", "必须先使用 read_file",
	} {
		if !strings.Contains(message.Content, want) {
			t.Fatalf("prompt missing %q: %q", want, message.Content)
		}
	}
	if strings.Contains(message.Content, "body-secret-must-not-leak") {
		t.Fatalf("prompt leaked Skill Body: %q", message.Content)
	}
	if report.IncludedSkills != 1 || report.Truncated {
		t.Fatalf("report = %#v", report)
	}
}

func TestPromptComposerDoesNotRequireUnavailableTools(t *testing.T) {
	message, _ := NewPromptComposer(t.TempDir()).Build(nil)
	for _, unavailable := range []string{"write_file", "test -f", "ls 或", "调用 bash"} {
		if strings.Contains(message.Content, unavailable) {
			t.Fatalf("core prompt requires unavailable tool %q: %q", unavailable, message.Content)
		}
	}
}

func TestPromptComposerOmitsAbsentWorkspaceSections(t *testing.T) {
	message, report := NewPromptComposer(t.TempDir()).Build(newSkillSnapshot(nil, nil))
	if message.Role != schema.RoleSystem || !strings.Contains(message.Content, "# 核心身份") {
		t.Fatalf("Build() = %#v", message)
	}
	for _, absent := range []string{"# 项目专属指南", "<available_skills>"} {
		if strings.Contains(message.Content, absent) {
			t.Fatalf("prompt contains absent section %q: %q", absent, message.Content)
		}
	}
	if report != (SkillPromptReport{}) {
		t.Fatalf("report = %#v", report)
	}
}

func TestPromptComposerReadsAgentsFileOnEveryBuild(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "AGENTS.md")
	composer := NewPromptComposer(workDir)
	if err := os.WriteFile(path, []byte("guide-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, _ := composer.Build(nil)

	if err := os.WriteFile(path, []byte("guide-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _ := composer.Build(nil)

	if !strings.Contains(first.Content, "guide-v1") || strings.Contains(first.Content, "guide-v2") {
		t.Fatalf("first Build() = %q", first.Content)
	}
	if !strings.Contains(second.Content, "guide-v2") || strings.Contains(second.Content, "guide-v1") {
		t.Fatalf("second Build() = %q", second.Content)
	}
}

func TestPromptComposerRejectsAgentsSymlinks(t *testing.T) {
	t.Run("outside workspace", func(t *testing.T) {
		outsidePath := filepath.Join(t.TempDir(), "outside-agents.md")
		if err := os.WriteFile(outsidePath, []byte("outside-agents-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		workDir := t.TempDir()
		if err := os.Symlink(outsidePath, filepath.Join(workDir, "AGENTS.md")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		message, _ := NewPromptComposer(workDir).Build(nil)
		if strings.Contains(message.Content, "outside-agents-secret") || strings.Contains(message.Content, "# 项目专属指南") {
			t.Fatalf("Build() followed external symlink: %q", message.Content)
		}
	})

	t.Run("inside workspace", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "guide.md"), []byte("inside-linked-guide"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("guide.md", filepath.Join(workDir, "AGENTS.md")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		message, _ := NewPromptComposer(workDir).Build(nil)
		if strings.Contains(message.Content, "inside-linked-guide") || strings.Contains(message.Content, "# 项目专属指南") {
			t.Fatalf("Build() followed internal symlink: %q", message.Content)
		}
	})
}
