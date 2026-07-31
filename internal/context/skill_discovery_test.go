package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillLoaderDiscoversSourcesWithPrecedence 验证多个来源的同名 Skill 按预定优先级选取，并记录被覆盖项。
func TestSkillLoaderDiscoversSourcesWithPrecedence(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, ".claw/skills/review/SKILL.md", "review", "legacy", "legacy-body-secret")
	writeWorkspaceSkill(t, workDir, ".agents/skills/review/SKILL.md", "review", "agents", "agents-body-secret")
	writeWorkspaceSkill(t, workDir, "skills/review/SKILL.md", "review", "workspace", "workspace-body-secret")

	snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	skills := snapshot.Skills()
	if len(skills) != 1 {
		t.Fatalf("skills = %#v", skills)
	}
	if skills[0].Name != "review" || skills[0].Description != "workspace" ||
		skills[0].Location != "skills/review/SKILL.md" || skills[0].Source != SkillSourceWorkspace {
		t.Fatalf("skill = %#v", skills[0])
	}
	if !strings.HasPrefix(skills[0].Version, "sha256:") || len(skills[0].Version) != len("sha256:")+16 {
		t.Fatalf("Version = %q", skills[0].Version)
	}
	for _, secret := range []string{"legacy-body-secret", "agents-body-secret", "workspace-body-secret"} {
		if strings.Contains(fmt.Sprint(skills), secret) {
			t.Fatalf("snapshot retained Body %q: %#v", secret, skills)
		}
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_shadowed")
}

