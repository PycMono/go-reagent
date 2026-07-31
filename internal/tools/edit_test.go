package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	if err == nil || !strings.Contains(err.Error(), "read") {
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

func TestEditToolDefinitionUsesFinalBatchedProtocol(t *testing.T) {
	tool := newEditToolForTest(t, t.TempDir())
	definition := tool.Definition()
	if definition.Name != "edit" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	root := requireSchemaObject(t, definition.InputSchema)
	if root["additionalProperties"] != false {
		t.Fatalf("root additionalProperties = %#v", root["additionalProperties"])
	}
	if got := root["required"]; !reflect.DeepEqual(got, []string{"path", "edits"}) {
		t.Fatalf("root required = %#v", got)
	}
	properties := requireSchemaObject(t, root["properties"])
	edits := requireSchemaObject(t, properties["edits"])
	if edits["type"] != "array" || edits["minItems"] != 1 {
		t.Fatalf("edits schema = %#v", edits)
	}
	item := requireSchemaObject(t, edits["items"])
	if item["additionalProperties"] != false || !reflect.DeepEqual(item["required"], []string{"oldText", "newText"}) {
		t.Fatalf("edit item schema = %#v", item)
	}
	itemProperties := requireSchemaObject(t, item["properties"])
	if _, ok := itemProperties["oldText"]; !ok {
		t.Fatalf("oldText schema missing: %#v", itemProperties)
	}
	if _, ok := itemProperties["newText"]; !ok {
		t.Fatalf("newText schema missing: %#v", itemProperties)
	}
}

func TestEditToolReturnsStructuredDetailsAndReplayablePatch(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "sample.txt")
	original := "one\ntwo\nthree\n"
	want := "one\nTWO\nthree\n"
	writeTestFile(t, path, []byte(original))
	tool := newEditToolForTest(t, workDir)

	output, err := tool.Execute(context.Background(), editArguments(t, "sample.txt", editOperation("two", "TWO")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Content) != 1 || output.Content[0].Text != "Applied 1 edits to sample.txt" {
		t.Fatalf("content = %#v", output.Content)
	}
	detailsValue := reflect.ValueOf(output.Details)
	if !detailsValue.IsValid() || detailsValue.Type().Name() != "EditDetails" {
		t.Fatalf("details type = %T", output.Details)
	}
	detailsJSON, err := json.Marshal(output.Details)
	if err != nil {
		t.Fatal(err)
	}
	var details struct {
		Diff             string `json:"diff"`
		Patch            string `json:"patch"`
		Replacements     int    `json:"replacements"`
		FirstChangedLine int    `json:"firstChangedLine"`
	}
	if err := json.Unmarshal(detailsJSON, &details); err != nil {
		t.Fatal(err)
	}
	if details.Replacements != 1 || details.FirstChangedLine != 2 {
		t.Fatalf("details = %#v", details)
	}
	if !strings.HasPrefix(details.Diff, "--- a/sample.txt\n+++ b/sample.txt\n@@ ") ||
		!strings.Contains(details.Diff, "-two\n") || !strings.Contains(details.Diff, "+TWO\n") {
		t.Fatalf("diff = %q", details.Diff)
	}
	operations, err := parseStructuredPatch(details.Patch)
	if err != nil || len(operations) != 1 || operations[0].kind != patchUpdate || operations[0].path != "sample.txt" {
		t.Fatalf("parsed patch = %#v, error = %v", operations, err)
	}
	replayed, err := applyPatchHunks("sample.txt", original, operations[0].hunks)
	if err != nil || replayed != want {
		t.Fatalf("replayed = %q, error = %v", replayed, err)
	}
}

func TestEditToolPlansNonOverlappingEditsAgainstOriginalContent(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "words.txt")
	writeTestFile(t, path, []byte("alpha beta gamma\n"))
	tool := newEditToolForTest(t, workDir)

	output, err := tool.Execute(context.Background(), editArguments(t, "words.txt",
		editOperation("alpha", "ALPHABET"),
		editOperation("gamma", "G"),
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	if output.Content[0].Text != "Applied 2 edits to words.txt" {
		t.Fatalf("output = %#v", output)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "ALPHABET beta G\n" {
		t.Fatalf("content = %q, error = %v", got, err)
	}
}

func TestEditToolDoesNotMatchLaterEditsAgainstReplacementText(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "words.txt")
	original := []byte("alpha beta\n")
	writeTestFile(t, path, original)
	setFileModTime(t, path)
	before := statFile(t, path)
	tool := newEditToolForTest(t, workDir)

	_, err := tool.Execute(context.Background(), editArguments(t, "words.txt",
		editOperation("alpha", "gamma"),
		editOperation("gamma", "delta"),
	), nil)
	if err == nil || !strings.Contains(err.Error(), "edits[1]") {
		t.Fatalf("error = %v", err)
	}
	assertFileUnchanged(t, path, original, before.ModTime())
}

func TestEditToolRejectsNonUniqueOldTextBeforeWriting(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "duplicate.txt")
	original := []byte("same and same\n")
	writeTestFile(t, path, original)
	setFileModTime(t, path)
	before := statFile(t, path)
	tool := newEditToolForTest(t, workDir)

	_, err := tool.Execute(context.Background(), editArguments(t, "duplicate.txt", editOperation("same", "changed")), nil)
	if err == nil || !strings.Contains(err.Error(), "edits[0]") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v", err)
	}
	assertFileUnchanged(t, path, original, before.ModTime())
}

