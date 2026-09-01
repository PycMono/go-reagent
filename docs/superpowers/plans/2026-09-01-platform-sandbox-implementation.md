# Platform Sandbox Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the existing configurable sandbox implementation with a zero-config platform-selected runner in symlinks Go code.

**Architecture:** `pi/harness/sandbox` owns the shared Runner contract plus Host, Seatbelt and Bubblewrap implementations; the go-reagent composition root decorates the SDK HostRunner on every CLI/server startup. Network is always allowed; host passthrough and extra mounts are not exposed.

**Tech Stack:** Go 1.26, `os/exec`, Fx, Seatbelt, Bubblewrap.

**Spec:** `docs/superpowers/specs/2026-08-31-sandbox-command-runner-design.md`

## Global Constraints

- darwin selects Seatbelt, linux selects Bubblewrap, windows selects Host.
- Network is fixed to `allow`.
- No sandbox configuration, host environment passthrough, or extra mounts.
- Missing darwin/linux backend support fails startup; no Host fallback.
- Preserve unrelated worktree changes and the existing Host behavior.

---

### Task 1: Remove sandbox configuration

**Files:**
- Delete: `config/sandbox.go`
- Modify: `config/config.go`
- Modify: `config/validate.go`
- Test: `config/config_test.go`

**Interfaces:**
- Produces: config loading with no sandbox fields or normalization.

- [ ] Add/update config tests proving existing configuration loads without sandbox behavior.
- [ ] Run `go test ./config -count=1` and confirm the old configurable expectations fail.
- [ ] Delete `SandboxConfig` and its validation/cwd hooks.
- [ ] Run `go test ./config -count=1` and confirm it passes.

### Task 2: Implement fixed platform runners

**Files:**
- Modify: `pi/harness/sandbox/runner.go`
- Modify: `pi/harness/sandbox/bubblewrap.go`
- Modify: `pi/harness/sandbox/seatbelt.go`
- Create: `pi/harness/sandbox/platform.go`
- Test: `pi/harness/sandbox/runner_test.go`
- Test: `pi/harness/sandbox/sandbox_test.go`
- Test: `pi/harness/sandbox/runner_test.go`

**Interfaces:**
- Produces: `sandbox.NewRunner(workspaceRoot string) (sandbox.Runner, error)`.
- Produces: `NewBubblewrapRunner(path, root string)` and `NewSeatbeltRunner(path, root string)` fixed-policy constructors.

- [ ] Write tests for fixed `allow`, no `--unshare-net`, no extra mounts, duplicate env rejection, and platform selection seams.
- [ ] Run package tests and confirm the new tests fail against configurable constructors.
- [ ] Simplify both backend constructors and add `NewRunner`.
- [ ] Run `go test ./pi/harness/sandbox -count=1` and confirm it passes.

### Task 3: Wire fixed policy through tools and MCP

**Files:**
- Modify: `pi/harness/tools/process_supervisor.go`
- Modify: `infrastructure/driver/mcp/mcp.go`
- Test: `pi/harness/tools/process_supervisor_test.go`
- Test: `infrastructure/driver/mcp/mcp_test.go`

**Interfaces:**
- Consumes: `sandbox.Runner` and fixed sandbox payload environment helpers.
- Produces: exec and stdio MCP commands using the same effective runner.

- [ ] Add tests proving sandbox exec does not inherit host variables and MCP uses only explicit server env.
- [ ] Run scoped tests and confirm failures expose current HostPayloadEnv/configurable assumptions.
- [ ] Select payload environment from `runner.Policy()` and remove passthrough/mount handling.
- [ ] Run scoped tests and confirm they pass.

### Task 4: Wire CLI/server startup and verify

**Files:**
- Modify: `infrastructure/driver/sandbox/driver.go`
- Modify: `cmd/cli/main.go`
- Modify: `cmd/server/app.go`
- Test: `infrastructure/driver/sandbox/driver_test.go`
- Test: `cmd/cli/main_test.go`

**Interfaces:**
- Consumes: `pi.WorkDir` and `sandbox.NewRunner`.
- Produces: unconditional platform sandbox decoration for CLI config/env modes and server.

- [ ] Add tests for zero-config driver construction and CLI env-mode registration.
- [ ] Run scoped tests and confirm they fail against the config-dependent driver.
- [ ] Remove `*config.Config` from the sandbox driver and register it unconditionally.
- [ ] Run `go test ./cmd/cli ./infrastructure/driver/sandbox ./infrastructure/driver/mcp ./pi/harness/... ./pi/mcp -count=1`.
- [ ] Run `go test ./... -count=1`, `go test -race ./...`, `go vet ./...`, and `git diff --check`; report any unrelated baseline failure separately.