// TestSkillLoaderExcludesSameSourceDuplicateNames 验证同一来源存在重复名称时禁用该名称及低优先级补位项。
func TestSkillLoaderExcludesSameSourceDuplicateNames(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/one/SKILL.md", "duplicate", "first", "first body")
	writeWorkspaceSkill(t, workDir, "skills/two/SKILL.md", "duplicate", "second", "second body")
	writeWorkspaceSkill(t, workDir, ".claw/skills/duplicate/SKILL.md", "duplicate", "legacy", "legacy body")

	snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 0 {
		t.Fatalf("skills = %#v, want duplicate name excluded", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_duplicate_name", "skill_shadowed")
}

// TestSkillLoaderReportsShadowedDuplicatesInLowerPrioritySource 验证低优先级来源的重复项会同时报告重名和被覆盖诊断。
func TestSkillLoaderReportsShadowedDuplicatesInLowerPrioritySource(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/review/SKILL.md", "review", "winner", "winner body")
	writeWorkspaceSkill(t, workDir, ".claw/skills/one/SKILL.md", "review", "legacy one", "legacy body one")
	writeWorkspaceSkill(t, workDir, ".claw/skills/two/SKILL.md", "review", "legacy two", "legacy body two")

	snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Description != "winner" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_duplicate_name", "skill_shadowed")
}

// TestSkillLoaderSortsSkillsAndVersionsContent 验证 Skill 按名称稳定排序，且内容变化会更新摘要版本。
func TestSkillLoaderSortsSkillsAndVersionsContent(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/zeta/SKILL.md", "zeta", "last", "zeta body")
	writeWorkspaceSkill(t, workDir, "skills/alpha/SKILL.md", "alpha", "first", "alpha body v1")
	loader := NewSkillLoader(workDir)

	first, err := loader.Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{first.Skills()[0].Name, first.Skills()[1].Name}; strings.Join(got, ",") != "alpha,zeta" {
		t.Fatalf("skill order = %v", got)
	}
	firstVersion := first.Skills()[0].Version
	second, err := loader.Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if second.Skills()[0].Version != firstVersion {
		t.Fatalf("stable content version changed: %q != %q", second.Skills()[0].Version, firstVersion)
	}

	writeWorkspaceSkill(t, workDir, "skills/alpha/SKILL.md", "alpha", "first", "alpha body v2")
	third, err := loader.Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if third.Skills()[0].Version == firstVersion {
		t.Fatalf("changed content kept version %q", firstVersion)
	}
}

// TestSkillLoaderReturnsEmptySnapshotWithoutSkills 验证没有 Skill 目录时返回非空但内容为空的快照。
func TestSkillLoaderReturnsEmptySnapshotWithoutSkills(t *testing.T) {
	snapshot, err := NewSkillLoader(t.TempDir()).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || len(snapshot.Skills()) != 0 || len(snapshot.Diagnostics()) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

// TestSkillLoaderReportsInvalidSkillsWithoutFailingSiblings 验证无效 Skill 只产生诊断，不影响同目录中的有效 Skill。
func TestSkillLoaderReportsInvalidSkillsWithoutFailingSiblings(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/valid/SKILL.md", "valid", "valid skill", "valid body")
	writeRawWorkspaceSkill(t, workDir, "skills/malformed/SKILL.md", "---\nname: [broken\n---\nBody")
	writeRawWorkspaceSkill(t, workDir, "skills/nul/SKILL.md", "---\nname: nul\ndescription: nul\n---\nA\x00B")
	writeRawWorkspaceBytes(t, workDir, "skills/utf8/SKILL.md", append([]byte("---\nname: utf8\ndescription: utf8\n---\n"), 0xff))
	writeRawWorkspaceBytes(t, workDir, "skills/large/SKILL.md", []byte(strings.Repeat("x", maxSkillFileBytes+1)))

	snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "valid" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(),
		"skill_frontmatter_invalid", "skill_binary_content", "skill_not_utf8", "skill_file_too_large")
}

// TestSkillLoaderRejectsSymlinkEntries 验证发现流程跳过指向工作区内外文件的 SKILL.md 软链接。
func TestSkillLoaderRejectsSymlinkEntries(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/shared/SKILL.md", "shared", "shared", "shared body")
	insideDir := filepath.Join(workDir, "skills", "inside-link")
	if err := os.MkdirAll(insideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared/SKILL.md", filepath.Join(insideDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "SKILL.md")
	if err := os.WriteFile(outsidePath, []byte("---\nname: escaped\ndescription: escaped\n---\noutside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideSkillDir := filepath.Join(workDir, "skills", "outside-link")
	if err := os.MkdirAll(outsideSkillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(outsideSkillDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "shared" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
}

// TestSkillLoaderFiltersIneligibleSkills 验证操作系统、二进制、环境变量和模型调用开关会过滤不合格 Skill。
func TestSkillLoaderFiltersIneligibleSkills(t *testing.T) {
	workDir := t.TempDir()
	writeRawWorkspaceSkill(t, workDir, "skills/os/SKILL.md", `---
name: os-only
description: OS restricted
metadata:
  openclaw:
    os: [darwin]
---
Body`)
	writeRawWorkspaceSkill(t, workDir, "skills/bin/SKILL.md", `---
name: bin-only
description: Binary restricted
metadata:
  openclaw:
    requires:
      bins: [ffmpeg]
---
Body`)
	writeRawWorkspaceSkill(t, workDir, "skills/env/SKILL.md", `---
name: env-only
description: Environment restricted
metadata:
  openclaw:
    requires:
      env: [SECRET_TOKEN]
---
Body`)
	writeRawWorkspaceSkill(t, workDir, "skills/disabled/SKILL.md", `---
name: disabled
description: Disabled invocation
disable-model-invocation: true
---
disabled-body-secret`)
	writeWorkspaceSkill(t, workDir, "skills/eligible/SKILL.md", "eligible", "eligible", "eligible body")

	env := SkillEnvironment{
		GOOS:      "linux",
		BinLookup: func(string) bool { return false },
		EnvLookup: func(string) bool { return false },
	}
	snapshot, err := NewSkillLoader(workDir).Discover(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "eligible" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(),
		"skill_os_ineligible", "skill_missing_binary", "skill_missing_environment", "skill_model_invocation_disabled")
	for _, diagnostic := range snapshot.Diagnostics() {
		if strings.Contains(diagnostic.Message, "token-value") || strings.Contains(diagnostic.Message, "disabled-body-secret") {
			t.Fatalf("diagnostic leaked secret: %#v", diagnostic)
		}
	}
}

// TestSkillLoaderMapsWindowsToWin32AndAcceptsRequirements 验证 Windows 名称兼容转换及完整环境条件匹配。
func TestSkillLoaderMapsWindowsToWin32AndAcceptsRequirements(t *testing.T) {
	workDir := t.TempDir()
	writeRawWorkspaceSkill(t, workDir, "skills/windows/SKILL.md", `---
name: windows-tool
description: Windows tool
metadata:
  openclaw:
    os: [win32]
    requires:
      bins: [helper]
      env: [HELPER_TOKEN]
---
Body`)

	env := SkillEnvironment{
		GOOS:      "windows",
		BinLookup: func(name string) bool { return name == "helper" },
		EnvLookup: func(name string) bool { return name == "HELPER_TOKEN" },
	}
	snapshot, err := NewSkillLoader(workDir).Discover(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "windows-tool" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
}

// TestSkillLoaderReturnsErrorForMissingWorkspace 验证工作区不存在时发现流程返回错误。
func TestSkillLoaderReturnsErrorForMissingWorkspace(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := NewSkillLoader(missing).Discover(testSkillEnvironment()); err == nil {
		t.Fatal("Discover() error = nil")
	}
}

// writeWorkspaceSkill 根据结构化字段生成测试用 SKILL.md 并写入指定工作区路径。
func writeWorkspaceSkill(t *testing.T, workDir, relativePath, name, description, body string) {
	t.Helper()
	writeRawWorkspaceSkill(t, workDir, relativePath,
		fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, description, body))
}

// writeRawWorkspaceSkill 将原始字符串内容作为测试用 SKILL.md 写入工作区。
func writeRawWorkspaceSkill(t *testing.T, workDir, relativePath, content string) {
	t.Helper()
	writeRawWorkspaceBytes(t, workDir, relativePath, []byte(content))
}

// writeRawWorkspaceBytes 创建父目录并写入原始字节，供无效编码和超大文件测试使用。
func writeRawWorkspaceBytes(t *testing.T, workDir, relativePath string, content []byte) {
	t.Helper()
	path := filepath.Join(workDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// testSkillEnvironment 返回允许所有命令和环境变量的固定 Linux 测试环境。
func testSkillEnvironment() SkillEnvironment {
	return SkillEnvironment{
		GOOS:      "linux",
		BinLookup: func(string) bool { return true },
		EnvLookup: func(string) bool { return true },
	}
}

// requireDiagnosticCodes 断言诊断列表包含测试期望的全部错误代码。
func requireDiagnosticCodes(t *testing.T, diagnostics []SkillDiagnostic, codes ...string) {
	t.Helper()
	available := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		available[diagnostic.Code] = true
	}
	for _, code := range codes {
		if !available[code] {
			t.Fatalf("diagnostics = %#v, missing %q", diagnostics, code)
		}
	}
}
