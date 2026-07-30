# Real Tool Registry and read_file Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the CLI's Mock Weather Registry with a thread-safe real Registry and a workspace-confined text `read_file` tool.

**Architecture:** Preserve the Engine-facing `Registry` interface and add `BaseTool` plus `MutableRegistry` for startup registration. Implement `read_file` with Go 1.26 `os.Root`, strict JSON parsing, regular-file enforcement, UTF-8/binary validation, and an 8000-byte output budget. Wire the real tool into `cmd/reagent` without changing Engine, Provider, or Schema behavior.

**Tech Stack:** Go 1.26 standard library (`os.Root`, `encoding/json`, `sync`, `unicode/utf8`), existing internal schema and engine interfaces, Go `testing` and race detector.

## Global Constraints

- Internal imports use `go-reagent/internal/...`, never `github.com/yourname/...`.
- Engine, Provider, Schema, and `LLMProvider` behavior remain unchanged.
- `Registry` remains the two-method Engine-facing interface.
- `read_file` accepts only relative paths beneath WorkDir, including safe internal symlinks.
- External symlink targets, absolute paths, traversal, non-regular files, and binary/non-UTF-8 content in the inspected read window are rejected.
- Tool output is bounded to 8000 bytes at a valid UTF-8 boundary.
- No third-party dependency is added.
- The workspace has no Git metadata, so commit steps are unavailable; each task ends with a fresh test checkpoint.

---

### Task 1: Thread-safe real Registry

**Files:**
- Modify: `internal/tools/registry.go`
- Create: `internal/tools/registry_test.go`

**Interfaces:**
- Preserves: `type Registry interface { GetAvailableTools() []schema.ToolDefinition; Execute(context.Context, schema.ToolCall) schema.ToolResult }`
- Produces: `type BaseTool interface { Name() string; Definition() schema.ToolDefinition; Execute(context.Context, json.RawMessage) (string, error) }`
- Produces: `type MutableRegistry interface { Registry; Register(BaseTool) error }`
- Produces: `func NewRegistry() MutableRegistry`

- [ ] **Step 1: Add failing registration and discovery tests**

Create a `stubTool` with configurable name, definition, output, error, panic, and received arguments. Add tests that register tools named `zeta` and `alpha`, assert definitions return in `alpha`, `zeta` order, and reject nil, typed-nil, blank name, mismatched definition name, and duplicates:

```go
func TestRegistryRegistersAndSortsDefinitions(t *testing.T) {
    registry := NewRegistry()
    for _, name := range []string{"zeta", "alpha"} {
        if err := registry.Register(&stubTool{name: name}); err != nil {
            t.Fatalf("Register(%q) error = %v", name, err)
        }
    }
    definitions := registry.GetAvailableTools()
    if len(definitions) != 2 || definitions[0].Name != "alpha" || definitions[1].Name != "zeta" {
        t.Fatalf("definitions = %#v", definitions)
    }
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
    var typedNil *stubTool
    tests := []struct { name string; tool BaseTool }{
        {"nil", nil},
        {"typed nil", typedNil},
        {"blank", &stubTool{name: " "}},
        {"mismatch", &stubTool{name: "route", definitionName: "schema"}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if err := NewRegistry().Register(tt.tool); err == nil { t.Fatal("Register() error = nil") }
        })
    }
}
```

- [ ] **Step 2: Run Registry tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test ./internal/tools`

Expected: compile failure because `BaseTool`, `MutableRegistry`, `NewRegistry`, and `Register` do not exist.

- [ ] **Step 3: Add failing routing, cancellation, error, and panic tests**

Assert successful arguments are passed unchanged; unknown tools and tool errors set `IsError`; canceled Context does not call the tool; and a panicking tool produces an error result while preserving `ToolCallID`.

```go
func TestRegistryExecutesRegisteredTool(t *testing.T) {
    tool := &stubTool{name: "echo", output: "ok"}
    registry := NewRegistry()
    if err := registry.Register(tool); err != nil { t.Fatal(err) }
    call := schema.ToolCall{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}
    result := registry.Execute(context.Background(), call)
    if result.IsError || result.ToolCallID != call.ID || result.Output != "ok" { t.Fatalf("result = %#v", result) }
    if string(tool.received) != string(call.Arguments) { t.Fatalf("args = %s", tool.received) }
}
```

- [ ] **Step 4: Implement the Registry**

Use `reflect.ValueOf(tool).IsNil()` only for nil-capable kinds, validate registration before taking the write lock, reject duplicates under the lock, snapshot tools under a read lock, sort definitions by name, and release the read lock before calling `tool.Execute`. Wrap Execute in a deferred recover that returns `ToolResult{ToolCallID: call.ID, IsError: true}` and logs only tool name plus `debug.Stack()`.

Core declarations:

```go
type registryImpl struct {
    mu    sync.RWMutex
    tools map[string]BaseTool
}

func NewRegistry() MutableRegistry {
    return &registryImpl{tools: make(map[string]BaseTool)}
}
```

- [ ] **Step 5: Run Registry tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/tools`

Expected: PASS.

---

### Task 2: Workspace-confined read_file

**Files:**
- Create: `internal/tools/read_file.go`
- Create: `internal/tools/read_file_test.go`

