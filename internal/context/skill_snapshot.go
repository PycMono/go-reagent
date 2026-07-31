package context

type SkillSource string

const (
	SkillSourceWorkspace SkillSource = "skills"
	SkillSourceAgents    SkillSource = ".agents/skills"
	SkillSourceClaw      SkillSource = ".claw/skills"
)

type DiagnosticSeverity string

const (
	DiagnosticSeverityInfo    DiagnosticSeverity = "info"
	DiagnosticSeverityWarning DiagnosticSeverity = "warning"
	DiagnosticSeverityError   DiagnosticSeverity = "error"
)

type SkillSummary struct {
	Name        string
	Description string
	Location    string
	Version     string
	Source      SkillSource
}

type SkillDiagnostic struct {
	Path     string
	Severity DiagnosticSeverity
	Code     string
	Message  string
}

type SkillSnapshot struct {
	skills      []SkillSummary
	diagnostics []SkillDiagnostic
}

// newSkillSnapshot 复制 Skill 摘要和诊断信息，创建不受调用方后续切片修改影响的快照。
func newSkillSnapshot(skills []SkillSummary, diagnostics []SkillDiagnostic) *SkillSnapshot {
	return &SkillSnapshot{
		skills:      append([]SkillSummary(nil), skills...),
		diagnostics: append([]SkillDiagnostic(nil), diagnostics...),
	}
}

// Skills 返回快照中可用 Skill 摘要的副本；接收者为空时返回 nil。
func (s *SkillSnapshot) Skills() []SkillSummary {
	if s == nil {
		return nil
	}
	return append([]SkillSummary(nil), s.skills...)
}

// Diagnostics 返回快照中 Skill 发现诊断的副本；接收者为空时返回 nil。
func (s *SkillSnapshot) Diagnostics() []SkillDiagnostic {
	if s == nil {
		return nil
	}
	return append([]SkillDiagnostic(nil), s.diagnostics...)
}
