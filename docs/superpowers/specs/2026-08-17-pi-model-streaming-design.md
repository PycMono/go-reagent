# Pi Model Streaming Design

## Goal

Replace the synchronous `ai.Provider.Generate` contract with a provider-neutral pull stream, expose Action text incrementally through Agent events, and carry those events through the existing HTTP SSE endpoint and chat page.

## Architecture

Model providers return an `ai.Stream` with normalized start, text-delta, tool-call-delta, done, and error events. OpenAI and Anthropic retain ownership of SDK-specific accumulation and expose a complete `ai.Message` through `Result()`. `Loop` consumes the normalized stream, publishes only Action text, validates the complete response before recording it or executing tools, and keeps Thinking and Compaction private.

Agent-facing events remain separate from model-facing events. The Chat service maps message start/update/end events to `message.started`, `message.delta`, and `message.completed` SSE events. The terminal renders deltas directly, Enterprise WeChat ignores deltas and sends only final responses, and the browser creates and updates one live assistant bubble per streamed Action message.

## Constraints

- Remove `Generate`; do not provide a compatibility layer.
- Do not expose OpenAI or Anthropic SDK event types outside their provider files.
- Never execute a tool until the provider stream has completed and the final ToolCalls pass validation.
- Action text is user-visible in real time, including text accompanying ToolCalls.
- Thinking and Compaction consume streams without publishing their content.
- Retry transient/rate-limit failures only when no Action text has been published.
- Do not add model-capacity configuration.
- Do not change database schemas or persist delta events.
- Preserve the existing Chinese compaction prompt changes in `pi/recovery.go`.

## Model Stream Contract

`ai.Provider.Stream(context.Context, []Message, []ToolDefinition) Stream` returns a pull stream with `Next`, `Current`, `Result`, and `Close`. The normalized stream emits start, text delta, tool-call delta, done, and error events. `Result` returns the complete message or the classified error after a terminal event.

`ai.Message` gains a provider-neutral finish reason. A length-truncated response containing ToolCalls is rejected before scheduling tools.

## Agent and HTTP Events

`pi.AgentEvent` exposes message start, update, and end events. The Chat SSE contract exposes corresponding `message.started`, `message.delta`, and `message.completed` events. A failed stream continues to use the existing run-level `run.failed` event and does not persist an incomplete assistant message.

## Persistence

Only complete assistant and tool messages remain eligible for persistence. Successful, fully metered model calls continue to produce `ModelInvocation`; failed calls without trustworthy Usage are logged but not inserted into the existing non-null invocation schema.

## Verification

Provider stream accumulation, Loop event order and retry behavior, transport rendering, SSE mapping, browser contract, cancellation, and complete repository tests must pass. The final checks are `go test ./...`, targeted race tests, and `git diff --check`.
