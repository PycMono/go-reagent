package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadFileToolDefinition(t *testing.T) {
	tool := newReadFileToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "read_file" || definition.Description == "" {
		t.Fatalf("definition = %#v", definition)
	}
	if !definition.ParallelSafe {
		t.Fatal("read_file must be marked parallel-safe")
	}
	schemaObject, ok := definition.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema = %T", definition.InputSchema)
	}
	if additional, exists := schemaObject["additionalProperties"]; !exists || additional != false {
		t.Fatalf("additionalProperties = %#v", additional)
	}
}

func TestReadFileToolReadsWorkspaceFiles(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "nested", "message.txt"), []byte("nested content"))
	tool := newReadFileToolForTest(t, workDir)

	tests := []struct {
		path string
		want string
	}{
		{path: "hello.txt", want: "hello"},
		{path: "nested/message.txt", want: "nested content"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			output, err := tool.Execute(context.Background(), readFileArguments(t, tt.path))
			if err != nil || output != tt.want {
				t.Fatalf("Execute() output = %q, error = %v", output, err)
			}
		})
	}
}

func TestReadFileToolRejectsInvalidArguments(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	tool := newReadFileToolForTest(t, workDir)
	absoluteArgs := readFileArguments(t, filepath.Join(workDir, "hello.txt"))

	tests := []struct {
		name string
		args json.RawMessage
		want string
	}{
		{name: "malformed JSON", args: json.RawMessage(`{"path":`), want: "参数解析失败"},
		{name: "unknown field", args: json.RawMessage(`{"path":"hello.txt","extra":true}`), want: "unknown field"},
		{name: "trailing JSON", args: json.RawMessage(`{"path":"hello.txt"} {}`), want: "多余"},
		{name: "blank path", args: json.RawMessage(`{"path":" "}`), want: "path 不能为空"},
		{name: "absolute path", args: absoluteArgs, want: "相对路径"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestReadFileToolEnforcesWorkspaceBoundary(t *testing.T) {
	workDir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "inside.txt"), []byte("inside"))
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	writeTestFile(t, outsidePath, []byte("outside"))
	tool := newReadFileToolForTest(t, workDir)

	relativeOutside, err := filepath.Rel(workDir, outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), readFileArguments(t, relativeOutside)); err == nil {
		t.Fatal("path traversal Execute() error = nil")
	}

	t.Run("internal symlink", func(t *testing.T) {
		link := filepath.Join(workDir, "inside-link.txt")
		if err := os.Symlink("inside.txt", link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		output, err := tool.Execute(context.Background(), readFileArguments(t, "inside-link.txt"))
		if err != nil || output != "inside" {
			t.Fatalf("Execute() output = %q, error = %v", output, err)
		}
	})

	t.Run("external symlink", func(t *testing.T) {
		link := filepath.Join(workDir, "outside-link.txt")
		if err := os.Symlink(outsidePath, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := tool.Execute(context.Background(), readFileArguments(t, "outside-link.txt")); err == nil {
			t.Fatal("external symlink Execute() error = nil")
		}
	})
}

func TestReadFileToolRejectsNonFilesAndMissingPaths(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool := newReadFileToolForTest(t, workDir)

	for _, path := range []string{"directory", "missing.txt"} {
		t.Run(path, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), readFileArguments(t, path)); err == nil {
				t.Fatalf("Execute(%q) error = nil", path)
			}
		})
	}
}

func TestReadFileToolTruncatesAtUTF8Boundary(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "exact.txt"), []byte(strings.Repeat("a", 8000)))
	writeTestFile(t, filepath.Join(workDir, "large.txt"), []byte(strings.Repeat("b", 8001)))
	writeTestFile(t, filepath.Join(workDir, "unicode.txt"), []byte(strings.Repeat("c", 7999)+"你tail"))
	tool := newReadFileToolForTest(t, workDir)

	exact, err := tool.Execute(context.Background(), readFileArguments(t, "exact.txt"))
	if err != nil || len(exact) != 8000 || strings.Contains(exact, "已截断") {
		t.Fatalf("exact output length = %d, error = %v", len(exact), err)
	}

	for _, path := range []string{"large.txt", "unicode.txt"} {
		t.Run(path, func(t *testing.T) {
			output, err := tool.Execute(context.Background(), readFileArguments(t, path))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !utf8.ValidString(output) || !strings.Contains(output, "已截断至前 8000 字节") {
				t.Fatalf("truncated output is invalid: %q", output[len(output)-80:])
			}
		})
	}
}

func TestReadFileToolRejectsBinaryAndInvalidUTF8(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "nul.bin"), []byte{'a', 0, 'b'})
	writeTestFile(t, filepath.Join(workDir, "invalid.bin"), []byte{0xff, 0xfe})
	lateInvalid := append([]byte(strings.Repeat("a", 8001)), 0xff)
	writeTestFile(t, filepath.Join(workDir, "late-invalid.bin"), lateInvalid)
	tool := newReadFileToolForTest(t, workDir)

	for _, path := range []string{"nul.bin", "invalid.bin", "late-invalid.bin"} {
		t.Run(path, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), readFileArguments(t, path)); err == nil {
				t.Fatalf("Execute(%q) error = nil", path)
			}
		})
	}
}

func TestReadFileToolHonorsCancellationAndClose(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	tool, err := NewReadFileTool(workDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, readFileArguments(t, "hello.txt")); err == nil {
		t.Fatal("canceled Execute() error = nil")
	}
	if err := tool.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := tool.Execute(context.Background(), readFileArguments(t, "hello.txt")); err == nil {
		t.Fatal("Execute() after Close error = nil")
	}
}

func TestNewReadFileToolRejectsInvalidWorkDir(t *testing.T) {
	if _, err := NewReadFileTool(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("NewReadFileTool() error = nil")
	}
}

func newReadFileToolForTest(t *testing.T, workDir string) *ReadFileTool {
	t.Helper()
	tool, err := NewReadFileTool(workDir)
	if err != nil {
		t.Fatalf("NewReadFileTool() error = %v", err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	return tool
}

func readFileArguments(t *testing.T, path string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
