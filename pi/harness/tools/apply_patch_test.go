package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestApplyPatchToolDefinitionIsExclusive(t *testing.T) {
	tool := newApplyPatchToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "apply_patch" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schemaObject, ok := definition.InputSchema.(map[string]any)
	if !ok || schemaObject["additionalProperties"] != false {
		t.Fatalf("InputSchema = %#v", definition.InputSchema)
	}
}

func TestApplyPatchToolAddsUpdatesDeletesAndMovesFiles(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "update.txt"), []byte("before\nold\nafter\n"))
	writeTestFile(t, filepath.Join(workDir, "delete.txt"), []byte("delete me\n"))
	writeTestFile(t, filepath.Join(workDir, "move.txt"), []byte("move old\n"))
	tool := newApplyPatchToolForTest(t, workDir)
	patch := `*** Begin Patch
*** Add File: nested/added.txt
+added one
+added two
*** Update File: update.txt
@@
 before
-old
+new
 after
*** Delete File: delete.txt
*** Update File: move.txt
*** Move to: nested/moved.txt
@@
-move old
+move new
*** End Patch`

	output, err := tool.Execute(context.Background(), applyPatchArguments(t, patch), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(output.Content) != 1 || output.Content[0].Text != "Applied patch: 4 operation(s) across 5 file(s)" {
		t.Fatalf("Execute() content = %#v", output.Content)
	}
	details, ok := output.Details.(ApplyPatchDetails)
	if !ok {
		t.Fatalf("Execute() details = %#v", output.Details)
	}
	wantDetails := ApplyPatchDetails{
		Operations: 4,
		Files:      []string{"delete.txt", "move.txt", "nested/added.txt", "nested/moved.txt", "update.txt"},
	}
	if !reflect.DeepEqual(details, wantDetails) {
		t.Fatalf("Execute() details = %#v, want %#v", details, wantDetails)
	}
	assertFileContent(t, filepath.Join(workDir, "nested", "added.txt"), "added one\nadded two\n")
	assertFileContent(t, filepath.Join(workDir, "update.txt"), "before\nnew\nafter\n")
	assertFileContent(t, filepath.Join(workDir, "nested", "moved.txt"), "move new\n")
	for _, path := range []string{"delete.txt", "move.txt"} {
		if _, err := os.Stat(filepath.Join(workDir, path)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
}

func TestApplyPatchToolPreflightsAllOperationsBeforeMutation(t *testing.T) {
	workDir := t.TempDir()
	first := filepath.Join(workDir, "first.txt")
	second := filepath.Join(workDir, "second.txt")
	writeTestFile(t, first, []byte("first old\n"))
	writeTestFile(t, second, []byte("second old\n"))
	tool := newApplyPatchToolForTest(t, workDir)
	patch := `*** Begin Patch
*** Update File: first.txt
@@
-first old
+first new
*** Update File: second.txt
@@
-missing
+second new
*** End Patch`

	if _, err := tool.execute(context.Background(), applyPatchArguments(t, patch)); err == nil || !strings.Contains(err.Error(), "上下文") {
		t.Fatalf("Execute() error = %v", err)
	}
	assertFileContent(t, first, "first old\n")
	assertFileContent(t, second, "second old\n")
}

func TestApplyPatchToolPreservesMoveSourceWhenDestinationWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission semantics do not provide a stable unwritable-directory fixture")
	}
	workDir := t.TempDir()
	source := filepath.Join(workDir, "source.txt")
	lockedDir := filepath.Join(workDir, "locked")
	writeTestFile(t, source, []byte("source\n"))
	if err := os.Mkdir(lockedDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o700) })
	tool := newApplyPatchToolForTest(t, workDir)
	patch := `*** Begin Patch
*** Update File: source.txt
*** Move to: locked/destination.txt
@@
 source
*** End Patch`

	if _, err := tool.execute(context.Background(), applyPatchArguments(t, patch)); err == nil {
		t.Fatal("Execute() error = nil")
	}
	assertFileContent(t, source, "source\n")
}

func TestApplyPatchToolRejectsPathGraphConflictsBeforeMutation(t *testing.T) {
	tests := []struct {
		name  string
		patch string
	}{
		{
			name: "parent and child files",
			patch: `*** Begin Patch
*** Add File: node
+parent
*** Add File: node/child.txt
+child
*** End Patch`,
		},
		{
			name: "case aliases",
			patch: `*** Begin Patch
*** Add File: Alias.txt
+upper
*** Add File: alias.txt
+lower
*** End Patch`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			tool := newApplyPatchToolForTest(t, workDir)
			if _, err := tool.execute(context.Background(), applyPatchArguments(t, tt.patch)); err == nil || !strings.Contains(err.Error(), "冲突") {
				t.Fatalf("Execute() error = %v", err)
			}
			entries, err := os.ReadDir(workDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("workdir entries = %#v, error = %v", entries, err)
			}
		})
	}
}

