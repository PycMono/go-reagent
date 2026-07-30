# Hardened edit_file Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a workspace-confined `edit_file` tool with four-level unique fuzzy matching, preserve unaffected bytes and file metadata, and register it beside `read_file` in the existing CLI.

**Architecture:** Keep fuzzy matching as pure byte-span selection helpers, then let `EditFileTool` own strict JSON parsing, `os.Root` confinement, text validation, and same-handle writeback. The CLI constructs both file tools and returns one composite closer; real modification behavior is exercised only in temporary directories.

**Tech Stack:** Go 1.26 standard library (`os.Root`, `encoding/json`, `io`, `unicode/utf8`, `errors`), existing `tools.BaseTool` and Registry interfaces, Go `testing` and race detector.

## Global Constraints

- Preserve Engine, Provider, Schema, Registry interfaces, config-driven startup, Thinking behavior, and the default read-only prompt.
- Add only `edit_file`; do not add `write_file`, `bash`, or a repository-root `server.go` fixture.
- Accept only regular UTF-8 text files beneath WorkDir; reject NUL content, absolute paths, traversal, and external symlink targets.
- Require one unique match at every fuzzy level; ambiguity is an error and never falls through to a weaker level.
- Preserve bytes outside the matched span, adapt replacement newlines to the matched/file style, and preserve file permissions.
- Keep `edit_file` exclusive by leaving `ToolDefinition.ParallelSafe` false.
- Add no third-party dependency.
- The workspace has no Git metadata, so commit steps are unavailable; every task ends with fresh verification instead.

---

## File Structure

- Create `internal/tools/edit_file.go`: strict argument decoding, workspace-confined editing, byte-span matching, newline adaptation, and writeback.
- Create `internal/tools/edit_file_test.go`: matching, ambiguity, path boundary, text validation, metadata, cancellation, and lifecycle tests.
- Modify `cmd/reagent/main.go`: create/register both tools and close them through one `io.Closer`.
- Modify `cmd/reagent/main_test.go`: assert both definitions and execute a real edit through Registry against a temp file.
- Modify `README.md`: document the new tool, its safety boundary, and updated project layout/capabilities.

---

### Task 1: Four-level unique matcher

**Files:**
- Create: `internal/tools/edit_file_test.go`
- Create: `internal/tools/edit_file.go`

**Interfaces:**
- Produces: `type textMatch struct { start int; end int; level int }`
- Produces: `func findUniqueTextMatch(content, oldText string) (textMatch, error)`
- Produces: `func replacementForMatch(content string, match textMatch, newText string) string`

- [ ] **Step 1: Write failing L1 and ambiguity tests**

Create table-driven tests with literal byte spans and results:

```go
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
```

The mutation caught is accepting the first exact occurrence instead of requiring uniqueness.

- [ ] **Step 2: Run the matcher tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestFindUniqueTextMatch' ./internal/tools`

Expected: compilation fails because `findUniqueTextMatch` and `textMatch` do not exist.

- [ ] **Step 3: Implement minimal L1 matching**

Add `textMatch`, an occurrence scanner that records every start position (including overlapping candidates), and a helper that returns zero/one/many status. L1 returns the sole original byte span, returns an error containing the count for multiple matches, and proceeds only when there are none.

```go
type textMatch struct {
    start int
    end   int
    level int
}

func occurrenceIndexes(content, needle string) []int {
    var indexes []int
    for offset := 0; offset <= len(content)-len(needle); {
        index := strings.Index(content[offset:], needle)
        if index < 0 {
            break
        }
        index += offset
        indexes = append(indexes, index)
        offset = index + 1
    }
    return indexes
}
```

- [ ] **Step 4: Run L1 tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestFindUniqueTextMatch(Exact|RejectsExactAmbiguity)$' ./internal/tools`

Expected: PASS.

- [ ] **Step 5: Write failing L2, L3, and L4 tests**

Add separate tests whose literal fixtures force each fallback:

```go
func TestFindUniqueTextMatchNormalizesOnlyNewlines(t *testing.T) {
    content := "before\r\nline one\r\nline two\r\nafter"
    match, err := findUniqueTextMatch(content, "line one\nline two")
    if err != nil || content[match.start:match.end] != "line one\r\nline two" || match.level != 2 {
        t.Fatalf("matched = %q, match = %#v, error = %v", content[match.start:match.end], match, err)
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
```

Add ambiguity fixtures for L2 and L4 and assert the error contains the literal match count. Add a no-match test asserting the error recommends `read_file`.

- [ ] **Step 6: Run fallback tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestFindUniqueTextMatch' ./internal/tools`

Expected: L2/L3/L4 tests fail because only exact matching exists.

- [ ] **Step 7: Implement normalized byte mapping and line-window matching**

