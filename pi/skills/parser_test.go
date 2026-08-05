package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseSkillMDParsesStrictFrontmatter 验证解析器能够处理严格 Frontmatter、CRLF、多行描述和运行要求。
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

// TestParseSkillMDRejectsInvalidContent 验证各种无效 SKILL.md 内容会返回对应的稳定错误代码。
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

// TestParseSkillMDAcceptsBoundaryLengths 验证名称和描述恰好达到长度上限时仍可解析。
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

// TestParseSkillMDRejectsYAMLDecodedXMLControlCharacters 验证 YAML 解码产生的非法 XML 控制字符会被拒绝。
func TestParseSkillMDRejectsYAMLDecodedXMLControlCharacters(t *testing.T) {
	content := []byte("---\nname: control\ndescription: \"\\0\"\n---\nBody")

	_, err := parseSkillMD(content)
	var parseErr *skillParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "skill_frontmatter_invalid" {
		t.Fatalf("parseSkillMD() error = %v", err)
	}
}

// TestParseSkillMDRedactsUnderlyingYAMLErrors 验证 YAML 解析错误不会泄露 Frontmatter 中的敏感内容。
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

// writeSkill 在测试工作区的 .claw/skills 目录中创建指定内容的 SKILL.md。
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
