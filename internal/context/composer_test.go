package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestPromptComposerBuildsCoreAgentsAndSkillsInOrder(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workDir, "AGENTS.md"),
		[]byte("Use project conventions."),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, workDir, "review/SKILL.md", "---\nname: Review\ndescription: Review changes\n---\nCheck tests.")

	message := NewPromptComposer(workDir).Build()
	if message.Role != schema.RoleSystem {
		t.Fatalf("Role = %q, want %q", message.Role, schema.RoleSystem)
	}
	core := strings.Index(message.Content, "# 核心身份")
	agents := strings.Index(message.Content, "# 项目专属指南")
	skills := strings.Index(message.Content, "### 可用专业技能")
	if core < 0 || agents <= core || skills <= agents {
		t.Fatalf("prompt sections out of order: %q", message.Content)
	}
	for _, want := range []string{
		"go-reagent",
		"Thinking",
		"修改文件前",
		"始终使用中文回复",
		"Use project conventions.",
		"Review changes",
		"Check tests.",
	} {
		if !strings.Contains(message.Content, want) {
			t.Fatalf("prompt missing %q: %q", want, message.Content)
		}
	}
}

func TestPromptComposerDoesNotRequireUnavailableTools(t *testing.T) {
	content := NewPromptComposer(t.TempDir()).Build().Content
	for _, unavailable := range []string{"write_file", "test -f", "ls 或", "调用 bash"} {
		if strings.Contains(content, unavailable) {
			t.Fatalf("core prompt requires unavailable tool %q: %q", unavailable, content)
		}
	}
}

func TestPromptComposerOmitsAbsentWorkspaceSections(t *testing.T) {
	message := NewPromptComposer(t.TempDir()).Build()
	if message.Role != schema.RoleSystem || !strings.Contains(message.Content, "# 核心身份") {
		t.Fatalf("Build() = %#v", message)
	}
	for _, absent := range []string{"# 项目专属指南", "### 可用专业技能"} {
		if strings.Contains(message.Content, absent) {
			t.Fatalf("prompt contains absent section %q: %q", absent, message.Content)
		}
	}
}

func TestPromptComposerReadsAgentsFileOnEveryBuild(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "AGENTS.md")
	composer := NewPromptComposer(workDir)
	if err := os.WriteFile(path, []byte("guide-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := composer.Build().Content

	if err := os.WriteFile(path, []byte("guide-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := composer.Build().Content

	if !strings.Contains(first, "guide-v1") || strings.Contains(first, "guide-v2") {
		t.Fatalf("first Build() = %q", first)
	}
	if !strings.Contains(second, "guide-v2") || strings.Contains(second, "guide-v1") {
		t.Fatalf("second Build() = %q", second)
	}
}

func TestPromptComposerReadsSkillFileOnEveryBuild(t *testing.T) {
	workDir := t.TempDir()
	writeSkill(t, workDir, "refresh/SKILL.md", "---\nname: refresh\ndescription: trigger-v1\n---\nbody-v1")
	composer := NewPromptComposer(workDir)
	first := composer.Build().Content

	writeSkill(t, workDir, "refresh/SKILL.md", "---\nname: refresh\ndescription: trigger-v2\n---\nbody-v2")
	second := composer.Build().Content

	if !strings.Contains(first, "trigger-v1") || !strings.Contains(first, "body-v1") ||
		strings.Contains(first, "trigger-v2") || strings.Contains(first, "body-v2") {
		t.Fatalf("first Build() = %q", first)
	}
	if !strings.Contains(second, "trigger-v2") || !strings.Contains(second, "body-v2") ||
		strings.Contains(second, "trigger-v1") || strings.Contains(second, "body-v1") {
		t.Fatalf("second Build() = %q", second)
	}
}

func TestPromptComposerRejectsAgentsSymlinkOutsideWorkspace(t *testing.T) {
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside-agents.md")
	const secret = "outside-agents-secret"
	if err := os.WriteFile(outsidePath, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	if err := os.Symlink(outsidePath, filepath.Join(workDir, "AGENTS.md")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}

	content := NewPromptComposer(workDir).Build().Content
	if strings.Contains(content, secret) || strings.Contains(content, "# 项目专属指南") {
		t.Fatalf("Build() followed an external symlink: %q", content)
	}
}

func TestPromptComposerRejectsAgentsSymlinkInsideWorkspace(t *testing.T) {
	workDir := t.TempDir()
	guideDir := filepath.Join(workDir, "docs")
	if err := os.MkdirAll(guideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guideDir, "agent-guide.md"), []byte("linked guide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("docs/agent-guide.md", filepath.Join(workDir, "AGENTS.md")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}

	content := NewPromptComposer(workDir).Build().Content
	if strings.Contains(content, "linked guide") || strings.Contains(content, "# 项目专属指南") {
		t.Fatalf("Build() loaded a symlinked AGENTS.md: %q", content)
	}
}
