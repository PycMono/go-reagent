package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteToolDefinitionIsExclusive(t *testing.T) {
	tool := newWriteToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "write" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schemaObject, ok := definition.InputSchema.(map[string]any)
	if !ok || schemaObject["additionalProperties"] != false {
		t.Fatalf("InputSchema = %#v", definition.InputSchema)
	}
}

func TestWriteToolCreatesParentsAndOverwritesText(t *testing.T) {
	workDir := t.TempDir()
	tool := newWriteToolForTest(t, workDir)

	if _, err := tool.execute(context.Background(), writeFileArguments(t, "nested/ping.go", "package main\n")); err != nil {
		t.Fatalf("create Execute() error = %v", err)
	}
	path := filepath.Join(workDir, "nested", "ping.go")
	if got, err := os.ReadFile(path); err != nil || string(got) != "package main\n" {
		t.Fatalf("created content = %q, error = %v", got, err)
	}
	if _, err := tool.execute(context.Background(), writeFileArguments(t, "nested/ping.go", "package ping\n")); err != nil {
		t.Fatalf("overwrite Execute() error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "package ping\n" {
		t.Fatalf("overwritten content = %q, error = %v", got, err)
	}
}

func TestWriteToolReturnsDetailsAndPreservesExistingMode(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "mode.txt")
	writeTestFile(t, path, []byte("before"))
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	tool := newWriteToolForTest(t, workDir)

	output, err := tool.Execute(context.Background(), writeFileArguments(t, "mode.txt", "after"), nil)
	if err != nil {
		t.Fatal(err)
	}
	details, ok := output.Details.(WriteDetails)
	if !ok || details != (WriteDetails{Path: "mode.txt", Bytes: len("after"), Changed: true}) {
		t.Fatalf("Details = %#v", output.Details)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, error = %v", info.Mode(), err)
	}

	output, err = tool.Execute(context.Background(), writeFileArguments(t, "mode.txt", "after"), nil)
	if err != nil {
		t.Fatal(err)
	}
	details, ok = output.Details.(WriteDetails)
	if !ok || details != (WriteDetails{Path: "mode.txt", Bytes: len("after"), Changed: false}) {
		t.Fatalf("Details = %#v", output.Details)
	}
}

func TestWriteToolIdenticalContentIsNoOp(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "same.txt")
	writeTestFile(t, path, []byte("same"))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tool := newWriteToolForTest(t, workDir)

	output, err := tool.execute(context.Background(), writeFileArguments(t, "same.txt", "same"))
	if err != nil || !strings.Contains(output, "未变化") {
		t.Fatalf("Execute() output = %q, error = %v", output, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("mtime changed: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestWriteToolRejectsInvalidArgumentsAndText(t *testing.T) {
	tool := newWriteToolForTest(t, t.TempDir())
	tests := []struct {
		name string
		args json.RawMessage
		want string
	}{
		{name: "malformed JSON", args: json.RawMessage(`{"path":`), want: "参数解析失败"},
		{name: "unknown field", args: json.RawMessage(`{"path":"a","content":"b","extra":true}`), want: "unknown field"},
		{name: "trailing JSON", args: json.RawMessage(`{"path":"a","content":"b"} {}`), want: "多余"},
		{name: "blank path", args: writeFileArguments(t, " ", "content"), want: "path 不能为空"},
		{name: "missing content", args: json.RawMessage(`{"path":"a"}`), want: "content"},
		{name: "NUL content", args: writeFileArguments(t, "a", "a\x00b"), want: "NUL"},
		{name: "invalid UTF-8", args: json.RawMessage([]byte{'{', '"', 'p', 'a', 't', 'h', '"', ':', '"', 'a', '"', ',', '"', 'c', 'o', 'n', 't', 'e', 'n', 't', '"', ':', '"', 0xff, '"', '}'}), want: "UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.execute(context.Background(), tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestWriteToolEnforcesWorkspaceBoundaryAndRegularFiles(t *testing.T) {
	workDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	writeTestFile(t, outsidePath, []byte("outside"))
	tool := newWriteToolForTest(t, workDir)

	relativeOutside, err := filepath.Rel(workDir, outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), writeFileArguments(t, relativeOutside, "changed")); err == nil {
		t.Fatal("path traversal Execute() error = nil")
	}
	if err := os.Mkdir(filepath.Join(workDir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), writeFileArguments(t, "directory", "changed")); err == nil {
		t.Fatal("directory Execute() error = nil")
	}
	if err := os.Symlink(outsidePath, filepath.Join(workDir, "outside-link")); err == nil {
		if _, err := tool.execute(context.Background(), writeFileArguments(t, "outside-link", "changed")); err == nil {
			t.Fatal("external symlink Execute() error = nil")
		}
	}
	got, err := os.ReadFile(outsidePath)
	if err != nil || string(got) != "outside" {
		t.Fatalf("outside content = %q, error = %v", got, err)
	}
}

func TestWriteToolRejectsExistingFIFOWithoutBlocking(t *testing.T) {
	workDir := t.TempDir()
	makeFIFO(t, filepath.Join(workDir, "stream"))
	tool := newWriteToolForTest(t, workDir)

	err := mustReturnBefore(t, func() error {
		_, err := tool.execute(context.Background(), writeFileArguments(t, "stream", "content"))
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "普通文件") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestWriteToolHonorsCancellationAndWorkspaceClose(t *testing.T) {
	workDir := t.TempDir()
	workspace := newWorkspaceForTest(t, workDir)
	tool := NewWriteTool(workspace)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.execute(ctx, writeFileArguments(t, "a.txt", "a")); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("canceled Execute() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("canceled write created file: %v", err)
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), writeFileArguments(t, "a.txt", "a")); err == nil {
		t.Fatal("Execute() after Close error = nil")
	}
}

func newWriteToolForTest(t *testing.T, workDir string) *WriteTool {
	t.Helper()
	return NewWriteTool(newWorkspaceForTest(t, workDir))
}

func writeFileArguments(t *testing.T, path, content string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}
