# Enterprise WeCom Reporter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send every Agent lifecycle event defined by `bot.md` to an enterprise WeChat group robot while preserving current terminal output and model/tool behavior.

**Architecture:** `AgentEngine.Run` accepts a run-scoped `engine.Reporter` and emits the four lifecycle callbacks without storing channel state on the shared engine. `TerminalReporter` and `MultiReporter` stay in `engine`; `dispatch.WeComReporter` translates callbacks to one Markdown Webhook request per event. Configor loads the optional address from `bot.wecom.webhookURL`.

**Tech Stack:** Go 1.26, standard `net/http`, Configor, `go-logger-sdk`, `httptest`, Go race detector.

## Global Constraints

- Do not aggregate `OnThinking`, `OnToolCall`, `OnToolResult`, or `OnMessage` events.
- Do not add an inbound HTTP callback server or receive enterprise WeChat messages.
- Do not change the configured model, Provider implementations, Tool Registry behavior, or `go-logger-sdk`.
- Never commit or log the real enterprise WeChat Webhook URL.
- Store the real URL only in ignored `config.json`; keep `config.example.json` empty.
- Limit each Markdown message to 4096 bytes at a valid UTF-8 boundary.
- Reporter delivery failures are logged without the URL and do not terminate the Agent Run.

---

### Task 1: Reporter contract and Engine lifecycle events

**Files:**
- Create: `internal/engine/reporter.go`
- Create: `internal/engine/reporter_test.go`
- Create: `internal/engine/terminal_reporter.go`
- Modify: `internal/engine/loop.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `cmd/reagent/main.go`

**Interfaces:**
- Produces: `Reporter` with `OnThinking(context.Context)`, `OnToolCall(context.Context, string, string)`, `OnToolResult(context.Context, string, string, bool)`, and `OnMessage(context.Context, string)`.
- Produces: `NewTerminalReporter() Reporter` and `NewMultiReporter(...Reporter) Reporter`.
- Changes: `(*AgentEngine).Run(context.Context, string, Reporter) error`.

- [ ] **Step 1: Write failing Reporter fan-out and Engine lifecycle tests**

Add thread-safe recording reporters. Verify `MultiReporter` forwards each method exactly once to every child. Add an Engine test whose fake Provider performs Thinking, one tool call, a tool result, then a final response; assert every callback and its arguments.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/engine`

Expected: FAIL because `Reporter`, `NewMultiReporter`, and the new `Run` signature do not exist.

- [ ] **Step 3: Implement Reporter types and callback placement**

Define the four-method interface. `MultiReporter` filters nil reporters and forwards synchronously in registration order. `TerminalReporter` reproduces the current Thinking and Action text output; tool callbacks remain no-ops because structured Engine logs already display them.

Call `OnThinking` immediately before the Thinking Provider request, `OnMessage` for every non-empty Action content, `OnToolCall` immediately before Registry execution, and `OnToolResult` immediately after it. Pass the run-scoped Reporter through serial and parallel execution helpers rather than storing it on `AgentEngine`.

- [ ] **Step 4: Update existing Run call sites and verify tests**

Pass `nil` from tests that do not inspect reporting. Temporarily pass `NewTerminalReporter()` from `cmd/reagent/main.go` until configuration wiring is added.

Run: `go test ./internal/engine ./cmd/reagent`

Expected: PASS.

- [ ] **Step 5: Commit Reporter core**

```bash
git add internal/engine/reporter.go internal/engine/reporter_test.go internal/engine/terminal_reporter.go internal/engine/loop.go internal/engine/loop_test.go cmd/reagent/main.go
git commit -m "feat: expose agent lifecycle reporter"
```

### Task 2: WeCom configuration and outbound Reporter

**Files:**
- Create: `internal/dispatch/wecom.go`
- Create: `internal/dispatch/wecom_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config.example.json`

