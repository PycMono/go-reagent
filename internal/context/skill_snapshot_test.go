package context

import "testing"

func TestSkillSnapshotReturnsCopies(t *testing.T) {
	originalSkills := []SkillSummary{{
		Name:        "review",
		Description: "Review changes",
		Location:    "skills/review/SKILL.md",
		Version:     "sha256:0123456789abcdef",
		Source:      SkillSourceWorkspace,
	}}
	originalDiagnostics := []SkillDiagnostic{{
		Path:     "skills/review/SKILL.md",
		Severity: DiagnosticSeverityInfo,
		Code:     "sample",
		Message:  "sample diagnostic",
	}}
	snapshot := newSkillSnapshot(originalSkills, originalDiagnostics)

	originalSkills[0].Name = "mutated-original"
	originalDiagnostics[0].Code = "mutated-original"
	firstSkills := snapshot.Skills()
	firstDiagnostics := snapshot.Diagnostics()
	firstSkills[0].Name = "mutated-copy"
	firstDiagnostics[0].Code = "mutated-copy"

	if got := snapshot.Skills()[0].Name; got != "review" {
		t.Fatalf("snapshot name = %q", got)
	}
	if got := snapshot.Diagnostics()[0].Code; got != "sample" {
		t.Fatalf("snapshot diagnostic code = %q", got)
	}
}

func TestNilSkillSnapshotAccessorsReturnNil(t *testing.T) {
	var snapshot *SkillSnapshot
	if snapshot.Skills() != nil || snapshot.Diagnostics() != nil {
		t.Fatalf("nil snapshot accessors returned values")
	}
}
