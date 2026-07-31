// Package context composes workspace-specific context for Agent runs.
package context

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const maxSkillFileBytes = 256 * 1024

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type parsedSkill struct {
	Name                   string
	Description            string
	Body                   string
	OS                     []string
	RequiredBins           []string
	RequiredEnv            []string
	DisableModelInvocation bool
}

// SkillLoader loads standard Agent Skills from one workspace.
type SkillLoader struct {
	workDir string
}

// NewSkillLoader creates a loader rooted at workDir.
func NewSkillLoader(workDir string) *SkillLoader {
	return &SkillLoader{workDir: workDir}
}

func readRootRegularFile(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return root.ReadFile(name)
}

type skillFrontmatter struct {
	Name                   string            `yaml:"name"`
	Description            string            `yaml:"description"`
	DisableModelInvocation bool              `yaml:"disable-model-invocation"`
	Metadata               skillMetadataRoot `yaml:"metadata"`
}

type skillMetadataRoot struct {
	OpenClaw openClawMetadata `yaml:"openclaw"`
}

type openClawMetadata struct {
	OS       []string             `yaml:"os"`
	Requires openClawRequirements `yaml:"requires"`
}

type openClawRequirements struct {
	Bins []string `yaml:"bins"`
	Env  []string `yaml:"env"`
}

type skillParseError struct {
	Code    string
	Message string
}

func (e *skillParseError) Error() string {
	return e.Message
}

func newSkillParseError(code string, message string) error {
	return &skillParseError{Code: code, Message: message}
}

func parseSkillMD(content []byte) (parsedSkill, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return parsedSkill{}, newSkillParseError("skill_binary_content", "SKILL.md 包含 NUL 字节")
	}
	if !utf8.Valid(content) {
		return parsedSkill{}, newSkillParseError("skill_not_utf8", "SKILL.md 不是有效的 UTF-8 文本")
	}

	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return parsedSkill{}, newSkillParseError("skill_frontmatter_missing", "SKILL.md 缺少首行 Frontmatter")
	}

	closing := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			closing = index
			break
		}
	}
	if closing == -1 {
		return parsedSkill{}, newSkillParseError("skill_frontmatter_invalid", "SKILL.md Frontmatter 未闭合")
	}

	var metadata skillFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &metadata); err != nil {
		return parsedSkill{}, newSkillParseError("skill_frontmatter_invalid", "SKILL.md Frontmatter YAML 无效")
	}

	name := strings.TrimSpace(metadata.Name)
	if name == "" {
		return parsedSkill{}, newSkillParseError("skill_name_missing", "Skill name 不能为空")
	}
	if utf8.RuneCountInString(name) > 64 || !skillNamePattern.MatchString(name) {
		return parsedSkill{}, newSkillParseError("skill_name_invalid", "Skill name 格式无效")
	}

	description := strings.TrimSpace(metadata.Description)
	if description == "" {
		return parsedSkill{}, newSkillParseError("skill_description_missing", "Skill description 不能为空")
	}
	if utf8.RuneCountInString(description) > 1024 {
		return parsedSkill{}, newSkillParseError("skill_description_too_long", "Skill description 超过 1024 个字符")
	}
	if !isValidXMLText(description) ||
		!allValidXMLText(metadata.Metadata.OpenClaw.OS) ||
		!allValidXMLText(metadata.Metadata.OpenClaw.Requires.Bins) ||
		!allValidXMLText(metadata.Metadata.OpenClaw.Requires.Env) {
		return parsedSkill{}, newSkillParseError("skill_frontmatter_invalid", "SKILL.md Frontmatter 包含非法控制字符")
	}

	body := strings.TrimSpace(strings.Join(lines[closing+1:], "\n"))
	if body == "" {
		return parsedSkill{}, newSkillParseError("skill_body_empty", "Skill Body 不能为空")
	}

	return parsedSkill{
		Name:                   name,
		Description:            description,
		Body:                   body,
		OS:                     append([]string(nil), metadata.Metadata.OpenClaw.OS...),
		RequiredBins:           append([]string(nil), metadata.Metadata.OpenClaw.Requires.Bins...),
		RequiredEnv:            append([]string(nil), metadata.Metadata.OpenClaw.Requires.Env...),
		DisableModelInvocation: metadata.DisableModelInvocation,
	}, nil
}

func allValidXMLText(values []string) bool {
	for _, value := range values {
		if !isValidXMLText(value) {
			return false
		}
	}
	return true
}
