# Model Cost Observability Design

## Goal

Add durable, per-call LLM token, cost, and latency observability without persisting hidden thinking text or duplicating complete model request snapshots.

The implementation must preserve the current conversation contract: customer messages, visible assistant messages, tool calls, and tool results remain conversation data; model invocations form a separate billing ledger.

## Confirmed Product Decisions

- All prices and calculated costs use USD.
- Each platform configures its own input and output price in USD per one million tokens.
- Customer input means the original message submitted for a conversation turn. The system prompt, accumulated history, tool definitions, and other complete provider request snapshots are not persisted again.
- Visible Action responses remain persisted as assistant messages.
- Every successfully metered provider call produces one invocation record, including Thinking calls and repeated Action calls in tool loops. A successful response whose upstream omitted Usage remains usable but cannot produce a trustworthy billing row.
- Thinking invocation records contain metrics and a phase label only. Hidden thinking text is never added to `RunResult`, Reporter events, or durable storage.
- The invocation ledger is the authoritative source for usage and cost aggregation. A visible assistant message also retains the usage of the provider call that created it for convenient inspection.

## Existing Architecture Constraints

The repository supports both OpenAI-compatible Chat Completions and Anthropic Messages providers through `provider.LLMProvider`. The engine can issue multiple provider requests for one customer turn, and optional Thinking responses are deliberately excluded from `RunResult.NewMessages`.

When conversation persistence is enabled, `conversation.Runner` appends the customer input and newly generated visible messages through `conversation.Store`. The MySQL implementation serializes the complete `schema.Message` as a JSON payload. Adding optional usage metadata to `schema.Message` therefore remains backward compatible with existing stored payloads and automatically preserves visible Action usage.

The working tree already contains unrelated edits in conversation and run-context files. Implementation must preserve those edits and apply only focused changes needed by this feature.

## Architecture

The feature has four focused layers:

1. Provider adapters normalize vendor-native token counts into `schema.Usage`.
2. `observability.CostTracker` decorates the configured provider, measures request latency, calculates USD cost using the selected platform's pricing, enriches successful response messages, and emits structured logs.
3. The engine labels each successful call as Thinking or Action and appends an ordered `schema.ModelInvocation` entry to the run result. It never promotes Thinking content into visible messages.
4. Conversation persistence writes the turn messages and invocation ledger entries in the same transaction.

This avoids an in-memory Session accumulator. The current runtime is a stateless one-shot API, while the MySQL conversation store is already the durable ownership and transaction boundary.

## Schema Contracts

### Usage

`schema.Message` gains an optional `Usage *Usage` field. `Usage` contains:

- `InputTokens int64`
- `OutputTokens int64`
- `InputPriceUSDPerMillionTokens float64`
- `OutputPriceUSDPerMillionTokens float64`
- `CostUSD float64`
- `LatencyMS int64`
- `PlatformID string`
- `Model string`

JSON field names use snake case and include explicit units, for example `input_tokens`, `cost_usd`, and `latency_ms`. `omitempty` applies only to the `Message.Usage` pointer, not to fields inside a present usage value; zero is meaningful for a free model or a sub-millisecond call.

The provider adapters initially set only normalized input and output token counts. The tracker fills platform, model, price snapshot, cost, and latency. Token values use `int64` to match both provider SDKs and avoid architecture-dependent narrowing.

### Model invocation

`schema.ModelInvocation` is the durable per-call record returned by the engine. It contains:

- `Sequence uint32`, equal to the provider-call ordinal within a run, starting at 1
- `Phase ModelInvocationPhase`, restricted to `thinking` or `action`
- all fields from the enriched `Usage` value

`schema.RunResult` gains `Invocations []ModelInvocation`. The slice includes every successful provider call with usage returned by the provider, including Thinking calls. It remains populated when a later provider or tool operation fails, just as `NewMessages` retains already completed visible work.

The engine copies values into invocation records. It does not retain pointers into response messages.

## Configuration

Each `config.PlatformConfig` gains a required nested pricing block:

