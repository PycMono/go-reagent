# Pi Model Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream model Action text through the existing HTTP SSE chat experience while preserving complete-message validation, tool safety, usage accounting, and hidden Thinking/Compaction.

**Architecture:** Replace `Provider.Generate` with a normalized pull stream modeled after Pi's two-layer event flow. Providers accumulate SDK chunks into a final `ai.Message`; Loop converts only Action text deltas into Agent events; the Chat service converts Agent events into SSE events consumed by the existing page.

**Tech Stack:** Go 1.x, openai-go/v3, anthropic-sdk-go, Gin SSE, vanilla JavaScript chat page.

**Spec:** `docs/superpowers/specs/2026-08-17-pi-model-streaming-design.md`

## Global Constraints

- No `Generate` compatibility layer.
- No database migration.
- No provider SDK types outside provider implementations.
- No tool execution before a successful terminal stream result.
- Preserve existing unrelated and compaction-prompt changes.

---

### Task 1: Define the model stream contract

**Files:**
- Modify: `pi/ai/provider.go`
- Modify: `pi/ai/message.go`
- Test: `pi/test/stream_test.go`

**Interfaces:**
- Produces: `ai.Provider.Stream`, `ai.Stream`, `ai.StreamEvent`, `ai.ToolCallDelta`, and `ai.FinishReason`.

- [ ] Write a compile-time behavioral test with a scripted pull stream that emits start, delta, and done and returns a complete assistant message.
- [ ] Run `go test ./pi/test -run TestModelStreamContract -count=1` and verify it fails because the stream API does not exist.
- [ ] Add the stream contract and finish-reason types, with no provider implementation yet.
- [ ] Run the targeted test and verify it passes.

### Task 2: Implement provider streams

**Files:**
- Modify: `pi/ai/providers/openai.go`
- Modify: `pi/ai/providers/anthropic.go`
- Test: `pi/test/provider_stream_test.go`

**Interfaces:**
- Consumes: `ai.Stream` and `ai.StreamEvent` from Task 1.
- Produces: OpenAI and Anthropic `Stream` implementations that accumulate complete messages and Usage.

- [ ] Add HTTP/SSE fixture tests proving text delta order, ToolCall argument accumulation, finish-reason mapping, final Usage, classified errors, and cancellation.
- [ ] Run the provider stream tests and verify failures because providers still expose `Generate`.
- [ ] Implement OpenAI streaming with `NewStreaming`, `ChatCompletionAccumulator`, and `IncludeUsage`.
- [ ] Implement Anthropic streaming with `NewStreaming` and `Message.Accumulate`.
- [ ] Run the provider stream tests and provider package tests.

### Task 3: Stream-aware cost tracking and recovery

**Files:**
- Modify: `pi/harness/observability/tracker.go`
- Modify: `pi/harness/observability/tracker_test.go`
- Modify: `pi/recovery.go`
- Modify: `pi/test/recovery_test.go`

**Interfaces:**
- Consumes: completed provider streams.
- Produces: metered stream results and retry/compaction behavior that knows whether Action text was published.

- [ ] Convert tracker tests to scripted streams and add a failing test proving the final result is enriched once while deltas remain unchanged.
- [ ] Add failing recovery tests for retry before publication, no retry after publication, hidden-stream retry, and context-overflow compaction.
- [ ] Implement the tracking stream wrapper and stream consumption helper.
- [ ] Replace `generateWithRetry` and `generate` calls with stream-aware versions while preserving the Chinese compaction prompt.
- [ ] Run tracker and recovery tests until green.

### Task 4: Convert Loop and Agent events

**Files:**
- Modify: `pi/event.go`
- Modify: `pi/loop.go`
- Modify: `pi/register_test.go`
- Modify: `pi/test/event_test.go`
- Modify: `pi/test/loop_test.go`
- Modify: `pi/test/agent_test.go`

**Interfaces:**
- Produces: `message_start`, `message_update`, and `message_end` Agent events.

- [ ] Add failing tests for message lifecycle order, streamed text accompanying ToolCalls, hidden Thinking, and tool execution after message end.
- [ ] Replace the final-only message event with start/update/end constructors.
- [ ] Consume Action streams through a delta reporter; consume Thinking streams privately; remove direct thinking output.
- [ ] Validate JSON ToolCall arguments and reject length-truncated ToolCalls before scheduling.
- [ ] Convert remaining test providers to the Stream contract and run all Pi tests.

### Task 5: Carry streaming through SSE and reporters

**Files:**
- Modify: `common/vo/chat.go`
- Modify: `application/service/chat/reporter.go`
- Modify: `application/service/chat/reporter_test.go`
- Modify: `transport/terminal.go`
- Modify: `transport/terminal_test.go`
- Modify: `transport/wecom.go`
- Modify: `transport/wecom_test.go`
- Modify: `infrastructure/controller/http/chat/controller_test.go`

**Interfaces:**
- Produces: `message.started`, `message.delta`, and `message.completed` HTTP SSE events.

- [ ] Add failing reporter and controller tests for the three ordered SSE events and complete final message.
- [ ] Map Agent message events to VO events; make message deltas non-droppable and context-cancelable.
- [ ] Render terminal deltas without duplicating the final response.
- [ ] Keep Enterprise WeChat final-only and ignore streamed intermediate ToolCall messages.
- [ ] Run application, transport, and HTTP controller tests.

### Task 6: Update the current chat page

**Files:**
- Modify: `frontend/static/js/pages/chat.js`
- Modify: `infrastructure/controller/http/page/controller_test.go`

**Interfaces:**
- Consumes: `message.started`, `message.delta`, and `message.completed` SSE events.

- [ ] Extend the page contract test to require handlers for all three message events and verify it fails.
- [ ] Add live assistant bubble creation, delta append, final-message reconciliation, and failed-run cleanup.
- [ ] Run the page controller tests and the HTTP chat tests.

### Task 7: Full migration and verification

**Files:**
- Modify: remaining test-only Provider implementations reported by `rg -n 'Generate\\(' --glob '*.go'`.

**Interfaces:**
- Produces: repository-wide use of `Provider.Stream` with no stale `Generate` call.

- [ ] Confirm `rg -n 'Generate\\(' pi application transport conversation --glob '*.go'` has no model-provider implementations or calls.
- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test ./...`.
- [ ] Run `go test -race ./pi/... ./application/... ./transport/...`.
- [ ] Run `git diff --check` and inspect the final diff for unrelated changes.
