package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/ai"
)

// TestPromptComposerBuildsCoreAgentsAndSkillCatalogInOrder 验证系统提示词按核心纪律、Agent 定义和 Skill 目录的顺序组合，且不会泄露 Skill Body。
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

	message, report, err := NewPromptComposer(workDir).Build(snapshot)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	content := messageText(t, message)
	if message.Role != ai.RoleSystem {
		t.Fatalf("Role = %q, want %q", message.Role, ai.RoleSystem)
	}
	core := strings.Index(content, "# Agent Runtime 核心纪律")
	agents := strings.Index(content, "# Agent 定义（来自 AGENTS.md）")
	skills := strings.Index(content, "<available_skills>")
	if core < 0 || agents <= core || skills <= agents {
		t.Fatalf("prompt sections out of order: %q", content)
	}
	for _, want := range []string{
		"AGENTS.md", "Thinking", "必须通过 read 完整读取",
		"Use project conventions.", "Review changes", ".claw/skills/review/SKILL.md",
		"sha256:0123456789abcdef", "必须先使用 read",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt missing %q: %q", want, content)
		}
	}
	if strings.Contains(content, "body-secret-must-not-leak") {
		t.Fatalf("prompt leaked Skill Body: %q", content)
	}
	if report.IncludedSkills != 1 || report.Truncated {
		t.Fatalf("report = %#v", report)
	}
}

// TestPromptComposerDoesNotRequireUnavailableTools 验证核心提示词不会要求模型调用未提供的工具。
func TestPromptComposerDoesNotRequireUnavailableTools(t *testing.T) {
	workDir := t.TempDir()
	writeAgentsInstructions(t, workDir, "Follow the configured Agent role.")
	message, _, err := NewPromptComposer(workDir).Build(nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	content := messageText(t, message)
	for _, unavailable := range []string{"write_file", "test -f", "ls 或", "调用 bash"} {
		if strings.Contains(content, unavailable) {
			t.Fatalf("core prompt requires unavailable tool %q: %q", unavailable, content)
		}
	}
}

// TestPromptComposerAllowsEmptyCatalog 验证 Composer 只负责构建 Prompt，零 Skill 的运行约束由 RunContextFactory 执行。
func TestPromptComposerAllowsEmptyCatalog(t *testing.T) {
	workDir := t.TempDir()
	writeAgentsInstructions(t, workDir, "General Agent instructions.")
	message, report, err := NewPromptComposer(workDir).Build(newSkillSnapshot(nil, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	content := messageText(t, message)
	if message.Role != ai.RoleSystem || !strings.Contains(content, "# Agent Runtime 核心纪律") {
		t.Fatalf("Build() = %#v", message)
	}
	for _, absent := range []string{"<available_skills>"} {
		if strings.Contains(content, absent) {
			t.Fatalf("prompt contains absent section %q: %q", absent, content)
		}
	}
	if report != (SkillPromptReport{}) {
		t.Fatalf("report = %#v", report)
	}
}

// TestPromptComposerReadsAgentsFileOnEveryBuild 验证每次构建都会重新读取 AGENTS.md，而不是复用旧内容。
func TestPromptComposerReadsAgentsFileOnEveryBuild(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "AGENTS.md")
	composer := NewPromptComposer(workDir)
	if err := os.WriteFile(path, []byte("guide-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, _, err := composer.Build(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("guide-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := composer.Build(nil)
	if err != nil {
		t.Fatal(err)
	}

	firstContent := messageText(t, first)
	secondContent := messageText(t, second)
	if !strings.Contains(firstContent, "guide-v1") || strings.Contains(firstContent, "guide-v2") {
		t.Fatalf("first Build() = %q", firstContent)
	}
	if !strings.Contains(secondContent, "guide-v2") || strings.Contains(secondContent, "guide-v1") {
		t.Fatalf("second Build() = %q", secondContent)
	}
}

// TestPromptComposerRejectsAgentsSymlinks 验证组合器不会读取指向工作区内外文件的 AGENTS.md 软链接。
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
		_, _, err := NewPromptComposer(workDir).Build(nil)
		if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
			t.Fatalf("Build() error = %v, want rejected symlink", err)
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
		_, _, err := NewPromptComposer(workDir).Build(nil)
		if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
			t.Fatalf("Build() error = %v, want rejected symlink", err)
		}
	})
}

func messageText(t *testing.T, message ai.Message) string {
	t.Helper()
	text, err := ai.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