```json
{
  "id": "zhipu",
  "protocol": "openai",
  "baseURL": "https://open.bigmodel.cn/api/paas/v4/",
  "apiKey": "replace-me",
  "model": "glm-4.5-air",
  "pricing": {
    "input_usd_per_million_tokens": 0.15,
    "output_usd_per_million_tokens": 0.15
  }
}
```

Both prices must be finite and non-negative. Zero is valid for a free or internally hosted model. A missing pricing block is invalid so an unknown price cannot silently become a zero-dollar charge. Validation applies to every configured platform, consistent with existing platform validation.

`config.example.json`, configuration tests, and relevant documentation must show the new required block.

## Provider Mapping

The OpenAI-compatible adapter maps `response.Usage.PromptTokens` to `InputTokens` and `response.Usage.CompletionTokens` to `OutputTokens`.

The Anthropic adapter maps `response.Usage.InputTokens` to `InputTokens` and `response.Usage.OutputTokens` to `OutputTokens`. Anthropic's documented `InputTokens` is used directly; cache creation and cache read breakdowns are outside this feature's initial scope and are not added to the total a second time.

A provider sets `Message.Usage` when its response contains a valid Usage object, including an explicit zero-token Usage object. Presence must be determined from the SDK response metadata where available, rather than by testing whether token counts are greater than zero. If a compatible upstream omits Usage entirely, the response remains usable and `Message.Usage` remains nil.

Negative token counts are treated as invalid provider data: the response continues through normal message validation without usage, and the tracker emits a structured `usage_invalid` warning. No negative ledger values are persisted.

## Cost Tracker

`internal/observability/tracker.go` defines a provider decorator and an immutable pricing value. Construction receives:

- the next `provider.LLMProvider`
- platform ID
- model name
- input and output USD prices per one million tokens

`Generate` measures elapsed time around exactly one delegated provider call. On success with valid usage it calculates:

```text
cost_usd = (input_tokens × input_price + output_tokens × output_price) / 1,000,000
```

It enriches the returned message and emits one structured success log with component, platform, model, input/output tokens, price snapshot, cost USD, and latency milliseconds. On success without Usage it emits `usage_missing`. On invalid Usage it emits `usage_invalid`. On provider failure it emits a structured failure log with latency and the sanitized error already returned by the provider, then returns the original response and error unchanged.

The tracker does not own a Session, database connection, mutable aggregate, or phase detection. This keeps it concurrency-safe and reusable. The engine owns phase and sequence because only the engine knows why a provider call was made.

The Fx provider constructor wraps the real configured provider before exposing it as `provider.LLMProvider`. The rest of the engine remains unaware of the concrete provider and pricing source.

## Engine Data Flow

For each provider call, the engine performs these steps:

1. Call the tracked provider.
2. Validate the returned message as it does today.
3. If `Message.Usage` is present, append a copied invocation record using the current call ordinal and explicit current phase.
4. For Thinking, use the response only in in-memory context and exclude its content from new messages and reporter events.
5. For Action, append the response to context and `NewMessages` as today, retaining its optional Usage.

Invocation sequence follows provider-call order, not tool-call or message ordinal. A failed call or a successful call without Usage can therefore leave a sequence gap. Such calls do not create fabricated zero-value invocations; tracker logs are the observable indication that billing data is unavailable.

The internal loop result becomes a focused structure containing both `NewMessages` and `Invocations`, allowing `runtime.Run` to construct `schema.RunResult` without global state or context-value collectors.

## Persistence

A second migration creates `agent_model_invocations` with:

- primary key `id`
- `conversation_pk`
- `turn_version`
- nullable `run_id`, following the current message persistence convention
- `sequence`
- `phase`
- `platform_id`
- `model`
- `input_tokens`
- `output_tokens`
- `input_price_usd_per_million_tokens DECIMAL(20,12)`
- `output_price_usd_per_million_tokens DECIMAL(20,12)`
- `cost_usd DECIMAL(20,12)`
- `latency_ms`
- `created_at`

The migration adds a foreign key to `agent_conversations`, an index for conversation-time queries, and a uniqueness constraint on `(conversation_pk, turn_version, sequence)`. `turn_version` is assigned by the store from the same optimistic version increment used for message rows, so duplicate invocation writes are rejected even when the optional caller-owned `run_id` is empty.

