# OpenClaw Local Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the OpenClaw-style local coding tool loop: write, structured patching, shell execution, and background process control.

**Architecture:** Keep each tool behind the existing `tools.BaseTool` contract. File mutation tools use `os.Root`; `exec` and `process` share a bounded `ProcessManager` owned by Fx lifecycle. All APIs use strict JSON decoding and return stable JSON/text observations.

**Tech Stack:** Go 1.26 standard library, `os.Root`, `os/exec`, existing Fx Registry and test helpers.

## Global Constraints

- Preserve all pre-existing uncommitted work and modify overlapping files incrementally.
- Do not add third-party dependencies.
- Keep file paths workspace-relative and file content UTF-8 text without NUL.
- Cap command timeout at 600000 ms, per-poll wait at 30000 ms, and retained process output at 50 KiB.
- Do not claim that a workspace cwd is a shell sandbox.
- Do not commit unless the user explicitly requests a commit.

---

### Task 1: `write_file`

**Files:**
- Create: `internal/tools/write_file.go`
- Create: `internal/tools/write_file_test.go`

**Interfaces:**
- Produces: `NewWriteFileTool(workDir string) (*WriteFileTool, error)` and a `BaseTool` named `write_file`.

- [ ] Write failing tests for definition metadata, create/overwrite/idempotency, parent directories, invalid JSON/text, workspace escape, cancellation and close.
- [ ] Run `go test ./internal/tools -run '^Test(New)?WriteFileTool' -count=1` and verify compilation/test failure because the tool is absent.
- [ ] Implement strict `{path,content}` decoding, `os.Root` containment, UTF-8/NUL validation, parent creation, regular-file checks and stable results.
- [ ] Re-run the targeted tests and keep them green.

### Task 2: `apply_patch`

**Files:**
- Create: `internal/tools/apply_patch.go`
- Create: `internal/tools/apply_patch_parser.go`
- Create: `internal/tools/apply_patch_test.go`

**Interfaces:**
- Produces: `NewApplyPatchTool(workDir string) (*ApplyPatchTool, error)` and a `BaseTool` named `apply_patch` accepting `{input:string}`.
- Consumes: the same workspace text validation rules as `write_file`.

- [ ] Write failing tests for Add/Update/Delete/Move, multi-file changes, unique context, malformed patches, duplicate destinations, workspace escape, preflight atomicity, cancellation and close.
- [ ] Run `go test ./internal/tools -run '^Test(New)?ApplyPatch' -count=1` and verify failure because the tool is absent.
- [ ] Implement the `*** Begin Patch` parser and an in-memory preflight result map before any mutation.
- [ ] Apply validated operations through `os.Root` and re-run targeted tests.

### Task 3: shared process manager, `exec`, and `process`

**Files:**
- Create: `internal/tools/process_manager.go`
- Create: `internal/tools/process_group_unix.go`
- Create: `internal/tools/process_group_windows.go`
- Create: `internal/tools/exec.go`
- Create: `internal/tools/exec_test.go`
- Create: `internal/tools/process.go`
- Create: `internal/tools/process_test.go`

**Interfaces:**
- Produces: `NewProcessManager(workDir string) (*ProcessManager, error)`.
- Produces: `NewExecTool(manager *ProcessManager) *ExecTool` named `exec`.
- Produces: `NewProcessTool(manager *ProcessManager) *ProcessTool` named `process`.
- `ProcessManager.Close()` terminates active process groups and is Fx-owned.

- [ ] Write failing tests for definitions, success/non-zero/stderr, workdir validation, timeout, 50 KiB tail output, background/yield behavior, list/poll/write/kill, cancellation and close.
- [ ] Run `go test ./internal/tools -run '^Test(Process|Exec|NewProcess)' -count=1` and verify failure because the APIs are absent.
- [ ] Implement bounded combined output, session state, shell selection, process-group termination and strict argument decoders.
- [ ] Re-run targeted tests, then run them with `-race`.

### Task 4: Registry and documentation integration

**Files:**
- Modify: `internal/app/providers.go`
- Modify: `internal/app/providers_test.go`
- Modify: `.claw/skills/git-workflow/SKILL.md`
- Modify: `README.md`

**Interfaces:**
- Registry exposes exactly `apply_patch`, `edit_file`, `exec`, `process`, `read_file`, and `write_file` in sorted order.

- [ ] Change the Registry test first to require all six tools and lifecycle cleanup.
- [ ] Run `go test ./internal/app -run '^TestNewRegistry' -count=1` and verify it fails with only two definitions.
- [ ] Construct/register all tools with failure cleanup; share one `ProcessManager` between `exec` and `process`.
- [ ] Change the Git skill from unavailable `bash` to `exec`, and document the new tool surface and shell security boundary in README.
- [ ] Re-run `go test ./internal/app -count=1`.

### Task 5: verification and review

**Files:**
- Verify all modified and created files.

- [ ] Run `gofmt` on new and modified Go files.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `go test -race ./internal/tools ./internal/app -count=1`.
- [ ] Run `go vet ./...`.
- [ ] Run `git diff --check` and inspect `git diff --stat` plus the focused diff.
- [ ] Review requirements against `docs/superpowers/specs/2026-07-31-openclaw-local-tools-design.md` and fix any gap before completion.
