package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestReadToolDefinitionDescribesPagination(t *testing.T) {
	tool := newReadToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "read" || definition.Description == "" || !definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schemaObject, ok := definition.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema = %T", definition.InputSchema)
	}
	properties, ok := schemaObject["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schemaObject["properties"])
	}
	for _, field := range []string{"offset", "limit"} {
		property, ok := properties[field].(map[string]any)
		if !ok || property["type"] != "integer" || property["minimum"] != 1 {
			t.Fatalf("%s schema = %#v", field, properties[field])
		}
	}
	if limit := properties["limit"].(map[string]any); limit["maximum"] != 2000 {
		t.Fatalf("limit schema = %#v", limit)
	}
	if schemaObject["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v", schemaObject["additionalProperties"])
	}
}

func TestReadToolReturnsStructuredPageDetails(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "lines.txt"), []byte("one\ntwo\nthree\n"))
	tool := newReadToolForTest(t, workDir)
	limit := 2

	output, err := tool.Execute(context.Background(), readFilePageArguments(t, "lines.txt", nil, &limit), nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err := schema.TextContent(output.Content)
	if err != nil {
		t.Fatal(err)
	}
	if content != "one\ntwo\n\n[Showing lines 1-2. Use offset=3 to continue.]" {
		t.Fatalf("content = %q", content)
	}
	details, ok := output.Details.(ReadDetails)
	if !ok {
		t.Fatalf("Details = %#v", output.Details)
	}
	if want := (ReadDetails{Path: "lines.txt", Lines: 2, Bytes: len("one\ntwo\n"), Truncated: true, NextOffset: 3}); details != want {
		t.Fatalf("Details = %#v, want %#v", details, want)
	}
}

func TestReadToolReadsWorkspaceFiles(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "nested", "message.txt"), []byte("nested content\n"))
	tool := newReadToolForTest(t, workDir)

	for _, tt := range []struct{ path, want string }{
		{path: "hello.txt", want: "hello"},
		{path: "nested/message.txt", want: "nested content\n"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			output, err := tool.execute(context.Background(), readFileArguments(t, tt.path))
			if err != nil || output != tt.want {
				t.Fatalf("Execute() output = %q, error = %v", output, err)
			}
		})
	}
}

func TestReadToolPaginatesByLine(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "lines.txt"), []byte("one\ntwo\nthree\nfour\n"))
	tool := newReadToolForTest(t, workDir)
	offset, limit := 2, 2

	got, err := tool.execute(context.Background(), readFilePageArguments(t, "lines.txt", &offset, &limit))
	if err != nil {
		t.Fatal(err)
	}
	want := "two\nthree\n\n[Showing lines 2-3. Use offset=4 to continue.]"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}

	offset = 4
	got, err = tool.execute(context.Background(), readFilePageArguments(t, "lines.txt", &offset, &limit))
	if err != nil || got != "four\n" {
		t.Fatalf("final page = %q, error = %v", got, err)
	}
}