**Interfaces:**
- Implements: `BaseTool`
- Produces: `func NewReadFileTool(workDir string) (*ReadFileTool, error)`
- Produces: `func (t *ReadFileTool) Close() error`

- [ ] **Step 1: Add failing definition, parsing, and normal-read tests**

Verify the definition contains `additionalProperties: false`, read a root file and nested file, and reject malformed JSON, unknown fields, trailing JSON, blank paths, and absolute paths.

```go
func TestReadFileToolReadsWorkspaceFile(t *testing.T) {
    workDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello"), 0o600); err != nil { t.Fatal(err) }
    tool, err := NewReadFileTool(workDir)
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { _ = tool.Close() })
    output, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
    if err != nil || output != "hello" { t.Fatalf("output = %q, error = %v", output, err) }
}
```

- [ ] **Step 2: Run read_file tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestReadFile' ./internal/tools`

Expected: compile failure because `ReadFileTool` and `NewReadFileTool` do not exist.

- [ ] **Step 3: Add failing path-boundary and file-type tests**

Create an external temp directory and assert `../`, absolute paths, and a symlink pointing outside fail. Create a relative symlink pointing to another file inside WorkDir and assert it succeeds. Assert directories and missing files fail. Skip symlink cases only when `os.Symlink` reports an unsupported-platform error.

- [ ] **Step 4: Add failing output-budget and text-validation tests**

Cover exactly 8000 ASCII bytes, 8001 bytes, a Chinese rune crossing byte 8000, invalid UTF-8, NUL bytes, canceled Context, and execution after `Close`. Assert truncated output is valid UTF-8 and includes the fixed marker.

```go
const wantTruncationMarker = "...[文件内容超过限制，已截断至前 8000 字节]..."
```

- [ ] **Step 5: Implement read_file**

Construct `*os.Root` with `os.OpenRoot(workDir)`. Strictly decode one JSON object with `DisallowUnknownFields`, trim and reject empty/absolute paths, check Context, call `root.Open`, call `file.Stat` on the opened handle, require `Mode().IsRegular`, and read at most `maxReadFileBytes + utf8.UTFMax` bytes with `io.LimitReader`.

Use these declarations:

```go
const maxReadFileBytes = 8000
const readFileTruncationMarker = "...[文件内容超过限制，已截断至前 8000 字节]..."

type ReadFileTool struct { root *os.Root }
type readFileArgs struct { Path string `json:"path"` }
```

Reject NUL bytes in the inspected window. For content longer than 8000 bytes, decrement the cut point from 8000 by at most `utf8.UTFMax-1` until `utf8.Valid(content[:cut])`; if no valid returned prefix exists, reject as non-UTF-8. For untruncated content require `utf8.Valid(content)`.

- [ ] **Step 6: Run all tool tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/tools`

Expected: PASS.

---

### Task 3: Wire the real tool into the CLI

**Files:**
- Modify: `cmd/reagent/main.go`
- Modify: `cmd/reagent/main_test.go`
- Delete: `cmd/reagent/mock_registry.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `tools.NewRegistry`, `tools.NewReadFileTool`, and `MutableRegistry.Register`.
- Preserves: `providerFromConfig`, `configurationPath`, and `AGENT_PROMPT` override.

- [ ] **Step 1: Remove Mock Weather tests and add a failing real-registry assembly test**

Extract:

```go
func registryForWorkDir(workDir string) (tools.Registry, io.Closer, error)
```

Test that its available tools contain exactly `read_file`, execute it against a temp file, and close it through the returned `io.Closer`.

- [ ] **Step 2: Run CLI tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestRegistryForWorkDir' ./cmd/reagent`

Expected: FAIL because `registryForWorkDir` does not exist.

- [ ] **Step 3: Implement CLI assembly and remove the Mock file**

In `main`, call `registryForWorkDir`, defer the closer, construct the Engine with Thinking `false`, and default the prompt to:

```text
请调用 read_file 工具读取当前工作区的 README.md，并用一句话总结这个项目的用途。
```

`registryForWorkDir` creates `ReadFileTool`, creates Registry, registers the tool, closes the tool if registration fails, and returns the Registry plus the tool as `io.Closer`.

- [ ] **Step 4: Update README**

Remove Mock Weather claims, show `read_file.go` and its tests in the project tree, explain the WorkDir boundary, symlink policy, text-only rule, and 8000-byte limit, and mark the Registry/basic-tool roadmap item complete.

- [ ] **Step 5: Run CLI and tool tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./cmd/reagent ./internal/tools`

Expected: PASS.

---

### Task 4: Full regression and security verification

**Files:**
- Verify all Go source under `cmd` and `internal` plus `README.md`.

**Interfaces:**
- Verifies complete Provider → Engine → Registry → read_file integration without a real API call.

- [ ] **Step 1: Format source**

Run: `gofmt -w cmd internal`

Expected: exit code 0.

- [ ] **Step 2: Run static analysis**

Run: `GOCACHE=/tmp/go-reagent-build-cache go vet ./...`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 3: Run race-enabled tests**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./...`

Expected: all packages PASS.

- [ ] **Step 4: Check formatting and stale Mock references**

Run: `gofmt -l cmd internal`

Expected: no output.

Run: `rg -n 'mockRegistry|get_weather|Mock Weather|mock_registry.go|github.com/yourname' --glob '!docs/**' .`

Expected: no matches.
