package skills

import "testing"

// TestSnapshotReturnsCopies 验证快照在构造和读取时都会复制切片，避免外部修改内部状态。
func TestSnapshotReturnsCopies(t *testing.T) {
	originalSkills := []Summary{{
		Name:        "review",
		Description: "Review changes",
		Location:    "skills/review/SKILL.md",
		Version:     "sha256:0123456789abcdef",
		Source:      SourceWorkspace,
	}}
	originalDiagnostics := []Diagnostic{{
		Path:     "skills/review/SKILL.md",
		Severity: SeverityInfo,
		Code:     "sample",
		Message:  "sample diagnostic",
	}}
	snapshot := newSnapshot(originalSkills, originalDiagnostics)

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

// TestNilSnapshotAccessorsReturnNil 验证空快照接收者的访问方法安全返回 nil。
func TestNilSnapshotAccessorsReturnNil(t *testing.T) {
	var snapshot *Snapshot
	if snapshot.Skills() != nil || snapshot.Diagnostics() != nil {
		t.Fatalf("nil snapshot accessors returned values")
	}
}