Implement:

```go
func normalizeNewlinesWithBoundaries(input string) (string, []int)
func normalizedMatch(content, oldText string, level int) ([]textMatch, error)
func lineByLineMatches(content, oldText string) []textMatch
```

`normalizeNewlinesWithBoundaries` returns normalized text plus an array of length `len(normalized)+1`; each normalized boundary maps to its original byte boundary. When consuming `\r\n`, append one `\n` and map the next normalized boundary past both original bytes. L2 finds normalized occurrences and maps `[start,end]` back to original byte spans. L3 applies `strings.TrimSpace` only to normalized `oldText`. L4 builds original line records with content start/end offsets, compares `strings.TrimSpace` values, and returns spans from the first matched line's content start through the last matched line's content end without its terminator.

At every level call the same uniqueness gate: one returns, many errors immediately, zero advances.

- [ ] **Step 8: Write failing replacement newline tests**

```go
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
```

- [ ] **Step 9: Implement newline adaptation and run matcher tests GREEN**

Normalize replacement CRLF to LF, detect CRLF or LF first within the matched span, otherwise count file-level CRLF and lone LF terminators, and convert replacement LF to the chosen style. Leave replacement unchanged when no style can be inferred.

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^(TestFindUniqueTextMatch|TestReplacementFor)' ./internal/tools`

Expected: PASS.

---

### Task 2: Workspace-confined EditFileTool execution

**Files:**
- Modify: `internal/tools/edit_file.go`
- Modify: `internal/tools/edit_file_test.go`

**Interfaces:**
- Consumes: `findUniqueTextMatch`, `replacementForMatch`, and existing `BaseTool`.
- Produces: `func NewEditFileTool(workDir string) (*EditFileTool, error)` and the complete `EditFileTool` lifecycle.

- [ ] **Step 1: Write failing definition and strict argument tests**

Test the consumer-visible schema and Execute validation: definition name `edit_file`, non-empty description, `ParallelSafe == false`, three required fields, and `additionalProperties == false`. Table-test malformed JSON, unknown field, trailing JSON, blank path, absolute path, blank `old_text`, and nil Context. Assert empty `new_text` is accepted later by a deletion behavior test.

```go
func TestEditFileToolDefinitionIsExclusive(t *testing.T) {
    tool := newEditFileToolForTest(t, t.TempDir())
    definition := tool.Definition()
    if definition.Name != "edit_file" || definition.Description == "" || definition.ParallelSafe {
        t.Fatalf("definition = %#v", definition)
    }
}
```

- [ ] **Step 2: Run tool API tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestEditFileTool(Definition|RejectsInvalidArguments)' ./internal/tools`

Expected: compilation fails because `EditFileTool` and `NewEditFileTool` do not exist.

- [ ] **Step 3: Implement constructor, definition, close, and argument decoding**

Follow `read_file.go` conventions:

```go
type EditFileTool struct { root *os.Root }
type editFileArgs struct {
    Path    string `json:"path"`
    OldText string `json:"old_text"`
    NewText string `json:"new_text"`
}

func NewEditFileTool(workDir string) (*EditFileTool, error)
func (t *EditFileTool) Close() error
func decodeEditFileArgs(args json.RawMessage) (editFileArgs, error)
```

Trim/validate WorkDir, call `filepath.Abs`, then `os.OpenRoot`. Decode exactly one JSON value with `DisallowUnknownFields`. Validate a nonblank relative path and nonempty `old_text` without trimming the actual match text.

- [ ] **Step 4: Write failing real edit, deletion, metadata, and path tests**

Use real files in `t.TempDir()` and assert disk contents, not helper calls:

```go
func TestEditFileToolEditsExistingText(t *testing.T) {
    workDir := t.TempDir()
    path := filepath.Join(workDir, "server.go")
    writeTestFile(t, path, []byte("package main\n\nif true {\n\tfmt.Println(\"open\")\n}\n"))
    tool := newEditFileToolForTest(t, workDir)
    args := editFileArguments(t, "server.go", "if true {\nfmt.Println(\"open\")\n}", "if user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}")
    if _, err := tool.Execute(context.Background(), args); err != nil { t.Fatal(err) }
    got, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    want := "package main\n\nif user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}\n"
    if string(got) != want { t.Fatalf("content = %q", got) }
}
```

Add tests for deleting the sole match with empty `new_text`, preserving mode `0o640`, CRLF retention, nested paths, internal symlink success, external symlink failure, `../` traversal, directory/missing path, NUL, invalid UTF-8, canceled Context, Close, and nonexistent WorkDir.

- [ ] **Step 5: Run execution tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^Test(EditFileTool|NewEditFileTool)' ./internal/tools`

Expected: behavior tests fail because Execute does not yet edit files.

