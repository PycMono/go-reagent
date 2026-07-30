package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMD(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Skill
		wantErr bool
	}{
		{
			name: "YAML metadata and body separator",
			content: `---
name: review
description: |
  Review code carefully.
  Report concrete risks.
---
# Guide
Keep this --- marker in the body.
---
Done.`,
			want: Skill{
				Name:        "review",
				Description: "Review code carefully.\nReport concrete risks.",
				Body:        "# Guide\nKeep this --- marker in the body.\n---\nDone.",
			},
		},
		{
			name:    "CRLF and quoted values",
			content: "---\r\nname: \"release\"\r\ndescription: 'Ship safely'\r\n---\r\nRun checks.\r\n",
			want:    Skill{Name: "release", Description: "Ship safely", Body: "Run checks."},
		},
		{
			name:    "plain Markdown",
			content: "# Plain skill\nNo frontmatter.",
			want: Skill{
				Name:        "Unknown Skill",
				Description: "No description provided.",
				Body:        "# Plain skill\nNo frontmatter.",
			},
		},
		{
			name:    "empty metadata uses defaults",
			content: "---\nname: '  '\ndescription: ''\n---\nBody",
			want: Skill{
				Name:        "Unknown Skill",
				Description: "No description provided.",
				Body:        "Body",
			},
		},
		{name: "unclosed frontmatter", content: "---\nname: broken", wantErr: true},
		{name: "invalid YAML", content: "---\nname: [broken\n---\nBody", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSkillMD(tt.content)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSkillMD() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("parseSkillMD() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSkillLoaderLoadsValidSkillsInPathOrder(t *testing.T) {
	workDir := t.TempDir()
	writeSkill(t, workDir, "zeta/SKILL.md", "---\nname: Zeta\ndescription: Last\n---\nZ body")
	writeSkill(t, workDir, "alpha/SKILL.md", "---\nname: Alpha\ndescription: First\n---\nA body")
	writeSkill(t, workDir, "middle/SKILL.md", "---\nname: [broken\n---\nBad")
	writeSkill(t, workDir, "ignored/skill.md", "---\nname: ignored\n---\nignored")

	got := NewSkillLoader(workDir).LoadAll()
	if strings.Count(got, "### 可用专业技能 (Agent Skills)") != 1 {
		t.Fatalf("skill header count in %q", got)
	}
	alpha := strings.Index(got, "#### 技能名称: Alpha")
	zeta := strings.Index(got, "#### 技能名称: Zeta")
	if alpha < 0 || zeta < 0 || alpha >= zeta {
		t.Fatalf("skills not rendered in path order: %q", got)
	}
	for _, want := range []string{
		"**触发条件**: First",
		"**执行指南**:\nA body",
		"**触发条件**: Last",
		"**执行指南**:\nZ body",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered skills missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "broken") || strings.Contains(got, "ignored") {
		t.Fatalf("invalid or non-SKILL file was rendered: %q", got)
	}
}

func TestSkillLoaderReturnsEmptyWithoutValidSkills(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, workDir string)
	}{
		{name: "missing skills directory"},
		{
			name: "only malformed skill",
			setup: func(t *testing.T, workDir string) {
				writeSkill(t, workDir, "broken/SKILL.md", "---\nname: [broken\n---\nBad")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, workDir)
			}
			if got := NewSkillLoader(workDir).LoadAll(); got != "" {
				t.Fatalf("LoadAll() = %q, want empty", got)
			}
		})
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