func TestEditToolRejectsOverlappingAndNestedRangesBeforeWriting(t *testing.T) {
	tests := []struct {
		name  string
		edits []map[string]any
	}{
		{name: "overlapping", edits: []map[string]any{editOperation("bcd", "one"), editOperation("def", "two")}},
		{name: "nested", edits: []map[string]any{editOperation("bcde", "one"), editOperation("cd", "two")}},
		{name: "duplicate range", edits: []map[string]any{editOperation("bcd", "one"), editOperation("bcd", "two")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			path := filepath.Join(workDir, "letters.txt")
			original := []byte("abcdef\n")
			writeTestFile(t, path, original)
			setFileModTime(t, path)
			before := statFile(t, path)
			tool := newEditToolForTest(t, workDir)

			_, err := tool.Execute(context.Background(), editArguments(t, "letters.txt", tt.edits...), nil)
			if err == nil || !strings.Contains(err.Error(), "重叠或嵌套") {
				t.Fatalf("error = %v", err)
			}
			assertFileUnchanged(t, path, original, before.ModTime())
		})
	}
}

func TestEditToolRejectsInvalidFinalArguments(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, filepath.Join(workDir, "file.txt"), []byte("old"))
	tool := newEditToolForTest(t, workDir)
	absolutePath := filepath.Join(workDir, "file.txt")

	tests := []struct {
		name string
		ctx  context.Context
		args json.RawMessage
		want string
	}{
		{name: "nil context", args: editArguments(t, "file.txt", editOperation("old", "new")), want: "context"},
		{name: "malformed JSON", ctx: context.Background(), args: json.RawMessage(`{"path":`), want: "参数解析失败"},
		{name: "invalid UTF-8 JSON", ctx: context.Background(), args: json.RawMessage{'{', '"', 0xff}, want: "UTF-8"},
		{name: "unknown root field", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"oldText":"old","newText":"new"}],"extra":true}`), want: "unknown field"},
		{name: "unknown item field", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"oldText":"old","newText":"new","extra":true}]}`), want: "unknown field"},
		{name: "trailing JSON", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"oldText":"old","newText":"new"}]} {}`), want: "多余"},
		{name: "blank path", ctx: context.Background(), args: editArguments(t, " ", editOperation("old", "new")), want: "path 不能为空"},
		{name: "absolute path", ctx: context.Background(), args: editArguments(t, absolutePath, editOperation("old", "new")), want: "相对路径"},
		{name: "missing edits", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt"}`), want: "edits"},
		{name: "null edits", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":null}`), want: "edits"},
		{name: "empty edits", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[]}`), want: "edits"},
		{name: "missing oldText", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"newText":"new"}]}`), want: "oldText"},
		{name: "blank oldText", ctx: context.Background(), args: editArguments(t, "file.txt", editOperation(" \n ", "new")), want: "oldText"},
		{name: "missing newText", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"oldText":"old"}]}`), want: "newText"},
		{name: "null newText", ctx: context.Background(), args: json.RawMessage(`{"path":"file.txt","edits":[{"oldText":"old","newText":null}]}`), want: "newText"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(tt.ctx, tt.args, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEditToolAllowsDeletionAndPreservesCRLFAndPermissions(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "config.txt")
	writeTestFile(t, path, []byte("before\r\nold one\r\nold two\r\nremove\r\nafter\r\n"))
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	tool := newEditToolForTest(t, workDir)

	_, err := tool.Execute(context.Background(), editArguments(t, "config.txt",
		editOperation("old one\nold two", "new one\nnew two"),
		editOperation("remove\n", ""),
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "before\r\nnew one\r\nnew two\r\nafter\r\n" {
		t.Fatalf("content = %q, error = %v", got, err)
	}
	if mode := statFile(t, path).Mode().Perm(); mode != 0o640 {
		t.Fatalf("permissions = %o", mode)
	}
}

func TestEditToolUsesSharedWorkspaceBoundaryAndLifecycle(t *testing.T) {
	workDir := t.TempDir()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "outside.txt")
	writeTestFile(t, outPath, []byte("outside"))
	workspace := newWorkspaceForTest(t, workDir)
	tool := editToolForWorkspace(t, workspace)
	relativeOutside, err := filepath.Rel(workDir, outPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), editArguments(t, relativeOutside, editOperation("outside", "changed")), nil); err == nil {
		t.Fatal("path traversal error = nil")
	}
	if err := os.Symlink(outPath, filepath.Join(workDir, "outside-link.txt")); err == nil {
		if _, err := tool.Execute(context.Background(), editArguments(t, "outside-link.txt", editOperation("outside", "changed")), nil); err == nil {
			t.Fatal("external symlink error = nil")
		}
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), editArguments(t, "missing.txt", editOperation("old", "new")), nil); err == nil {
		t.Fatal("Execute() after Workspace.Close error = nil")
	}
}

func TestEditToolRejectsNonFilesInvalidContentAndCancellation(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "nul.bin"), []byte{'a', 0, 'b'})
	writeTestFile(t, filepath.Join(workDir, "invalid.bin"), []byte{0xff, 0xfe})
	writeTestFile(t, filepath.Join(workDir, "valid.txt"), []byte("old"))
	tool := newEditToolForTest(t, workDir)
	tests := []struct{ path, want string }{
		{path: "directory", want: "普通文件"},
		{path: "missing.txt", want: "文件"},
		{path: "nul.bin", want: "NUL"},
		{path: "invalid.bin", want: "UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), editArguments(t, tt.path, editOperation("a", "b")), nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, editArguments(t, "valid.txt", editOperation("old", "new")), nil); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("canceled error = %v", err)
	}
}

func requireSchemaObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema object = %T (%#v)", value, value)
	}
	return object
}

func newEditToolForTest(t *testing.T, workDir string) Tool {
	t.Helper()
	return NewEditTool(newWorkspaceForTest(t, workDir))
}

func editToolForWorkspace(t *testing.T, workspace *Workspace) Tool {
	t.Helper()
	return NewEditTool(workspace)
}

func editOperation(oldText, newText string) map[string]any {
	return map[string]any{"oldText": oldText, "newText": newText}
}

func editArguments(t *testing.T, path string, edits ...map[string]any) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{"path": path, "edits": edits})
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}

func setFileModTime(t *testing.T, path string) {
	t.Helper()
	want := time.Unix(1_700_000_000, 123_000_000)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Fatal(err)
	}
}

func statFile(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertFileUnchanged(t *testing.T, path string, want []byte, wantModTime time.Time) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("content = %q, error = %v", got, err)
	}
	if gotModTime := statFile(t, path).ModTime(); !gotModTime.Equal(wantModTime) {
		t.Fatalf("mtime = %v, want %v", gotModTime, wantModTime)
	}
}
