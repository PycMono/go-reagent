package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindUniqueTextMatchExact(t *testing.T) {
	match, err := findUniqueTextMatch("before TARGET after", "TARGET")
	if err != nil || match != (textMatch{start: 7, end: 13, level: 1}) {
		t.Fatalf("match = %#v, error = %v", match, err)
	}
}

func TestFindUniqueTextMatchRejectsExactAmbiguity(t *testing.T) {
	_, err := findUniqueTextMatch("same\nsame\n", "same")
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindUniqueTextMatchNormalizesOnlyNewlines(t *testing.T) {
	content := "before\r\nline one\r\nline two\r\nafter"
	match, err := findUniqueTextMatch(content, "line one\nline two")
	if err != nil || content[match.start:match.end] != "line one\r\nline two" || match.level != 2 {
		t.Fatalf("matched = %q, match = %#v, error = %v", content[match.start:match.end], match, err)
	}
}

func TestFindUniqueTextMatchRejectsNormalizedNewlineAmbiguity(t *testing.T) {
	_, err := findUniqueTextMatch("x\r\ny\r\nx\r\ny", "x\ny")
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindUniqueTextMatchTrimsSnippetEdges(t *testing.T) {
	content := "prefix\nvalue := 1\nsuffix"
	match, err := findUniqueTextMatch(content, " \nvalue := 1\n ")
	if err != nil || content[match.start:match.end] != "value := 1" || match.level != 3 {
		t.Fatalf("matched = %q, match = %#v, error = %v", content[match.start:match.end], match, err)
	}
}

func TestFindUniqueTextMatchIgnoresLineIndentation(t *testing.T) {
	content := "func main() {\n\tif true {\n\t\tfmt.Println(\"ok\")\n\t}\n}\n"
	oldText := "if true {\nfmt.Println(\"ok\")\n}"
	match, err := findUniqueTextMatch(content, oldText)
	if err != nil || content[match.start:match.end] != "\tif true {\n\t\tfmt.Println(\"ok\")\n\t}" || match.level != 4 {
		t.Fatalf("matched = %q, match = %#v, error = %v", content[match.start:match.end], match, err)
	}
}

func TestFindUniqueTextMatchRejectsLineAmbiguity(t *testing.T) {
	content := "\tif true {\n\t\trun()\n\t}\n\tif true {\n\t\trun()\n\t}\n"
	_, err := findUniqueTextMatch(content, "if true {\nrun()\n}")
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindUniqueTextMatchReportsNoMatch(t *testing.T) {
	_, err := findUniqueTextMatch("present", "missing")
	if err == nil || !strings.Contains(err.Error(), "read_file") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplacementForMatchUsesMatchedCRLF(t *testing.T) {
	content := "a\r\nold one\r\nold two\r\nz"
	match := textMatch{start: 3, end: 19, level: 2}
	if got := replacementForMatch(content, match, "new one\nnew two"); got != "new one\r\nnew two" {
		t.Fatalf("replacement = %q", got)
	}
}

func TestReplacementForSingleLineUsesFileCRLF(t *testing.T) {
	content := "a\r\nold\r\nz"
	match := textMatch{start: 3, end: 6, level: 1}
	if got := replacementForMatch(content, match, "new one\nnew two"); got != "new one\r\nnew two" {
		t.Fatalf("replacement = %q", got)
	}
}

func TestEditFileToolDefinitionIsExclusive(t *testing.T) {
	tool := newEditFileToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "edit_file" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schemaObject, ok := definition.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema = %T", definition.InputSchema)
	}
	if additional, exists := schemaObject["additionalProperties"]; !exists || additional != false {
		t.Fatalf("additionalProperties = %#v", additional)
	}
	required, ok := schemaObject["required"].([]string)
	if !ok || len(required) != 3 {
		t.Fatalf("required = %#v", schemaObject["required"])
	}
}

func TestEditFileToolRejectsInvalidArguments(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "file.txt"), []byte("old"))
	tool := newEditFileToolForTest(t, workDir)
	absoluteArgs := editFileArguments(t, filepath.Join(workDir, "file.txt"), "old", "new")

	tests := []struct {
		name string
		ctx  context.Context
		args json.RawMessage
		want string
	}{
		{name: "nil context", args: editFileArguments(t, "file.txt", "old", "new"), want: "context"},
		{name: "malformed JSON", ctx: context.Background(), args: json.RawMessage(`{"path":`), want: "参数解析失败"},
		{name: "unknown field", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","old_text":"old","new_text":"new","extra":true}`), want: "unknown field"},
		{name: "trailing JSON", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","old_text":"old","new_text":"new"} {}`), want: "多余"},
		{name: "blank path", ctx: context.Background(), args: editFileArguments(t, " ", "old", "new"), want: "path 不能为空"},
		{name: "absolute path", ctx: context.Background(), args: absoluteArgs, want: "相对路径"},
		{name: "blank old text", ctx: context.Background(), args: editFileArguments(t, "file.txt", " \n ", "new"), want: "old_text 不能为空"},
		{name: "missing new text", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","old_text":"old"}`), want: "new_text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(tt.ctx, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEditFileToolEditsExistingText(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "server.go")
	writeTestFile(t, path, []byte("package main\n\nif true {\n\tfmt.Println(\"open\")\n}\n"))
	tool := newEditFileToolForTest(t, workDir)
	args := editFileArguments(
		t,
		"server.go",
		"if true {\nfmt.Println(\"open\")\n}",
		"if user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}",
	)

	output, err := tool.Execute(context.Background(), args)
	if err != nil || output != "成功修改文件: server.go" {
		t.Fatalf("Execute() output = %q, error = %v", output, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nif user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileToolAllowsDeletion(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "notes.txt")
	writeTestFile(t, path, []byte("keep\nremove me\nkeep too\n"))
	tool := newEditFileToolForTest(t, workDir)

	if _, err := tool.Execute(context.Background(), editFileArguments(t, "notes.txt", "remove me\n", "")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep\nkeep too\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestEditFileToolPreservesCRLFAndPermissions(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "config.txt")
	writeTestFile(t, path, []byte("before\r\nold one\r\nold two\r\nafter\r\n"))
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	tool := newEditFileToolForTest(t, workDir)

	if _, err := tool.Execute(
		context.Background(),
		editFileArguments(t, "config.txt", "old one\nold two", "new one\nnew two"),
	); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before\r\nnew one\r\nnew two\r\nafter\r\n" {
		t.Fatalf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
}

func TestEditFileToolEditsNestedPathAndInternalSymlink(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workDir, "nested", "target.txt")
	writeTestFile(t, target, []byte("old"))
	link := filepath.Join(workDir, "target-link.txt")
	if err := os.Symlink("nested/target.txt", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tool := newEditFileToolForTest(t, workDir)

	if _, err := tool.Execute(context.Background(), editFileArguments(t, "target-link.txt", "old", "new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("target content = %q", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: info = %#v, error = %v", info, err)
	}
}

func TestEditFileToolEnforcesWorkspaceBoundary(t *testing.T) {
	workDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	writeTestFile(t, outsidePath, []byte("outside"))
	tool := newEditFileToolForTest(t, workDir)

	relativeOutside, err := filepath.Rel(workDir, outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), editFileArguments(t, relativeOutside, "outside", "changed")); err == nil || !strings.Contains(err.Error(), "打开文件失败") {
		t.Fatalf("path traversal Execute() error = %v", err)
	}

	link := filepath.Join(workDir, "outside-link.txt")
	if err := os.Symlink(outsidePath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := tool.Execute(context.Background(), editFileArguments(t, "outside-link.txt", "outside", "changed")); err == nil || !strings.Contains(err.Error(), "打开文件失败") {
		t.Fatalf("external symlink Execute() error = %v", err)
	}
	got, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "outside" {
		t.Fatalf("outside content = %q", got)
	}
}

func TestEditFileToolRejectsNonFilesAndInvalidText(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "nul.bin"), []byte{'a', 0, 'b'})
	writeTestFile(t, filepath.Join(workDir, "invalid.bin"), []byte{0xff, 0xfe})
	tool := newEditFileToolForTest(t, workDir)

	tests := []struct {
		path string
		want string
	}{
		{path: "directory", want: "打开文件失败"},
		{path: "missing.txt", want: "打开文件失败"},
		{path: "nul.bin", want: "NUL"},
		{path: "invalid.bin", want: "UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), editFileArguments(t, tt.path, "a", "b")); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute(%q) error = %v, want containing %q", tt.path, err, tt.want)
			}
		})
	}
}

func TestEditFileToolHonorsCancellationAndClose(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "file.txt"), []byte("old"))
	tool, err := NewEditFileTool(workDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, editFileArguments(t, "file.txt", "old", "new")); err == nil || !strings.Contains(err.Error(), "已取消") {
		t.Fatalf("canceled Execute() error = %v", err)
	}
	if err := tool.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := tool.Execute(context.Background(), editFileArguments(t, "file.txt", "old", "new")); err == nil || !strings.Contains(err.Error(), "打开文件失败") {
		t.Fatalf("Execute() after Close error = %v", err)
	}
}

func TestNewEditFileToolRejectsInvalidWorkDir(t *testing.T) {
	if _, err := NewEditFileTool(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("NewEditFileTool() error = nil")
	}
}

func newEditFileToolForTest(t *testing.T, workDir string) *EditFileTool {
	t.Helper()
	tool, err := NewEditFileTool(workDir)
	if err != nil {
		t.Fatalf("NewEditFileTool() error = %v", err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	return tool
}

func editFileArguments(t *testing.T, path, oldText, newText string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{
		"path":     path,
		"old_text": oldText,
		"new_text": newText,
	})
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}
