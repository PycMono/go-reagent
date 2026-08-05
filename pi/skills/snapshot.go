package skills

type Source string

const (
	SourceWorkspace Source = "skills"
	SourceAgents    Source = ".agents/skills"
	SourceClaw      Source = ".claw/skills"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Summary struct {
	Name        string
	Description string
	Location    string
	Version     string
	Source      Source
}

type Diagnostic struct {
	Path     string
	Severity Severity
	Code     string
	Message  string
}

type Snapshot struct {
	skills      []Summary
	diagnostics []Diagnostic
}

// newSnapshot 复制 Skill 摘要和诊断信息，创建不受调用方后续切片修改影响的快照。
func newSnapshot(skills []Summary, diagnostics []Diagnostic) *Snapshot {
	return &Snapshot{
		skills:      append([]Summary(nil), skills...),
		diagnostics: append([]Diagnostic(nil), diagnostics...),
	}
}

// Skills 返回快照中可用 Skill 摘要的副本；接收者为空时返回 nil。
func (s *Snapshot) Skills() []Summary {
	if s == nil {
		return nil
	}
	return append([]Summary(nil), s.skills...)
}

// Diagnostics 返回快照中 Skill 发现诊断的副本；接收者为空时返回 nil。
func (s *Snapshot) Diagnostics() []Diagnostic {
	if s == nil {
		return nil
	}
	return append([]Diagnostic(nil), s.diagnostics...)
}
