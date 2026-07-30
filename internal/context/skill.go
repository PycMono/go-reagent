// Package context composes workspace-specific context for Agent runs.
package context

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultSkillName        = "Unknown Skill"
	defaultSkillDescription = "No description provided."
)

// Skill defines the normalized metadata and instructions loaded from SKILL.md.
type Skill struct {
	Name        string
	Description string
	Body        string
}

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
		skill, err := parseSkillMD(string(content))
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

type skillMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func parseSkillMD(content string) (Skill, error) {
	skill := Skill{
		Name:        defaultSkillName,
		Description: defaultSkillDescription,
		Body:        content,
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return skill, nil
	}

	closing := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			closing = index
			break
		}
	}
	if closing == -1 {
		return Skill{}, errors.New("skill frontmatter is not closed")
	}

	var metadata skillMetadata
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &metadata); err != nil {
		return Skill{}, fmt.Errorf("parse skill frontmatter: %w", err)
	}
	if name := strings.TrimSpace(metadata.Name); name != "" {
		skill.Name = name
	}
	if description := strings.TrimSpace(metadata.Description); description != "" {
		skill.Description = description
	}
	skill.Body = strings.TrimSpace(strings.Join(lines[closing+1:], "\n"))
	return skill, nil
}
