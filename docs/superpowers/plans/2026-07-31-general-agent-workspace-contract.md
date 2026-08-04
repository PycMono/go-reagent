# General Agent Workspace Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make AGENTS.md, at least one eligible Skill, and confined read mandatory while replacing the coding-specific built-in prompt with general Agent runtime discipline.

**Architecture:** Preserve the current PromptComposer, SkillLoader, RunContextFactory, and Workspace boundaries. Make PromptComposer surface AGENTS.md errors, make RunContextFactory enforce the Skill/read invariants, and update CLI/test workspaces to satisfy the strict contract.

**Tech Stack:** Go 1.26, standard library testing, existing os.Root workspace protections, existing Skill parser and Tool Registry.

## Global Constraints

- Preserve the pre-existing staged deletion of `cmd/ping/main.go` and all unrelated user changes.
- Do not introduce optional Context contributors or business tools.
- Do not change structured Run, Session, persistence, or public package boundaries.
- Write a failing test and observe the expected failure before each production behavior change.

---

### Task 1: Generic Prompt and Mandatory AGENTS.md

**Files:**
- Modify: `internal/context/composer.go`
- Modify: `internal/context/composer_test.go`

- [ ] Add failing tests for missing/empty AGENTS.md and a prompt that contains service identity but no coding identity.
- [ ] Change `Build` to return an error, require a safe non-empty AGENTS.md, and inject it as authoritative instructions.
- [ ] Replace the coding core prompt with general runtime discipline.
- [ ] Run `go test ./internal/context -run 'TestPromptComposer' -count=1`.

### Task 2: Mandatory Skill and read Invariants

**Files:**
- Modify: `internal/context/run_context.go`
- Modify: `internal/context/run_context_test.go`

- [ ] Add failing tests for zero eligible Skills and missing read.
- [ ] Reject zero eligible Skills and require read for every prepared run.
- [ ] Propagate PromptComposer errors before returning a RunContext.
- [ ] Run `go test ./internal/context -count=1`.

### Task 3: Migrate Test and Command Workspaces

**Files:**
- Modify: context/engine/integration tests that construct temporary workspaces.
- Create: `AGENTS.md`
- Create: `skills/repository-development/SKILL.md`
- Modify: `README.md`

- [ ] Add reusable test helpers that create AGENTS.md and one eligible Skill.
- [ ] Ensure Registry definitions include read where RunContextFactory is exercised.
- [ ] Add a service-Agent integration case and preserve Context/History/Input ordering.
- [ ] Document the mandatory Workspace layout and optional coding tools.
- [ ] Run focused context/engine/app tests.

### Task 4: Verification

- [ ] Run `gofmt` on feature-owned Go files.
- [ ] Run `go test -race ./internal/context ./internal/engine ./internal/app -count=1`.
- [ ] Run `go test ./... -count=1` and distinguish feature failures from unrelated concurrent edits.
- [ ] Run path-scoped `git diff --check` and audit that `cmd/ping/main.go` remains untouched.