func TestReadToolDefaultsTo2000Lines(t *testing.T) {
	workDir := t.TempDir()
	var content strings.Builder
	for line := 1; line <= 2001; line++ {
		fmt.Fprintf(&content, "line-%04d\n", line)
	}
	writeTestFile(t, filepath.Join(workDir, "many-lines.txt"), []byte(content.String()))
	tool := newReadToolForTest(t, workDir)

	first, err := tool.execute(context.Background(), readFileArguments(t, "many-lines.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "line-2000\n") || strings.Contains(first, "line-2001\n") ||
		!strings.HasSuffix(first, "[Showing lines 1-2000. Use offset=2001 to continue.]") {
		t.Fatalf("first page ending = %q", first[len(first)-120:])
	}
	offset := 2001
	last, err := tool.execute(context.Background(), readFilePageArguments(t, "many-lines.txt", &offset, nil))
	if err != nil || last != "line-2001\n" {
		t.Fatalf("last page = %q, error = %v", last, err)
	}
}

func TestReadToolConsecutivePagesReconstructOriginalContent(t *testing.T) {
	workDir := t.TempDir()
	var original strings.Builder
	for line := 1; line <= 1800; line++ {
		fmt.Fprintf(&original, "第%04d行-%s", line, strings.Repeat("内容", 12))
		if line < 1800 {
			original.WriteByte('\n')
		}
	}
	writeTestFile(t, filepath.Join(workDir, "reconstruct.txt"), []byte(original.String()))
	tool := newReadToolForTest(t, workDir)

	var rebuilt strings.Builder
	nextOffset := 1
	pageCount := 0
	for {
		pageCount++
		page, err := tool.execute(context.Background(), readFilePageArguments(t, "reconstruct.txt", &nextOffset, nil))
		if err != nil {
			t.Fatalf("page %d error = %v", pageCount, err)
		}
		markerStart := strings.LastIndex(page, "[Showing lines ")
		if markerStart < 0 {
			rebuilt.WriteString(page)
			break
		}
		pageWithSeparator := page[:markerStart]
		if !strings.HasSuffix(pageWithSeparator, "\n") {
			t.Fatalf("page %d has no marker separator", pageCount)
		}
		rebuilt.WriteString(strings.TrimSuffix(pageWithSeparator, "\n"))

		var start, end, next int
		if _, err := fmt.Sscanf(page[markerStart:], "[Showing lines %d-%d. Use offset=%d to continue.]", &start, &end, &next); err != nil {
			t.Fatalf("parse page %d marker: %v", pageCount, err)
		}
		if start != nextOffset || next != end+1 {
			t.Fatalf("page %d marker = start %d end %d next %d", pageCount, start, end, next)
		}
		nextOffset = next
		if pageCount > 20 {
			t.Fatal("pagination did not terminate")
		}
	}

	if rebuilt.String() != original.String() {
		t.Fatalf("rebuilt content differs: got %d bytes, want %d", rebuilt.Len(), original.Len())
	}
	if pageCount < 2 {
		t.Fatalf("page count = %d, want multiple byte-limited pages", pageCount)
	}
}

func TestReadToolHandlesEmptyAndPastEOF(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "empty.txt"), nil)
	writeTestFile(t, filepath.Join(workDir, "one.txt"), []byte("one\n"))
	tool := newReadToolForTest(t, workDir)

	for _, tt := range []struct {
		path   string
		offset *int
	}{
		{path: "empty.txt"},
		{path: "one.txt", offset: intPointer(2)},
		{path: "one.txt", offset: intPointer(20)},
	} {
		output, err := tool.execute(context.Background(), readFilePageArguments(t, tt.path, tt.offset, nil))
		if err != nil || output != "" {
			t.Fatalf("Execute(%q) = %q, %v", tt.path, output, err)
		}
	}
}

func TestReadToolRejectsInvalidArguments(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	tool := newReadToolForTest(t, workDir)
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
		{name: "zero offset", args: json.RawMessage(`{"path":"hello.txt","offset":0}`), want: "offset"},
		{name: "negative offset", args: json.RawMessage(`{"path":"hello.txt","offset":-1}`), want: "offset"},
		{name: "zero limit", args: json.RawMessage(`{"path":"hello.txt","limit":0}`), want: "limit"},
		{name: "excessive limit", args: json.RawMessage(`{"path":"hello.txt","limit":2001}`), want: "limit"},
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

func TestReadToolEnforcesWorkspaceBoundary(t *testing.T) {
	workDir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "inside.txt"), []byte("inside"))
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	writeTestFile(t, outsidePath, []byte("outside"))
	tool := newReadToolForTest(t, workDir)
	relativeOutside, err := filepath.Rel(workDir, outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), readFileArguments(t, relativeOutside)); err == nil {
		t.Fatal("path traversal Execute() error = nil")
	}

	t.Run("internal symlink", func(t *testing.T) {
		if err := os.Symlink("inside.txt", filepath.Join(workDir, "inside-link.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		output, err := tool.execute(context.Background(), readFileArguments(t, "inside-link.txt"))
		if err != nil || output != "inside" {
			t.Fatalf("Execute() output = %q, error = %v", output, err)
		}
	})

	t.Run("external symlink", func(t *testing.T) {
		if err := os.Symlink(outsidePath, filepath.Join(workDir, "outside-link.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := tool.execute(context.Background(), readFileArguments(t, "outside-link.txt")); err == nil {
			t.Fatal("external symlink Execute() error = nil")
		}
	})
}

func TestReadToolRejectsNonFilesAndMissingPaths(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool := newReadToolForTest(t, workDir)
	for _, path := range []string{"directory", "missing.txt"} {
		if _, err := tool.execute(context.Background(), readFileArguments(t, path)); err == nil {
			t.Fatalf("Execute(%q) error = nil", path)
		}
	}
}

func TestReadToolLimitsFinalOutputTo50KiB(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "large.txt"), []byte(strings.Repeat("中文内容\n", 7000)))
	tool := newReadToolForTest(t, workDir)

	got, err := tool.execute(context.Background(), readFileArguments(t, "large.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(got)) > defaultReadFileMaxBytes || !utf8.ValidString(got) {
		t.Fatalf("output bytes = %d, valid = %v", len([]byte(got)), utf8.ValidString(got))
	}
	if !strings.Contains(got, "Use offset=") {
		t.Fatalf("missing continuation marker: %q", got[len(got)-120:])
	}
}

func TestReadToolRejectsRequestedLineTooLargeForPage(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "long-line.txt"), []byte(strings.Repeat("a", defaultReadFileMaxBytes)+"\nnext\n"))
	tool := newReadToolForTest(t, workDir)

	_, err := tool.execute(context.Background(), readFileArguments(t, "long-line.txt"))
	if err == nil || !strings.Contains(err.Error(), "单行超过 read 单页限制") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestReadToolSkipsLargeLinesWithoutReturningThem(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "skip.txt"), []byte(strings.Repeat("x", 100*1024)+"\ntarget\n"))
	tool := newReadToolForTest(t, workDir)
	offset := 2

	got, err := tool.execute(context.Background(), readFilePageArguments(t, "skip.txt", &offset, nil))
	if err != nil || got != "target\n" {
		t.Fatalf("Execute() = %q, %v", got, err)
	}
}