**Interfaces:**
- Produces: `config.BotConfig`, containing `WeCom WeComConfig`, containing optional `WebhookURL string` mapped from `bot.wecom.webhookURL`.
- Produces: `dispatch.NewWeComReporter(webhookURL string, client *http.Client) (*WeComReporter, error)`.
- `WeComReporter` satisfies `engine.Reporter` and sends one `msgtype=markdown` request per callback.

- [ ] **Step 1: Write failing configuration tests**

Verify missing/empty `bot.wecom.webhookURL` is allowed, surrounding whitespace is removed, an HTTPS address is accepted, and non-HTTPS or hostless addresses are rejected without including the full credential in the error.

- [ ] **Step 2: Implement the optional nested configuration**

Add JSON/YAML/TOML tags for `bot`, `wecom`, and `webhookURL`. Normalize and validate the optional URL during `Config.normalizeAndValidate`. Add an empty example:

```json
"bot": {
  "wecom": {
    "webhookURL": ""
  }
}
```

- [ ] **Step 3: Write failing WeCom Reporter protocol tests**

Use `httptest.Server` to capture requests. Invoke all four methods and assert four POST requests, `Content-Type: application/json`, `msgtype: markdown`, correct tool/error formatting, and no aggregation. Add tests for HTTP failure, business `errcode != 0`, UTF-8-safe 4096-byte truncation, and concurrent callback safety.

- [ ] **Step 4: Implement minimal WeCom Reporter**

Use a shared concurrency-safe `http.Client`. Encode requests with `encoding/json`; decode the enterprise WeChat `{errcode, errmsg}` response; always close the body. Each public callback calls a private `sendMarkdown`, and logs a sanitized error through `go-logger-sdk` without recording the Webhook URL or full message content.

- [ ] **Step 5: Run focused tests and the race detector**

Run: `go test -race ./internal/config ./internal/dispatch`

Expected: PASS with no races.

- [ ] **Step 6: Commit configuration and WeCom Reporter**

```bash
git add internal/config/config.go internal/config/config_test.go internal/dispatch/wecom.go internal/dispatch/wecom_test.go config.example.json
git commit -m "feat: add wecom group reporter"
```

### Task 3: Bootstrap wiring, local secret, and documentation

**Files:**
- Modify: `cmd/reagent/main.go`
- Modify: `cmd/reagent/main_test.go`
- Modify: `README.md`
- Modify locally only: `config.json`

**Interfaces:**
- Produces: `reporterFromConfig(config.BotConfig) (engine.Reporter, error)` returning Terminal-only when empty and Terminal+WeCom when configured.
- `providerFromConfig` additionally returns the loaded bot configuration so the file is loaded once.

- [ ] **Step 1: Write failing bootstrap tests**

Verify empty configuration creates a Terminal Reporter, configured Webhook creates a MultiReporter containing WeCom delivery, invalid construction returns a bootstrap error, and neither error nor logs reveal the configured URL.

- [ ] **Step 2: Wire Reporter construction into main**

Load model and bot configuration together. Build `TerminalReporter`, optionally build `WeComReporter`, combine with `MultiReporter`, and pass it to `eng.Run(ctx, prompt, reporter)`.

- [ ] **Step 3: Add the supplied address only to ignored local configuration**

Update local `config.json` under `bot.wecom.webhookURL`. Confirm with `git check-ignore config.json` and never stage this file.

- [ ] **Step 4: Update README**

Document the four unaggregated notifications, local configuration field, disabled-empty behavior, 4096-byte truncation, and the fact that this phase does not receive enterprise WeChat messages.

- [ ] **Step 5: Run full verification**

Run: `gofmt -w` on changed Go files.

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `git diff --check`

Expected: all commands succeed; `git status --short` does not show `config.json` and does not stage unrelated `.idea`, `bot.md`, or `img.png` changes.

- [ ] **Step 6: Commit bootstrap and documentation**

```bash
git add cmd/reagent/main.go cmd/reagent/main_test.go README.md
git commit -m "feat: send agent lifecycle to wecom group"
```
