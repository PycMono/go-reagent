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

func newSkillSnapshot(skills []SkillSummary, diagnostics []SkillDiagnostic) *SkillSnapshot {
	return &SkillSnapshot{
		skills:      append([]SkillSummary(nil), skills...),
		diagnostics: append([]SkillDiagnostic(nil), diagnostics...),
	}
}

func (s *SkillSnapshot) Skills() []SkillSummary {
	if s == nil {
		return nil
	}
	return append([]SkillSummary(nil), s.skills...)
}

func (s *SkillSnapshot) Diagnostics() []SkillDiagnostic {
	if s == nil {
		return nil
	}
	return append([]SkillDiagnostic(nil), s.diagnostics...)
}
