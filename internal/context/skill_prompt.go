package context

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxSkillsInPrompt        = 150
	maxSkillsPromptChars     = 18_000
	maxSkillDescriptionChars = 1_024
)

const skillPromptInstructions = `
# 可用专业技能 (Agent Skills)
以下技能为特定任务提供专业执行指南。
当任务与某项 <description> 匹配时，必须先使用 read_file 读取该技能的 <location>。
如果 read_file 返回 "Use offset=N to continue"，必须继续读取，直到完整取得 SKILL.md 后再执行。
不要猜测未读取的技能内容，不要读取明显无关的技能。
如果 <version> 与之前看到的版本不同，必须重新读取该技能。
相对路径引用以 SKILL.md 所在目录为基准解析。

<available_skills>
`

const skillPromptClosing = "</available_skills>\n"

type SkillPromptReport struct {
	IncludedSkills        int
	OmittedSkills         int
	ShortenedDescriptions int
	Truncated             bool
}

var xmlTextReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

func renderSkillPrompt(snapshot *SkillSnapshot) (string, SkillPromptReport) {
	skills := snapshot.Skills()
	if len(skills) == 0 {
		return "", SkillPromptReport{}
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			return skills[i].Location < skills[j].Location
		}
		return skills[i].Name < skills[j].Name
	})

	included := selectSkillIdentities(skills)
	descriptions := make([]string, len(included))
	report := SkillPromptReport{
		IncludedSkills: len(included),
		OmittedSkills:  len(skills) - len(included),
	}

	base := renderSkillCatalog(included, descriptions, report.OmittedSkills)
	remaining := maxSkillsPromptChars - utf8.RuneCountInString(base)
	for index, skill := range included {
		originalRunes := []rune(skill.Description)
		candidateRunes := originalRunes
		shortened := false
		if len(candidateRunes) > maxSkillDescriptionChars {
			candidateRunes = candidateRunes[:maxSkillDescriptionChars]
			shortened = true
		}

		fullDescription := string(candidateRunes)
		fullDelta := descriptionRuneDelta(skill, fullDescription)
		if fullDelta <= remaining {
			descriptions[index] = fullDescription
			remaining -= fullDelta
		} else {
			maximum := longestDescriptionPrefix(skill, candidateRunes, remaining)
			descriptions[index] = string(candidateRunes[:maximum])
			remaining -= descriptionRuneDelta(skill, descriptions[index])
			shortened = true
		}
		if shortened {
			report.ShortenedDescriptions++
		}
	}

	report.Truncated = report.OmittedSkills > 0 || report.ShortenedDescriptions > 0
	prompt := renderSkillCatalog(included, descriptions, report.OmittedSkills)
	return prompt, report
}

func selectSkillIdentities(skills []SkillSummary) []SkillSummary {
	maximum := len(skills)
	if maximum > maxSkillsInPrompt {
		maximum = maxSkillsInPrompt
	}
	selected := make([]SkillSummary, 0, maximum)
	for index := 0; index < maximum; index++ {
		candidate := append(append([]SkillSummary(nil), selected...), skills[index])
		descriptions := make([]string, len(candidate))
		omitted := len(skills) - len(candidate)
		if utf8.RuneCountInString(renderSkillCatalog(candidate, descriptions, omitted)) > maxSkillsPromptChars {
			break
		}
		selected = candidate
	}
	return selected
}

func longestDescriptionPrefix(skill SkillSummary, runes []rune, available int) int {
	low, high := 0, len(runes)
	for low < high {
		middle := low + (high-low+1)/2
		if descriptionRuneDelta(skill, string(runes[:middle])) <= available {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return low
}

func descriptionRuneDelta(skill SkillSummary, description string) int {
	empty := utf8.RuneCountInString(renderSkillEntry(skill, ""))
	filled := utf8.RuneCountInString(renderSkillEntry(skill, description))
	return filled - empty
}

func renderSkillCatalog(skills []SkillSummary, descriptions []string, omitted int) string {
	var builder strings.Builder
	builder.WriteString(skillPromptInstructions)
	for index, skill := range skills {
		description := ""
		if index < len(descriptions) {
			description = descriptions[index]
		}
		builder.WriteString(renderSkillEntry(skill, description))
	}
	builder.WriteString(skillPromptClosing)
	if omitted > 0 {
		fmt.Fprintf(&builder, "[因技能目录预算限制，已省略 %d 个技能。]\n", omitted)
	}
	return builder.String()
}

func renderSkillEntry(skill SkillSummary, description string) string {
	return fmt.Sprintf(
		"  <skill>\n    <name>%s</name>\n    <description>%s</description>\n    <location>%s</location>\n    <version>%s</version>\n  </skill>\n",
		xmlTextReplacer.Replace(skill.Name),
		xmlTextReplacer.Replace(description),
		xmlTextReplacer.Replace(skill.Location),
		xmlTextReplacer.Replace(skill.Version),
	)
}
