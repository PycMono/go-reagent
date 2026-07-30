# go-reagent Complete Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Completely rename the project from `go-reagent` to `go-reagent` with canonical module path `github.com/PycMono/go-reagent`.

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
- Move: `cmd/reagent/main.go` to `cmd/reagent/main.go`
- Move: `cmd/reagent/main_test.go` to `cmd/reagent/main_test.go`

**Interfaces:**
- Consumes: the existing internal package APIs without signature changes.
- Produces: local packages importable below `github.com/PycMono/go-reagent/internal/...` and the executable package `github.com/PycMono/go-reagent/cmd/reagent`.

- [ ] **Step 1: Run the source-identity guard and verify the old identity is present**

```bash
test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent" && \
test -d cmd/reagent && test ! -e cmd/reagent && \
! rg -n 'go-reagent|cmd/reagent' --glob '*.go' go.mod cmd internal
```

Expected: FAIL because `go.mod`, internal imports, runtime strings, and `cmd/reagent` still use the old identity.

- [ ] **Step 2: Update the module path and every internal Go import**

Apply these exact mappings to `go.mod` and Go source files:

```text
module go-reagent
→ module github.com/PycMono/go-reagent

go-reagent/internal/<package>
→ github.com/PycMono/go-reagent/internal/<package>
```

Do not modify any entry in either `require` block.

- [ ] **Step 3: Move and retarget the command package**

Move both command files to `cmd/reagent/`, then apply these exact source mappings:

```text
cmd/reagent/main.go
→ cmd/reagent/main.go

You are go-reagent, an expert coding assistant.
→ You are go-reagent, an expert coding assistant.

Package config loads and validates the claw process configuration.
→ Package config loads and validates the go-reagent process configuration.
```

Update tool-description examples and the default agent prompt from `cmd/reagent/main.go` to `cmd/reagent/main.go`. Keep `/secure/claw.json` in the configuration-path unit test because it is arbitrary test input rather than a project identity.

- [ ] **Step 4: Format and rerun the source-identity guard**

```bash
gofmt -w cmd/reagent/main.go cmd/reagent/main_test.go internal/engine/loop.go \
  internal/engine/loop_test.go internal/provider/interface.go internal/provider/openai.go \
  internal/provider/openai_test.go internal/provider/claude.go internal/provider/claude_test.go \
  internal/schema/message_test.go internal/tools/read_file.go internal/tools/edit_file.go \
  internal/tools/registry.go internal/tools/registry_test.go internal/config/config.go

test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent" && \
test -d cmd/reagent && test ! -e cmd/reagent && \
! rg -n 'go-reagent|cmd/reagent' --glob '*.go' go.mod cmd internal
```

Expected: PASS.

- [ ] **Step 5: Verify the renamed Go package graph**

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

- [ ] **Step 1: Run the repository residue guard and verify documentation still fails it**

```bash
! rg -n --hidden --glob '!.git/**' --glob '!.idea/**' \
  'go-reagent|cmd/reagent|/tmp/go-reagent-build-cache' .
```

Expected: FAIL with matches in `README.md`, `hello.txt`, existing specs, and existing plans.

- [ ] **Step 2: Update project identity and command examples**

Apply these exact mappings across `README.md`, `hello.txt`, and existing Markdown files:

```text
go-reagent
→ go-reagent

cmd/reagent
→ cmd/reagent

/tmp/go-reagent-build-cache
→ /tmp/go-reagent-build-cache

/secure/reagent/config.<extension>
→ /secure/reagent/config.<extension>
```

Where a historical document describes the Go import path, use `github.com/PycMono/go-reagent/internal/...` rather than the short project name. Leave `OpenClaw` unchanged.

- [ ] **Step 3: Inspect all remaining `claw` occurrences**

```bash
rg -n --hidden --glob '!.git/**' --glob '!.idea/**' -i 'claw' .
```

Expected: only the deliberate external `OpenClaw` reference and the arbitrary `/secure/claw.json` unit-test fixture remain. Any project identity or command-path occurrence is an error and must be changed.

- [ ] **Step 4: Run the complete rename acceptance suite**

```bash
test "$(sed -n '1p' go.mod)" = "module github.com/PycMono/go-reagent"
test -d cmd/reagent
test ! -e cmd/reagent
! rg -n --hidden --glob '!.git/**' --glob '!.idea/**' \
  'go-reagent|cmd/reagent|/tmp/go-reagent-build-cache' .
go list ./...
go test ./...
go vet ./...
git diff --check
```

Expected: every command passes; `go list` reports only the canonical module prefix; tests and vet are clean.

- [ ] **Step 5: Review the final diff without staging user work**

```bash
git status --short
git diff -- README.md docs/superpowers/specs docs/superpowers/plans
```

Expected: only the requested rename appears in touched tracked files; pre-existing untracked and modified files remain unstaged.
