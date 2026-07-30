# go-reagent Complete Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Normalize every legacy project identity to `go-reagent` with canonical module path `github.com/PycMono/go-reagent`.

**Architecture:** Perform the rename in two independently verifiable layers. First update the Go module identity, internal imports, runtime identity, and command package; then update user-facing and historical documentation before a repository-wide residue scan. Runtime behavior and third-party dependency versions remain unchanged.

**Tech Stack:** Go 1.26, Go modules, Markdown, Git

## Global Constraints

- The exact Go module path is `github.com/PycMono/go-reagent`.
- The command entry point is `cmd/reagent` and the run command is `go run ./cmd/reagent`.
- Do not change runtime behavior, configuration structure, tool protocols, or third-party dependency versions.
- Update existing plans and specs as part of the complete rename.
- Preserve `OpenClaw` when it refers to the external project or its concepts.
- Preserve all pre-existing worktree changes and do not stage or commit implementation files without explicit user authorization.

---

### Task 1: Rename the Go module and runnable command

**Files:**
- Modify: `go.mod`
- Modify: `internal/engine/loop.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/provider/interface.go`
- Modify: `internal/provider/openai.go`
- Modify: `internal/provider/openai_test.go`
- Modify: `internal/provider/claude.go`
- Modify: `internal/provider/claude_test.go`
- Modify: `internal/schema/message_test.go`
- Modify: `internal/tools/read_file.go`
- Modify: `internal/tools/edit_file.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/config/config.go`
- Move: legacy command `main.go` to `cmd/reagent/main.go`
- Move: legacy command `main_test.go` to `cmd/reagent/main_test.go`

**Interfaces:**
- Consumes: the existing internal package APIs without signature changes.
- Produces: local packages importable below `github.com/PycMono/go-reagent/internal/...` and the executable package `github.com/PycMono/go-reagent/cmd/reagent`.

- [x] **Step 1: Run the source-identity guard and verify the old identity is present**

```bash
test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent" && \
test -d cmd/reagent && \
! rg -n '"go-reagent/internal/' --glob '*.go' cmd internal
```

Expected: FAIL because `go.mod`, internal imports, runtime strings, and the command directory still use the legacy identity.

- [x] **Step 2: Update the module path and every internal Go import**

Replace the legacy module declaration and internal import prefix with these exact targets:

```text
module github.com/PycMono/go-reagent

github.com/PycMono/go-reagent/internal/<package>
```

Do not modify any entry in either `require` block.

- [x] **Step 3: Move and retarget the command package**

Move both legacy command files to `cmd/reagent/`, then use these exact target strings:

```text
cmd/reagent/main.go

You are go-reagent, an expert coding assistant.

Package config loads and validates the go-reagent process configuration.
```

Update tool-description examples and the default agent prompt to `cmd/reagent/main.go`. Update the configuration-path unit-test fixture to `/secure/reagent.json` for naming consistency.

- [x] **Step 4: Format and rerun the source-identity guard**

```bash
gofmt -w cmd/reagent/main.go cmd/reagent/main_test.go internal/engine/loop.go \
  internal/engine/loop_test.go internal/provider/interface.go internal/provider/openai.go \
  internal/provider/openai_test.go internal/provider/claude.go internal/provider/claude_test.go \
  internal/schema/message_test.go internal/tools/read_file.go internal/tools/edit_file.go \
  internal/tools/registry.go internal/tools/registry_test.go internal/config/config.go

test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent" && \
test -d cmd/reagent && \
! rg -n '"go-reagent/internal/' --glob '*.go' cmd internal
```

Expected: PASS.

- [x] **Step 5: Verify the renamed Go package graph**

```bash
go list ./...
go test ./...
```

Expected: every local package starts with `github.com/PycMono/go-reagent/`, and all tests pass.

### Task 2: Rename user-facing content and repository history documents

**Files:**
- Modify: `README.md`
- Modify: `hello.txt`
- Modify: `docs/superpowers/specs/*.md` containing the old identity
- Modify: `docs/superpowers/plans/*.md` containing the old identity

**Interfaces:**
- Consumes: the module and command paths produced by Task 1.
- Produces: documentation and examples that consistently identify the project as `go-reagent` and run it through `cmd/reagent`.

- [x] **Step 1: Run the repository residue guard and verify documentation still fails it**

```bash
rg -n --hidden --glob '!.git/**' --glob '!.idea/**' 'OpenClaw' .
```

Expected: output identifies the external-name references that must be preserved while other project identity text is updated.

- [x] **Step 2: Update project identity and command examples**

Replace legacy identity references across `README.md`, `hello.txt`, and existing Markdown files with these exact targets:

```text
go-reagent

cmd/reagent

/tmp/go-reagent-build-cache

/secure/reagent/config.<extension>
```

Where a historical document describes the Go import path, use `github.com/PycMono/go-reagent/internal/...` rather than the short project name. Leave `OpenClaw` unchanged.

- [x] **Step 3: Inspect all remaining external-name occurrences**

```bash
rg -n --hidden --glob '!.git/**' --glob '!.idea/**' 'OpenClaw' .
```

Expected: project source and user-facing documentation contain no legacy identity; deliberate `OpenClaw` references remain because they name an external project.

- [x] **Step 4: Run the complete rename acceptance suite**

```bash
test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent"
test -d cmd/reagent
! rg -n '"go-reagent/internal/' --glob '*.go' cmd internal
rg -n --hidden --glob '!.git/**' --glob '!.idea/**' 'OpenClaw' .
go list ./...
go test ./...
go vet ./...
git diff --check
```

Expected: every command passes; `go list` reports only the canonical module prefix; tests and vet are clean.

- [x] **Step 5: Review the final diff without staging user work**

```bash
git status --short
git diff -- README.md docs/superpowers/specs docs/superpowers/plans
```

Expected: only the requested rename appears in touched tracked files; pre-existing untracked and modified files remain unstaged.