`conversation.AppendRequest` gains `Invocations`. `conversation.Runner` passes `runtimeResult.Invocations` alongside messages. The MySQL store validates phase, non-negative counts/prices/cost/latency, sequence ordering, and finite floating-point values before opening its transaction.

The existing optimistic conversation version update, message insertion, and invocation insertion execute in one transaction. A failure in any insert rolls the whole turn back. Existing `AppendTurn` callers that provide messages and no invocations remain valid. An invocation-only append is not introduced: a run with no completed message remains a failed, non-persisted turn, while its failed request latency is available in logs.

Database `DECIMAL` values are encoded from stable base-10 strings and decoded without routing through binary floating-point database columns. Application-facing schema values remain `float64`, matching the configuration and log APIs, while persisted decimal scale prevents database aggregation drift.

## Message Persistence and Privacy

Customer inputs and visible Action outputs continue to be persisted exactly once as conversation messages. Tool calls and tool results keep their existing behavior. The implementation must not add any of the following to durable storage:

- composed system prompts
- full accumulated provider request history snapshots
- available tool definitions
- Thinking response content
- API keys or authorization headers

Visible assistant message payloads may contain their own `usage` object. This duplicates the corresponding Action ledger metrics intentionally for local message inspection; all totals and billing reports must aggregate `agent_model_invocations`, never both sources.

## Failure Semantics

- A provider error has no usage or cost record because no trustworthy billing response exists. The tracker logs failure latency.
- A successful response without Usage remains a valid model response; it produces no invocation row and emits `usage_missing`.
- Successfully observed calls completed before a later run failure remain in `RunResult.Invocations`. If the run also contains completed messages, the current partial-turn persistence behavior stores those messages and invocation rows together.
- Configuration rejects absent, negative, NaN, or infinite prices at startup.
- Persistence rejects malformed invocation data before database writes.
- Observability logging failures must not change provider results or crash the run; logging uses the repository's existing logger SDK.

## Query and Aggregation Contract

Per-run or per-conversation totals are computed from the invocation table:

```text
SUM(input_tokens)
SUM(output_tokens)
SUM(cost_usd)
SUM(latency_ms)
```

Grouping by `phase`, `platform_id`, or `model` supports Thinking-vs-Action and model-level analysis. No separate mutable total columns are added to `agent_conversations`; avoiding cached aggregates prevents drift and keeps the invocation ledger auditable.

This feature does not add a reporting CLI or HTTP endpoint. Structured logs, `RunResult.Invocations`, persisted message payloads, and the queryable invocation table are the supported observability outputs for the initial version.

## Testing Strategy

Tests follow red-green-refactor cycles and cover:

- `schema.Message` Usage JSON round trips and omission when nil.
- OpenAI-compatible Usage presence and prompt/completion token mapping.
- Anthropic Usage presence and input/output token mapping.
- Missing and invalid Usage behavior.
- Tracker price calculation, free pricing, latency population, delegated errors, and concurrency independence.
- Platform pricing normalization and validation, including missing, negative, NaN, and infinite values.
- Engine invocation phase labels and sequence across Thinking and repeated Action calls.
- Thinking content remains absent from `NewMessages`, reporter message events, and persisted message payloads.
- `RunResult` retains completed invocation records on later failures.
- Conversation cloning preserves Usage without pointer aliasing.
- MySQL message codec round-trips visible Action Usage.
- MySQL invocation validation, decimal encoding, ordering, duplicate protection, and transaction rollback.
- Integration wiring exposes a tracked provider and preserves the existing Fx dependency graph.
- Existing configurations and fixtures are updated with explicit pricing, followed by the full Go test suite and formatting/static checks.

## Out of Scope

- Persisting complete provider requests or hidden reasoning text.
- Streaming token usage.
- Provider-specific cache-token price tiers or reasoning-token price tiers.
- Currency conversion or CNY reporting.
- Retry-attempt billing when the upstream does not return Usage.
- Mutable session total fields.
- A dashboard, billing API, or dedicated cost-report command.