func TestApplyPatchToolRejectsAmbiguousUpdateContext(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "duplicate.txt")
	writeTestFile(t, path, []byte("same\nsame\n"))
	tool := newApplyPatchToolForTest(t, workDir)
	patch := `*** Begin Patch
*** Update File: duplicate.txt
@@
-same
+changed
*** End Patch`

	if _, err := tool.execute(context.Background(), applyPatchArguments(t, patch)); err == nil || !strings.Contains(err.Error(), "多处") {
		t.Fatalf("Execute() error = %v", err)
	}
	assertFileContent(t, path, "same\nsame\n")
}

func TestApplyPatchToolRejectsMalformedAndConflictingPatches(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "exists.txt"), []byte("exists\n"))
	tool := newApplyPatchToolForTest(t, workDir)
	tests := []struct {
		name  string
		patch string
		want  string
	}{
		{name: "missing envelope", patch: "*** Add File: a.txt\n+a", want: "Begin Patch"},
		{name: "invalid add line", patch: "*** Begin Patch\n*** Add File: a.txt\nplain\n*** End Patch", want: "+"},
		{name: "add existing", patch: "*** Begin Patch\n*** Add File: exists.txt\n+new\n*** End Patch", want: "已存在"},
		{name: "delete missing", patch: "*** Begin Patch\n*** Delete File: missing.txt\n*** End Patch", want: "不存在"},
		{name: "duplicate destination", patch: "*** Begin Patch\n*** Add File: same.txt\n+one\n*** Add File: same.txt\n+two\n*** End Patch", want: "已存在"},
		{name: "path traversal", patch: "*** Begin Patch\n*** Add File: ../outside.txt\n+bad\n*** End Patch", want: "工作区"},
		{name: "NUL content", patch: "*** Begin Patch\n*** Add File: nul.txt\n+bad\x00content\n*** End Patch", want: "NUL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.execute(context.Background(), applyPatchArguments(t, tt.patch))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestApplyPatchToolRejectsInvalidArguments(t *testing.T) {
	tool := newApplyPatchToolForTest(t, t.TempDir())
	tests := []struct {
		args json.RawMessage
		want string
	}{
		{args: json.RawMessage(`{"input":`), want: "参数解析失败"},
		{args: json.RawMessage(`{"input":"x","extra":true}`), want: "unknown field"},
		{args: json.RawMessage(`{"input":"x"} {}`), want: "多余"},
		{args: json.RawMessage(`{}`), want: "input"},
	}
	for _, tt := range tests {
		_, err := tool.execute(context.Background(), tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
		}
	}
}

func TestApplyPatchToolHonorsCancellationAndWorkspaceClose(t *testing.T) {
	workDir := t.TempDir()
	workspace := newWorkspaceForTest(t, workDir)
	tool := NewApplyPatchTool(workspace)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	patch := "*** Begin Patch\n*** Add File: a.txt\n+a\n*** End Patch"
	if _, err := tool.execute(ctx, applyPatchArguments(t, patch)); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("canceled Execute() error = %v", err)
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), applyPatchArguments(t, patch)); err == nil {
		t.Fatal("Execute() after Workspace.Close error = nil")
	}
}

func TestApplyPatchToolUsesSharedWorkspaceBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink permissions are not stable in tests")
	}
	workDir := t.TempDir()
	externalDir := t.TempDir()
	if err := os.Symlink(externalDir, filepath.Join(workDir, "linked")); err != nil {
		t.Fatal(err)
	}
	tool := NewApplyPatchTool(newWorkspaceForTest(t, workDir))
	patch := "*** Begin Patch\n*** Add File: linked/blocked.txt\n+blocked\n*** End Patch"
	if _, err := tool.execute(context.Background(), applyPatchArguments(t, patch)); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(externalDir, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside target exists: %v", err)
	}
}

func newApplyPatchToolForTest(t *testing.T, workDir string) *ApplyPatchTool {
	t.Helper()
	return NewApplyPatchTool(newWorkspaceForTest(t, workDir))
}

func applyPatchArguments(t *testing.T, input string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("read %s = %q, error = %v, want %q", path, got, err, want)
	}
}
