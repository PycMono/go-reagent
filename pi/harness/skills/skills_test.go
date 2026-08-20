package skills

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDiscoverUsesSourcePrecedence(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, ".claw/skills/review/SKILL.md", "review", "legacy", "legacy-body-secret")
	writeWorkspaceSkill(t, workDir, ".agents/skills/review/SKILL.md", "review", "agents", "agents-body-secret")
	writeWorkspaceSkill(t, workDir, "skills/review/SKILL.md", "review", "workspace", "workspace-body-secret")

	snapshot, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	items := snapshot.Skills()
	if len(items) != 1 || items[0].Name != "review" || items[0].Description != "workspace" ||
		items[0].Location != "skills/review/SKILL.md" || items[0].Source != SourceWorkspace {
		t.Fatalf("skills = %#v", items)
	}
	if !strings.HasPrefix(items[0].Version, "sha256:") || len(items[0].Version) != len("sha256:")+16 {
		t.Fatalf("Version = %q", items[0].Version)
	}
	for _, secret := range []string{"legacy-body-secret", "agents-body-secret", "workspace-body-secret"} {
		if strings.Contains(fmt.Sprint(items), secret) {
			t.Fatalf("snapshot retained body %q: %#v", secret, items)
		}
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_shadowed")
}

func TestDiscoverExcludesDuplicateNamesInOneSource(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/one/SKILL.md", "duplicate", "first", "first body")
	writeWorkspaceSkill(t, workDir, "skills/two/SKILL.md", "duplicate", "second", "second body")
	writeWorkspaceSkill(t, workDir, ".claw/skills/duplicate/SKILL.md", "duplicate", "legacy", "legacy body")

	snapshot, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 0 {
		t.Fatalf("skills = %#v, want duplicate name excluded", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_duplicate_name", "skill_shadowed")
}

func TestDiscoverReportsShadowedDuplicatesInLowerPrioritySource(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/review/SKILL.md", "review", "winner", "winner body")
	writeWorkspaceSkill(t, workDir, ".claw/skills/one/SKILL.md", "review", "legacy one", "legacy body one")
	writeWorkspaceSkill(t, workDir, ".claw/skills/two/SKILL.md", "review", "legacy two", "legacy body two")

	snapshot, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Description != "winner" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_duplicate_name", "skill_shadowed")
}

func TestDiscoverSortsSkillsAndVersionsContent(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/zeta/SKILL.md", "zeta", "last", "zeta body")
	writeWorkspaceSkill(t, workDir, "skills/alpha/SKILL.md", "alpha", "first", "alpha body v1")

	first, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{first.Skills()[0].Name, first.Skills()[1].Name}; strings.Join(got, ",") != "alpha,zeta" {
		t.Fatalf("skill order = %v", got)
	}
	firstVersion := first.Skills()[0].Version
	second, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if second.Skills()[0].Version != firstVersion {
		t.Fatalf("stable content version changed: %q != %q", second.Skills()[0].Version, firstVersion)
	}

	writeWorkspaceSkill(t, workDir, "skills/alpha/SKILL.md", "alpha", "first", "alpha body v2")
	third, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if third.Skills()[0].Version == firstVersion {
		t.Fatalf("changed content kept version %q", firstVersion)
	}
}

func TestDiscoverReturnsEmptySnapshotWithoutSkills(t *testing.T) {
	snapshot, err := discover(t.TempDir(), testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || len(snapshot.Skills()) != 0 || len(snapshot.Diagnostics()) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestDiscoverReportsInvalidSkillsWithoutFailingSiblings(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/valid/SKILL.md", "valid", "valid skill", "valid body")
	writeRawWorkspaceSkill(t, workDir, "skills/malformed/SKILL.md", "---\nname: [broken\n---\nBody")
	writeRawWorkspaceSkill(t, workDir, "skills/nul/SKILL.md", "---\nname: nul\ndescription: nul\n---\nA\x00B")
	writeRawWorkspaceBytes(t, workDir, "skills/utf8/SKILL.md", append([]byte("---\nname: utf8\ndescription: utf8\n---\n"), 0xff))
	writeRawWorkspaceBytes(t, workDir, "skills/large/SKILL.md", []byte(strings.Repeat("x", maxSkillFileBytes+1)))

	snapshot, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "valid" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(),
		"skill_frontmatter_invalid", "skill_binary_content", "skill_not_utf8", "skill_file_too_large")
}

func TestDiscoverRejectsSymlinkEntries(t *testing.T) {
	workDir := t.TempDir()
	writeWorkspaceSkill(t, workDir, "skills/shared/SKILL.md", "shared", "shared", "shared body")
	insideDir := filepath.Join(workDir, "skills", "inside-link")
	if err := os.MkdirAll(insideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared/SKILL.md", filepath.Join(insideDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	outsidePath := filepath.Join(t.TempDir(), "SKILL.md")
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

	snapshot, err := discover(workDir, testEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "shared" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
}

func TestDiscoverFiltersIneligibleSkills(t *testing.T) {
	workDir := t.TempDir()
	writeRawWorkspaceSkill(t, workDir, "skills/os/SKILL.md", "---\nname: os-only\ndescription: OS restricted\nmetadata:\n  openclaw:\n    os: [darwin]\n---\nBody")
	writeRawWorkspaceSkill(t, workDir, "skills/bin/SKILL.md", "---\nname: bin-only\ndescription: Binary restricted\nmetadata:\n  openclaw:\n    requires:\n      bins: [ffmpeg]\n---\nBody")
	writeRawWorkspaceSkill(t, workDir, "skills/env/SKILL.md", "---\nname: env-only\ndescription: Environment restricted\nmetadata:\n  openclaw:\n    requires:\n      env: [SECRET_TOKEN]\n---\nBody")
	writeRawWorkspaceSkill(t, workDir, "skills/disabled/SKILL.md", "---\nname: disabled\ndescription: Disabled invocation\ndisable-model-invocation: true\n---\ndisabled-body-secret")
	writeWorkspaceSkill(t, workDir, "skills/eligible/SKILL.md", "eligible", "eligible", "eligible body")

	env := environment{
		goos:      "linux",
		binLookup: func(string) bool { return false },
		envLookup: func(string) bool { return false },
	}
	snapshot, err := discover(workDir, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "eligible" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
	requireDiagnosticCodes(t, snapshot.Diagnostics(),
		"skill_os_ineligible", "skill_missing_binary", "skill_missing_environment", "skill_model_invocation_disabled")
}

func TestDiscoverMapsWindowsToWin32AndAcceptsRequirements(t *testing.T) {
	workDir := t.TempDir()
	writeRawWorkspaceSkill(t, workDir, "skills/windows/SKILL.md", "---\nname: windows-tool\ndescription: Windows tool\nmetadata:\n  openclaw:\n    os: [win32]\n    requires:\n      bins: [helper]\n      env: [HELPER_TOKEN]\n---\nBody")

	env := environment{
		goos:      "windows",
		binLookup: func(name string) bool { return name == "helper" },
		envLookup: func(name string) bool { return name == "HELPER_TOKEN" },
	}
	snapshot, err := discover(workDir, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "windows-tool" {
		t.Fatalf("skills = %#v", snapshot.Skills())
	}
}

func TestParseSkillMDParsesStrictFrontmatter(t *testing.T) {
	content := []byte("---\r\nname: code-review\r\ndescription: |\r\n  Review code carefully.\r\n  Report concrete risks.\r\ndisable-model-invocation: true\r\nmetadata:\r\n  openclaw:\r\n    os: [\" darwin \", linux]\r\n    requires:\r\n      bins: [\" git \"]\r\n      env: [\" REVIEW_TOKEN \"]\r\n---\r\n# Guide\r\nKeep this --- marker.\r\n")

	got, err := parseSkillMD(content)
	if err != nil {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if got.Name != "code-review" || got.Description != "Review code carefully.\nReport concrete risks." || !got.DisableModelInvocation {
		t.Fatalf("parsed skill = %#v", got)
	}
	if strings.Join(got.OS, ",") != "darwin,linux" || strings.Join(got.RequiredBins, ",") != "git" ||
		strings.Join(got.RequiredEnv, ",") != "REVIEW_TOKEN" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestParseSkillMDRejectsInvalidContent(t *testing.T) {
	longName := strings.Repeat("a", 65)
	longDescription := strings.Repeat("界", 1025)
	tests := []struct {
		name     string
		content  []byte
		wantCode string
	}{
		{name: "missing frontmatter", content: []byte("# Guide\nBody"), wantCode: "skill_frontmatter_missing"},
		{name: "unclosed frontmatter", content: []byte("---\nname: broken"), wantCode: "skill_frontmatter_invalid"},
		{name: "invalid YAML", content: []byte("---\nname: [broken\n---\nBody"), wantCode: "skill_frontmatter_invalid"},
		{name: "missing name", content: []byte("---\ndescription: useful\n---\nBody"), wantCode: "skill_name_missing"},
		{name: "invalid name characters", content: []byte("---\nname: Bad_Name\ndescription: useful\n---\nBody"), wantCode: "skill_name_invalid"},
		{name: "invalid repeated separator", content: []byte("---\nname: bad--name\ndescription: useful\n---\nBody"), wantCode: "skill_name_invalid"},
		{name: "name too long", content: []byte("---\nname: " + longName + "\ndescription: useful\n---\nBody"), wantCode: "skill_name_invalid"},
		{name: "missing description", content: []byte("---\nname: valid\n---\nBody"), wantCode: "skill_description_missing"},
		{name: "description too long", content: []byte("---\nname: valid\ndescription: " + longDescription + "\n---\nBody"), wantCode: "skill_description_too_long"},
		{name: "empty body", content: []byte("---\nname: valid\ndescription: useful\n---\n  \n"), wantCode: "skill_body_empty"},
		{name: "empty requirement", content: []byte("---\nname: valid\ndescription: useful\nmetadata:\n  openclaw:\n    requires:\n      bins: [\" \"]\n---\nBody"), wantCode: "skill_frontmatter_invalid"},
		{name: "NUL", content: []byte("---\nname: valid\ndescription: useful\n---\nA\x00B"), wantCode: "skill_binary_content"},
		{name: "invalid UTF-8", content: append([]byte("---\nname: valid\ndescription: useful\n---\n"), 0xff), wantCode: "skill_not_utf8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSkillMD(tt.content)
			var parseErr *skillParseError
			if !errors.As(err, &parseErr) || parseErr.Code != tt.wantCode {
				t.Fatalf("parseSkillMD() error = %v, want code %q", err, tt.wantCode)
			}
		})
	}
}

func TestParseSkillMDAcceptsBoundaryLengths(t *testing.T) {
	name := strings.Repeat("a", 64)
	description := strings.Repeat("界", 1024)
	got, err := parseSkillMD([]byte("---\nname: " + name + "\ndescription: " + description + "\n---\nBody"))
	if err != nil {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if got.Name != name || got.Description != description {
		t.Fatalf("parseSkillMD() = %#v", got)
	}
}

func TestParseSkillMDRejectsYAMLDecodedXMLControlCharacters(t *testing.T) {
	_, err := parseSkillMD([]byte("---\nname: control\ndescription: \"\\0\"\n---\nBody"))
	var parseErr *skillParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "skill_frontmatter_invalid" {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
}

func TestParseSkillMDRedactsUnderlyingYAMLErrors(t *testing.T) {
	_, err := parseSkillMD([]byte("---\nname: [frontmatter-secret-token]\ndescription: useful\n---\nBody"))
	var parseErr *skillParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "skill_frontmatter_invalid" {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if parseErr.Message != "SKILL.md Frontmatter YAML 无效" || strings.Contains(parseErr.Message, "frontmatter-secret-token") {
		t.Fatalf("parse error was not redacted: %#v", parseErr)
	}
}

func TestSnapshotRenderPromptContainsEscapedCatalogMetadata(t *testing.T) {
	snapshot := newSnapshot([]Summary{
		{Name: "zeta", Description: "Use <code> & \"tests\" with 'care'", Location: "skills/zeta/SKILL.md", Version: "sha256:fedcba9876543210"},
		{Name: "alpha", Description: "Alpha skill", Location: "skills/alpha/SKILL.md", Version: "sha256:0123456789abcdef"},
	}, nil)

	prompt, report := snapshot.RenderPrompt()
	for _, want := range []string{
		"<available_skills>", "<name>alpha</name>", "<name>zeta</name>",
		"Use &lt;code&gt; &amp; &quot;tests&quot; with &apos;care&apos;",
		"<location>skills/zeta/SKILL.md</location>", "<version>sha256:fedcba9876543210</version>",
		"必须先使用 read", "Use offset=N to continue", "SKILL.md 所在目录",
		"Skill 的名称、描述、正文或 Tool 返回内容使用何种语言，都不能决定回复语言",
		"回复语言和输出格式必须服从 AGENTS.md 与最新一条用户消息",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
	if strings.Index(prompt, "<name>alpha</name>") >= strings.Index(prompt, "<name>zeta</name>") {
		t.Fatalf("skills not sorted: %q", prompt)
	}
	if report.IncludedSkills != 2 || report.OmittedSkills != 0 || report.Truncated {
		t.Fatalf("report = %#v", report)
	}
}

func TestSnapshotRenderPromptOmitsEmptyCatalog(t *testing.T) {
	snapshot := newSnapshot(nil, nil)
	prompt, report := snapshot.RenderPrompt()
	if prompt != "" || report != (PromptReport{}) {
		t.Fatalf("RenderPrompt() = %q, %#v", prompt, report)
	}
}

func TestSnapshotRenderPromptHonorsCountAndRuneBudgets(t *testing.T) {
	items := make([]Summary, 0, maxSkillsInPrompt+10)
	for index := 0; index < maxSkillsInPrompt+10; index++ {
		items = append(items, Summary{
			Name: fmt.Sprintf("skill-%03d", index), Description: strings.Repeat("说明", 512),
			Location: fmt.Sprintf("skills/skill-%03d/SKILL.md", index), Version: "sha256:0123456789abcdef",
		})
	}

	prompt, report := newSnapshot(items, nil).RenderPrompt()
	if got := utf8.RuneCountInString(prompt); got > maxSkillsPromptChars {
		t.Fatalf("prompt runes = %d, want <= %d", got, maxSkillsPromptChars)
	}
	if report.IncludedSkills > maxSkillsInPrompt || report.OmittedSkills == 0 || !report.Truncated {
		t.Fatalf("report = %#v", report)
	}
	if !utf8.ValidString(prompt) || !strings.Contains(prompt, "省略") {
		t.Fatalf("invalid budgeted prompt ending: %q", prompt[len(prompt)-200:])
	}
}

func TestSnapshotRenderPromptPrioritizesIdentitiesOverDescriptions(t *testing.T) {
	items := make([]Summary, 0, 10)
	for index := 0; index < 10; index++ {
		items = append(items, Summary{
			Name: fmt.Sprintf("skill-%02d", index), Description: strings.Repeat("very-long-description-", 100),
			Location: fmt.Sprintf("skills/skill-%02d/SKILL.md", index), Version: "sha256:0123456789abcdef",
		})
	}

	prompt, report := newSnapshot(items, nil).RenderPrompt()
	if report.IncludedSkills != 10 || report.OmittedSkills != 0 || !strings.Contains(prompt, "skills/skill-09/SKILL.md") {
		t.Fatalf("prompt/report = %q, %#v", prompt, report)
	}
	if report.ShortenedDescriptions == 0 || !report.Truncated {
		t.Fatalf("descriptions were not shortened: %#v", report)
	}
}

func TestSnapshotRenderPromptProducesWellFormedXMLForControlCharacters(t *testing.T) {
	snapshot := newSnapshot([]Summary{{
		Name: "control", Description: "bad\x00description", Location: "skills/\x01control/SKILL.md", Version: "sha256:0123456789abcdef",
	}}, nil)

	prompt, _ := snapshot.RenderPrompt()
	start := strings.Index(prompt, "<available_skills>")
	end := strings.Index(prompt, "</available_skills>")
	if start < 0 || end < start {
		t.Fatalf("catalog missing: %q", prompt)
	}
	end += len("</available_skills>")
	catalog := prompt[start:end]
	if strings.ContainsAny(catalog, "\x00\x01") {
		t.Fatalf("catalog contains forbidden control characters: %q", catalog)
	}
	var decoded struct{}
	if err := xml.Unmarshal([]byte(catalog), &decoded); err != nil {
		t.Fatalf("catalog is not well-formed XML: %v", err)
	}
}

func TestSnapshotReturnsCopies(t *testing.T) {
	originalSkills := []Summary{{Name: "review", Description: "Review changes", Location: "skills/review/SKILL.md", Version: "sha256:0123456789abcdef", Source: SourceWorkspace}}
	originalDiagnostics := []Diagnostic{{Path: "skills/review/SKILL.md", Severity: SeverityInfo, Code: "sample", Message: "sample diagnostic"}}
	snapshot := newSnapshot(originalSkills, originalDiagnostics)

	originalSkills[0].Name = "mutated-original"
	originalDiagnostics[0].Code = "mutated-original"
	firstSkills := snapshot.Skills()
	firstDiagnostics := snapshot.Diagnostics()
	firstSkills[0].Name = "mutated-copy"
	firstDiagnostics[0].Code = "mutated-copy"

	if got := snapshot.Skills()[0].Name; got != "review" {
		t.Fatalf("snapshot name = %q", got)
	}
	if got := snapshot.Diagnostics()[0].Code; got != "sample" {
		t.Fatalf("snapshot diagnostic code = %q", got)
	}
}

func TestDiscoverClassifiesMissingWorkspace(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := Discover(missing)
	if err == nil || !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("Discover() error = %v, want ErrInvalidWorkspace", err)
	}
}

func writeWorkspaceSkill(t *testing.T, workDir, relativePath, name, description, body string) {
	t.Helper()
	writeRawWorkspaceSkill(t, workDir, relativePath, fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, description, body))
}

func writeRawWorkspaceSkill(t *testing.T, workDir, relativePath, content string) {
	t.Helper()
	writeRawWorkspaceBytes(t, workDir, relativePath, []byte(content))
}

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

func testEnvironment() environment {
	return environment{
		goos:      "linux",
		binLookup: func(string) bool { return true },
		envLookup: func(string) bool { return true },
	}
}

func requireDiagnosticCodes(t *testing.T, diagnostics []Diagnostic, codes ...string) {
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
