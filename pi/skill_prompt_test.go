package pi

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestRenderSkillPromptContainsEscapedCatalogMetadata 验证 Skill 目录排序正确、元数据完成 XML 转义且包含渐进读取指令。
func TestRenderSkillPromptContainsEscapedCatalogMetadata(t *testing.T) {
	snapshot := newSkillSnapshot([]SkillSummary{
		{
			Name:        "zeta",
			Description: `Use <code> & "tests" with 'care'`,
			Location:    "skills/zeta/SKILL.md",
			Version:     "sha256:fedcba9876543210",
		},
		{
			Name:        "alpha",
			Description: "Alpha skill",
			Location:    "skills/alpha/SKILL.md",
			Version:     "sha256:0123456789abcdef",
		},
	}, nil)

	prompt, report := renderSkillPrompt(snapshot)
	for _, want := range []string{
		"<available_skills>", "<name>alpha</name>", "<name>zeta</name>",
		`Use &lt;code&gt; &amp; &quot;tests&quot; with &apos;care&apos;`,
		"<location>skills/zeta/SKILL.md</location>",
		"<version>sha256:fedcba9876543210</version>",
		"必须先使用 read", "Use offset=N to continue", "SKILL.md 所在目录",
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

// TestRenderSkillPromptOmitsEmptyCatalog 验证 nil 或空快照不会生成 Skill Prompt。
func TestRenderSkillPromptOmitsEmptyCatalog(t *testing.T) {
	for _, snapshot := range []*SkillSnapshot{nil, newSkillSnapshot(nil, nil)} {
		prompt, report := renderSkillPrompt(snapshot)
		if prompt != "" || report != (SkillPromptReport{}) {
			t.Fatalf("renderSkillPrompt() = %q, %#v", prompt, report)
		}
	}
}

// TestRenderSkillPromptHonorsCountAndRuneBudgets 验证渲染结果遵守技能数量和 Unicode 字符预算。
func TestRenderSkillPromptHonorsCountAndRuneBudgets(t *testing.T) {
	skills := make([]SkillSummary, 0, maxSkillsInPrompt+10)
	for index := 0; index < maxSkillsInPrompt+10; index++ {
		skills = append(skills, SkillSummary{
			Name:        fmt.Sprintf("skill-%03d", index),
			Description: strings.Repeat("说明", 512),
			Location:    fmt.Sprintf("skills/skill-%03d/SKILL.md", index),
			Version:     "sha256:0123456789abcdef",
		})
	}

	prompt, report := renderSkillPrompt(newSkillSnapshot(skills, nil))
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

// TestRenderSkillPromptPrioritizesAllFittingIdentitiesOverDescriptions 验证预算不足时优先保留 Skill 身份，再缩短描述。
func TestRenderSkillPromptPrioritizesAllFittingIdentitiesOverDescriptions(t *testing.T) {
	skills := make([]SkillSummary, 0, 10)
	for index := 0; index < 10; index++ {
		skills = append(skills, SkillSummary{
			Name:        fmt.Sprintf("skill-%02d", index),
			Description: strings.Repeat("very-long-description-", 100),
			Location:    fmt.Sprintf("skills/skill-%02d/SKILL.md", index),
			Version:     "sha256:0123456789abcdef",
		})
	}

	prompt, report := renderSkillPrompt(newSkillSnapshot(skills, nil))
	if report.IncludedSkills != 10 || report.OmittedSkills != 0 {
		t.Fatalf("report = %#v", report)
	}
	if !strings.Contains(prompt, "skills/skill-09/SKILL.md") {
		t.Fatalf("later identity was displaced by descriptions: %q", prompt)
	}
	if report.ShortenedDescriptions == 0 || !report.Truncated {
		t.Fatalf("descriptions were not shortened: %#v", report)
	}
}

// TestRenderSkillPromptProducesWellFormedXMLForControlCharacters 验证非法控制字符被清理后仍能生成合法 XML。
func TestRenderSkillPromptProducesWellFormedXMLForControlCharacters(t *testing.T) {
	snapshot := newSkillSnapshot([]SkillSummary{{
		Name:        "control",
		Description: "bad\x00description",
		Location:    "skills/\x01control/SKILL.md",
		Version:     "sha256:0123456789abcdef",
	}}, nil)

	prompt, _ := renderSkillPrompt(snapshot)
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
