# Structured Run Contract Design

## Goal

Upgrade the current one-shot `Run(context.Context, string) error` contract into a structured, stateless run contract without changing the package layout or introducing session persistence.

The runtime continues to own in-memory state for one run. The caller owns conversation history, current input, external context, business identifiers, and persistence across runs.

## Scope

This change introduces:

- a structured `RunRequest`;
- a `RunResult` containing messages created during the run;
- per-run reporter injection;
- composition of history, external context, and current user input;
- partial results when a run fails after producing messages.

This change does not introduce:

- public package migration;
- session, history, or memory storage;
- context compaction;
- token usage accounting;
- provider stop-reason mapping;
- run/event identifiers beyond the request `RunID`;
- changes to the existing coding tools, workspace, Fx bootstrap, or provider selection.

## Public Contract Within the Current Internal Package

The package layout remains internal for this iteration. Neutral request/result data types live in `internal/schema`, while `AgentRuntime` remains in `internal/engine`. This preserves the existing dependency direction: both `internal/context` and `internal/engine` can consume the contract without importing each other in a cycle.

```go
// internal/schema/run.go
type ContextBlock struct {
	Name     string
	Content  string
	Priority int
}

type RunRequest struct {
	RunID    string
	History  []schema.Message
	Input    schema.Message
	Context  []ContextBlock
	Metadata map[string]string
}

type RunResult struct {
	RunID       string
	NewMessages []schema.Message
}

// internal/engine/runtime.go
type AgentRuntime interface {
	Run(context.Context, schema.RunRequest, Reporter) (schema.RunResult, error)
}
```

`RunID` and `Metadata` are optional opaque caller values. The runtime does not interpret them. `RunID` is copied to every returned `RunResult`, including validation and execution failures. Metadata is accepted and copied into the prepared run context so later tool and event work can consume it without changing the request contract again; this iteration does not expose metadata to tools or events.

## Input Validation

Before prompt or skill discovery, the runtime validates:

- the Go context is non-nil and not already canceled;
- `Input.Role` is `schema.RoleUser`;
- `Input.Content` contains at least one non-empty supported content block;
- `Input` contains no tool calls, tool-call ID, or tool name;
- every context block has a non-empty name and non-empty content after trimming whitespace.

History remains provider-neutral. Existing provider conversion and action-response validation continue to reject structurally invalid history when applicable. The runtime clones top-level request slices and maps before use and does not mutate caller-owned values.

Message identity is not part of the current schema, so this iteration documents that `Input` must not also be included in `History` but does not attempt unreliable value-based duplicate detection.

## Context Assembly

`RunContextFactory` changes from receiving a plain user string to receiving the structured request plus the available tool definitions.

The effective model context is assembled in this order:

1. the existing core/workspace/skills system message;
2. external context blocks;
3. caller-supplied history;
4. the current input message.

Context blocks are stably ordered by descending `Priority`; blocks with the same priority retain caller order. Each block becomes a system message whose text is rendered as:

```text
# Context: <name>
<content>
```

The runtime does not infer business meaning from block names or content. The current input is appended exactly once and is never added to `RunResult.NewMessages`.

The existing skills-to-`read` dependency remains unchanged in this iteration.

## Run State and Result Semantics

`AgentLoop` continues to clone the prepared context into a local in-memory history. It additionally tracks only messages created by the action/tool loop:

- every assistant action message, including messages containing tool calls;
- every tool-result message;
- the final assistant response.

`NewMessages` excludes:

- the system prompt;
- external context messages;
- caller history;
- the current user input;
- the optional internal thinking response;
- the synthetic user instruction that transitions thinking into action.

This makes `NewMessages` safe for the caller to append to its conversation store without duplicating input context or persisting internal planning scaffolding.

The loop returns a value even when it also returns an error. If an error occurs after one or more action/tool messages have been created, those messages are preserved in `RunResult.NewMessages`. If validation or context preparation fails before execution, the result contains the request `RunID` and no new messages.

The runtime returns a newly allocated `NewMessages` slice and retains no message state after `Run` returns.

## Reporter Ownership

Reporter selection moves from `AgentRuntime` construction to each `Run` call:

```go
result, err := runtime.Run(ctx, request, reporter)
```

A nil reporter remains valid. The existing reporter event schema and failure-isolation behavior remain unchanged.

The Fx application keeps its current terminal and WeCom reporter composition. `AgentRunner` receives the composed reporter and passes it into `Run`, so command behavior remains the same while the runtime itself becomes safe to reuse with different reporters across concurrent calls.

## Error Behavior

The method follows normal Go partial-result semantics:

```go
result, err := runtime.Run(ctx, request, reporter)
```

- `err == nil` means the run reached a final assistant message without tool calls.
- `err != nil` means validation, preparation, provider generation, tool scheduling, or cancellation failed.
- `result.NewMessages` may be non-empty with `err != nil` and must describe all completed model/tool messages produced before the failure.
- cancellation remains represented by an error wrapping `context.Canceled` or `context.DeadlineExceeded` as it is today.

No persistence, retry, or resume behavior is implied by a partial result.

## Test Design

Tests will establish the new behavior before implementation:

1. Runtime validation rejects an invalid current input and returns the request `RunID`.
2. Context preparation assembles system, priority-ordered context, history, and input without mutating the request.
3. A direct assistant response is returned in `NewMessages`.
4. A tool-call assistant message, its tool result, and the final assistant message are returned in order.
5. Internal thinking scaffolding is not returned in `NewMessages`.
6. Provider or tool-loop failure returns messages already produced before the failure.
7. The reporter supplied to one run receives that run's events and is not stored on the shared runtime.
8. Existing app lifecycle tests verify that the composed Fx reporter is passed into the one-shot command run.
9. Existing provider, tool registry, scheduler, workspace, and integration tests remain green.

## Compatibility

This is intentionally a breaking internal API change. All current call sites and tests are updated in the same change. Because external projects cannot import these `internal` packages, no external Go module compatibility promise exists yet.

The later public-package migration can expose the validated contract without changing its core semantics.
