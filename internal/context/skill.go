// Package context composes workspace-specific context for Agent runs.
package context

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Skill is retained temporarily for the legacy renderer while discovery is migrated.
type Skill = parsedSkill

// SkillLoader loads standard Agent Skills from one workspace.
type SkillLoader struct {
	workDir string
}

// NewSkillLoader creates a loader rooted at workDir.
func NewSkillLoader(workDir string) *SkillLoader {
	return &SkillLoader{workDir: workDir}
}

// LoadAll renders every valid SKILL.md below .claw/skills for Prompt injection.
func (s *SkillLoader) LoadAll() string {
	root, err := os.OpenRoot(s.workDir)
	if err != nil {
		return ""
	}
	defer root.Close()

	skillBaseDir := filepath.Join(".claw", "skills")
	info, err := root.Stat(skillBaseDir)
	if err != nil || !info.IsDir() {
		return ""
	}

	skills := make([]Skill, 0)
	if err := fs.WalkDir(root.FS(), filepath.ToSlash(skillBaseDir), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}

		content, err := readRootRegularFile(root, filepath.FromSlash(path))
		if err != nil {
			return nil
		}
		skill, err := parseSkillMD(content)
		if err != nil {
			return nil
		}
		skills = append(skills, skill)
		return nil
	}); err != nil || len(skills) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n### 可用专业技能 (Agent Skills)\n")
	builder.WriteString("以下是你拥有的标准化外挂技能，请在符合 description 描述的场景下严格遵循其正文指令：\n\n")
	for _, skill := range skills {
		fmt.Fprintf(&builder, "#### 技能名称: %s\n", skill.Name)
		fmt.Fprintf(&builder, "**触发条件**: %s\n\n", skill.Description)
		builder.WriteString("**执行指南**:\n")
		builder.WriteString(skill.Body)
		builder.WriteString("\n\n---\n")
	}
	return builder.String()
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
		return parsedSkill{}, newSkillParseError("skill_frontmatter_invalid", fmt.Sprintf("解析 SKILL.md Frontmatter 失败: %v", err))
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
