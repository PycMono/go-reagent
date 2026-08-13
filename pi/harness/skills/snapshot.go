package skills

import "sort"

// Source 表示 Skill 的发现来源。
type Source string

const (
	// SourceWorkspace 表示工作区根目录下的 skills 来源。
	SourceWorkspace Source = "skills"
	// SourceAgents 表示工作区根目录下的 .agents/skills 来源。
	SourceAgents Source = ".agents/skills"
	// SourceClaw 表示工作区根目录下的 .claw/skills 来源。
	SourceClaw Source = ".claw/skills"
)

// Severity 表示 Skill 诊断信息的严重程度。
type Severity string

const (
	// SeverityInfo 表示仅供说明、不影响其他 Skill 的诊断信息。
	SeverityInfo Severity = "info"
	// SeverityWarning 表示当前 Skill 被跳过或覆盖的诊断信息。
	SeverityWarning Severity = "warning"
)

// Summary 保存一个可用 Skill 的目录摘要。
type Summary struct {
	// Name 是 Skill 的唯一名称。
	Name string
	// Description 说明 Skill 的用途。
	Description string
	// Location 是相对于工作区的 SKILL.md 路径。
	Location string
	// Version 是根据 SKILL.md 内容生成的版本标识。
	Version string
	// Source 表示 Skill 来自哪个约定目录。
	Source Source
}

// Diagnostic 描述发现过程中跳过或覆盖某个 Skill 的原因。
type Diagnostic struct {
	// Path 是相关 SKILL.md 的工作区相对路径。
	Path string
	// Severity 是诊断信息的严重程度。
	Severity Severity
	// Code 是稳定的机器可读诊断代码。
	Code string
	// Message 是适合展示的诊断说明。
	Message string
}

// Snapshot 保存一次发现得到的有序 Skill 摘要和诊断信息。
type Snapshot struct {
	skills      []Summary
	diagnostics []Diagnostic
}

// Skills 返回快照中可用 Skill 摘要的副本。
func (s *Snapshot) Skills() []Summary {
	return append([]Summary(nil), s.skills...)
}

// Diagnostics 返回快照中 Skill 发现诊断的副本。
func (s *Snapshot) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), s.diagnostics...)
}

// newSnapshot 复制 Skill 摘要和诊断信息，创建不受调用方后续切片修改影响的快照。
func newSnapshot(skills []Summary, diagnostics []Diagnostic) *Snapshot {
	items := append([]Summary(nil), skills...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Location < items[j].Location
		}
		return items[i].Name < items[j].Name
	})
	return &Snapshot{
		skills:      items,
		diagnostics: append([]Diagnostic(nil), diagnostics...),
	}
}

// Empty 表示快照中是否没有可用 Skill。
func (s *Snapshot) Empty() bool {
	return len(s.skills) == 0
}