func TestReadToolValidatesOnlyRequestedPage(t *testing.T) {
	workDir := t.TempDir()
	content := append([]byte("valid\n"), 0xff, '\n')
	writeTestFile(t, filepath.Join(workDir, "invalid-later.txt"), content)
	writeTestFile(t, filepath.Join(workDir, "nul.txt"), []byte{'a', 0, 'b', '\n'})
	tool := newReadToolForTest(t, workDir)
	limit := 1

	first, err := tool.execute(context.Background(), readFilePageArguments(t, "invalid-later.txt", nil, &limit))
	if err != nil || !strings.Contains(first, "Use offset=2") {
		t.Fatalf("first page = %q, error = %v", first, err)
	}
	offset := 2
	if _, err := tool.execute(context.Background(), readFilePageArguments(t, "invalid-later.txt", &offset, nil)); err == nil ||
		!strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
	if _, err := tool.execute(context.Background(), readFileArguments(t, "nul.txt")); err == nil ||
		!strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL error = %v", err)
	}
}

func TestReadToolDefersValidationForLineMovedToNextPage(t *testing.T) {
	workDir := t.TempDir()
	firstLine := []byte(strings.Repeat("a", defaultReadFileMaxBytes-100) + "\n")
	secondLine := append([]byte(strings.Repeat("b", 80)), 0xff, '\n')
	thirdLine := []byte(strings.Repeat("c", 100) + "\n")
	content := append(append(append([]byte(nil), firstLine...), secondLine...), thirdLine...)
	writeTestFile(t, filepath.Join(workDir, "marker-budget.txt"), content)
	tool := newReadToolForTest(t, workDir)

	first, err := tool.execute(context.Background(), readFileArguments(t, "marker-budget.txt"))
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	if !strings.HasSuffix(first, "[Showing lines 1-1. Use offset=2 to continue.]") ||
		!strings.HasPrefix(first, string(firstLine)) {
		t.Fatalf("first page boundaries are wrong: bytes=%d", len(first))
	}
	offset := 2
	if _, err := tool.execute(context.Background(), readFilePageArguments(t, "marker-budget.txt", &offset, nil)); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("second page error = %v", err)
	}
}

func TestReadToolHonorsCancellationAndWorkspaceClose(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "hello.txt"), []byte("hello"))
	workspace := newWorkspaceForTest(t, workDir)
	tool, err := NewReadTool(workspace)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.execute(ctx, readFileArguments(t, "hello.txt")); err == nil {
		t.Fatal("canceled Execute() error = nil")
	}
	if err := workspace.Close(); err != nil {
		t.Fatalf("Workspace.Close() error = %v", err)
	}
	if _, err := tool.execute(context.Background(), readFileArguments(t, "hello.txt")); err == nil {
		t.Fatal("Execute() after Close error = nil")
	}
}

func TestNewReadToolRejectsNilWorkspace(t *testing.T) {
	if _, err := NewReadTool(nil); err == nil {
		t.Fatal("NewReadTool(nil) error = nil")
	}
}

func newReadToolForTest(t *testing.T, workDir string) *ReadTool {
	t.Helper()
	tool, err := NewReadTool(newWorkspaceForTest(t, workDir))
	if err != nil {
		t.Fatalf("NewReadTool() error = %v", err)
	}
	return tool
}

func readFileArguments(t *testing.T, path string) json.RawMessage {
	t.Helper()
	return readFilePageArguments(t, path, nil, nil)
}

func readFilePageArguments(t *testing.T, path string, offset, limit *int) json.RawMessage {
	t.Helper()
	input := map[string]any{"path": path}
	if offset != nil {
		input["offset"] = *offset
	}
	if limit != nil {
		input["limit"] = *limit
	}
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}

func intPointer(value int) *int {
	return &value
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