- [ ] **Step 6: Implement same-handle safe execution**

`Execute` must:

1. reject nil/uninitialized tool and nil Context;
2. decode/validate arguments and check cancellation;
3. open once with `t.root.OpenFile(path, os.O_RDWR, 0)`;
4. `Stat` the handle and require `Mode().IsRegular()`;
5. `io.ReadAll`, require `utf8.Valid` and no NUL;
6. select one span and adapt replacement newlines;
7. build `original[:start] + replacement + original[end:]` and check cancellation;
8. `Seek(0, io.SeekStart)`, write all bytes in a short-write-safe loop, then `Truncate` to the final length;
9. return `成功修改文件: <relative path>`.

Wrap each filesystem failure with operation context but never include the absolute WorkDir.

- [ ] **Step 7: Run all tool tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/tools`

Expected: PASS.

---

### Task 3: CLI registration and real temporary-file integration

**Files:**
- Modify: `cmd/reagent/main_test.go`
- Modify: `cmd/reagent/main.go`

**Interfaces:**
- Consumes: `tools.NewReadFileTool`, `tools.NewEditFileTool`, `MutableRegistry.Register`.
- Preserves: `func registryForWorkDir(workDir string) (tools.Registry, io.Closer, error)`.

- [ ] **Step 1: Change the assembly test and verify RED**

Rename the existing test to `TestRegistryForWorkDirAdvertisesAndExecutesFileTools`. Assert sorted definitions are exactly `edit_file`, `read_file`; execute this call through Registry:

```go
result := registry.Execute(context.Background(), schema.ToolCall{
    ID:   "call-edit",
    Name: "edit_file",
    Arguments: []byte(`{"path":"server.go","old_text":"if true {\n    fmt.Println(\"open\")\n}","new_text":"if user == nil {\n    fmt.Println(\"Forbidden!\")\n    return\n}"}`),
})
```

Read the temp file afterward and compare it to a hand-written expected literal. Also execute `read_file` to prove it observes the modified content.

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestRegistryForWorkDirAdvertisesAndExecutesFileTools$' ./cmd/reagent`

Expected: FAIL because only `read_file` is registered.

- [ ] **Step 2: Implement composite resource lifecycle**

Add a private closer in `cmd/reagent/main.go`:

```go
type toolClosers []io.Closer

func (closers toolClosers) Close() error {
    var errs []error
    for index := len(closers)-1; index >= 0; index-- {
        errs = append(errs, closers[index].Close())
    }
    return errors.Join(errs...)
}
```

Construct `read_file`, then `edit_file`, register both, and close already-created resources on every failure. Return `toolClosers{readFileTool, editFileTool}`. Leave provider construction, engine options, default Prompt, and environment overrides unchanged.

- [ ] **Step 3: Run CLI and package tests GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./cmd/reagent ./internal/tools`

Expected: PASS.

---

### Task 4: Documentation and full verification

**Files:**
- Modify: `README.md`
- Verify: all Go source beneath `cmd` and `internal`.

**Interfaces:**
- Documents: `edit_file(path, old_text, new_text)`, four-level unique matching, workspace boundary, text-only rule, newline/permission preservation, and exclusive scheduling.

- [ ] **Step 1: Update README**

Update the status paragraph and project tree to include `edit_file.go` and `edit_file_test.go`. Add an `edit_file` subsection stating:

- it only edits existing regular UTF-8 files beneath WorkDir;
- `old_text` must uniquely match through exact, newline-normalized, edge-trimmed, or indentation-insensitive matching;
- ambiguity and no-match return corrective errors;
- unaffected bytes, newline style, and file permissions are retained;
- it is an exclusive tool and is not run concurrently with other calls.

Add `edit_file` to current capabilities and change the roadmap tool item to name both `read_file` and `edit_file`. Do not change the default prompt description.

- [ ] **Step 2: Format changed Go files**

Run: `gofmt -w internal/tools/edit_file.go internal/tools/edit_file_test.go cmd/reagent/main.go cmd/reagent/main_test.go`

Expected: exit code 0.

- [ ] **Step 3: Run static analysis**

Run: `GOCACHE=/tmp/go-reagent-build-cache go vet ./...`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 4: Run race-enabled full tests**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./...`

Expected: every package reports PASS and the command exits 0.

- [ ] **Step 5: Verify formatting and scope**

Run: `gofmt -l cmd internal`

Expected: no output.

Run: `rg -n 'github.com/yourname|NewWriteFileTool|NewBashTool|server.go 文件' --glob '!docs/**' .`

Expected: no matches.

Run: `rg -n 'edit_file' README.md cmd/reagent internal/tools`

Expected: matches in the new tool, tests, CLI assembly, and documentation.
