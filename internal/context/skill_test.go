package context

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMDParsesStrictFrontmatter(t *testing.T) {
	content := []byte("---\r\nname: code-review\r\ndescription: |\r\n  Review code carefully.\r\n  Report concrete risks.\r\ndisable-model-invocation: true\r\nmetadata:\r\n  openclaw:\r\n    os: [darwin, linux]\r\n    requires:\r\n      bins: [git]\r\n      env: [REVIEW_TOKEN]\r\n---\r\n# Guide\r\nKeep this --- marker.\r\n")

	got, err := parseSkillMD(content)
	if err != nil {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if got.Name != "code-review" {
		t.Fatalf("Name = %q", got.Name)
	}
	if got.Description != "Review code carefully.\nReport concrete risks." {
		t.Fatalf("Description = %q", got.Description)
	}
	if got.Body != "# Guide\nKeep this --- marker." {
		t.Fatalf("Body = %q", got.Body)
	}
	if !got.DisableModelInvocation {
		t.Fatal("DisableModelInvocation = false")
	}
	if strings.Join(got.OS, ",") != "darwin,linux" ||
		strings.Join(got.RequiredBins, ",") != "git" ||
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
		{name: "NUL", content: []byte("---\nname: valid\ndescription: useful\n---\nA\x00B"), wantCode: "skill_binary_content"},
		{name: "invalid UTF-8", content: append([]byte("---\nname: valid\ndescription: useful\n---\n"), 0xff), wantCode: "skill_not_utf8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSkillMD(tt.content)
			var parseErr *skillParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("parseSkillMD() error = %v, want *skillParseError", err)
			}
			if parseErr.Code != tt.wantCode {
				t.Fatalf("error code = %q, want %q", parseErr.Code, tt.wantCode)
			}
		})
	}
}

func TestParseSkillMDAcceptsBoundaryLengths(t *testing.T) {
	name := strings.Repeat("a", 64)
	description := strings.Repeat("界", 1024)
	content := []byte("---\nname: " + name + "\ndescription: " + description + "\n---\nBody")

	got, err := parseSkillMD(content)
	if err != nil {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if got.Name != name || got.Description != description || got.Body != "Body" {
		t.Fatalf("parseSkillMD() = %#v", got)
	}
}

func TestParseSkillMDRejectsYAMLDecodedXMLControlCharacters(t *testing.T) {
	content := []byte("---\nname: control\ndescription: \"\\0\"\n---\nBody")

	_, err := parseSkillMD(content)
	var parseErr *skillParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "skill_frontmatter_invalid" {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
}

func TestParseSkillMDRedactsUnderlyingYAMLErrors(t *testing.T) {
	content := []byte("---\nname: [frontmatter-secret-token]\ndescription: useful\n---\nBody")

	_, err := parseSkillMD(content)
	var parseErr *skillParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "skill_frontmatter_invalid" {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
	if parseErr.Message != "SKILL.md Frontmatter YAML 无效" || strings.Contains(parseErr.Message, "frontmatter-secret-token") {
		t.Fatalf("parse error was not redacted: %#v", parseErr)
	}
}

func writeSkill(t *testing.T, workDir string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(workDir, ".claw", "skills", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
