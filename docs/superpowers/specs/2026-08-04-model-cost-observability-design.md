# Model Cost Observability Design

## Goal

Add mandatory, durable, per-call LLM token, cost, and latency observability without persisting hidden thinking text or duplicating complete model request snapshots.

The implementation must preserve the current conversation contract: customer messages, visible assistant messages, tool calls, and tool results remain conversation data; model invocations form a separate billing ledger.

## Confirmed Product Decisions

- All prices and calculated costs use USD.
- Each platform configures its own input and output price in USD per one million tokens.
- Customer input means the original message submitted for a conversation turn. The system prompt, accumulated history, tool definitions, and other complete provider request snapshots are not persisted again.
- Visible Action responses remain persisted as assistant messages.
- Every successful provider call accepted by the Agent loop must have valid Usage, a calculated cost, and exactly one invocation record. This includes Thinking calls and every repeated Action call in tool loops.
- A provider response with missing or invalid Usage is not accepted as a successful loop response. The tracker returns an explicit generation error so the run cannot silently continue without a trustworthy cost record.
- Thinking invocation records contain metrics and a phase label only. Hidden thinking text is never added to `RunResult`, Reporter events, or durable storage.
- The invocation ledger is the authoritative source for usage and cost aggregation. A visible assistant message also retains the usage of the provider call that created it for convenient inspection.

## Existing Architecture Constraints

The repository supports both OpenAI-compatible Chat Completions and Anthropic Messages providers through the public `ai.Client` contract. `agent.Loop` can issue multiple provider requests for one customer turn, and optional Thinking responses are deliberately excluded from `agent.RunResult.NewMessages`.

When conversation persistence is enabled, `internal/cli/conversation.Runner` appends the customer input and newly generated visible messages through its Store. The MySQL implementation serializes the complete `ai.Message` as a JSON payload. Adding optional usage metadata to `ai.Message` therefore remains backward compatible with existing stored payloads and automatically preserves visible Action usage.

The current `master` moved public model contracts into `ai`, the runtime into `agent`, default construction into `internal/bootstrap`, and bundled persistence into `internal/cli`. The migration must preserve those package boundaries and must not restore the removed `internal/schema`, `internal/provider`, `internal/engine`, or `internal/conversation` packages.

## Architecture

The feature has four focused layers:

1. Provider adapters under `ai/providers` normalize vendor-native token counts into `ai.Usage`.
2. `internal/observability.CostTracker` decorates the configured `ai.Client`, measures request latency, validates Usage, calculates USD cost using the selected platform's pricing, enriches successful response messages, and emits structured logs. Missing or invalid Usage becomes an error.
3. `agent.Loop` labels each accepted call as Thinking or Action and appends an ordered `agent.ModelInvocation` entry to the run result. It never promotes Thinking content into visible messages.
4. Conversation persistence writes the turn messages and invocation ledger entries in the same transaction.

This avoids an in-memory Session accumulator. The current runtime is a stateless one-shot API, while the MySQL conversation store is already the durable ownership and transaction boundary.

## Schema Contracts

### Usage

`ai.Message` gains an optional `Usage *Usage` field. `ai.Usage` contains:

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

An accepted loop response must have non-negative token counts, non-empty platform and model identifiers, finite non-negative prices and cost, and non-negative latency. Zero token counts, zero prices, zero cost, and zero-millisecond latency remain valid. This lets the loop reject a raw or partially enriched Usage value while still supporting free models.

### Model invocation

`agent.ModelInvocation` is the durable per-call record returned by the Agent. It contains:

- `Sequence uint32`, equal to the provider-call ordinal within a run, starting at 1
- `Phase ModelInvocationPhase`, restricted to `thinking` or `action`
- all fields from the enriched `Usage` value

`agent.RunResult` gains `Invocations []ModelInvocation`. The slice includes every successful provider call accepted by the loop, including Thinking calls and repeated Action calls. It remains populated when a later provider or tool operation fails, just as `NewMessages` retains already completed visible work.

The Agent copies values into invocation records. It does not retain pointers into response messages.

## Configuration

Each public `ai.PlatformConfig` gains a required nested pricing block, also exposed through the root `reagent.PlatformConfig` alias:

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

A provider sets `Message.Usage` whenever its response contains a Usage object, including an explicit zero-token Usage object. Presence must be determined from SDK response metadata where available, rather than by testing whether token counts are greater than zero. If a compatible upstream omits Usage, the adapter may return the otherwise valid message with nil Usage, but the mandatory tracker rejects it before `agent.Loop` can accept it.

Negative token counts are retained long enough for the tracker to distinguish invalid Usage from missing Usage. The tracker emits `usage_invalid` and returns an error. No invalid response enters loop history, visible messages, invocation results, or durable storage.

## Cost Tracker

`internal/observability/tracker.go` defines a provider decorator and an immutable pricing value. Construction receives:

- the next `ai.Client`
- platform ID
- model name
- input and output USD prices per one million tokens

`Generate` measures elapsed time around exactly one delegated provider call. On success with valid usage it calculates:

```text
cost_usd = (input_tokens × input_price + output_tokens × output_price) / 1,000,000
```

It enriches the returned message and emits one structured success log with component, platform, model, input/output tokens, price snapshot, cost USD, and latency milliseconds. On success without Usage it emits `usage_missing` and returns an explicit error. On invalid Usage it emits `usage_invalid` and returns an explicit error. On provider failure it emits a structured failure log with latency and the sanitized error already returned by the provider, then returns the original response and error unchanged.

The tracker does not own a Session, database connection, mutable aggregate, or phase detection. This keeps it concurrency-safe and reusable. `agent.Loop` owns phase and sequence because only the loop knows why a provider call was made.

`internal/bootstrap` decorates the configured `ai.Client` with `internal/observability.CostTracker` after `ai/providers` constructs the vendor client and before `agent.Loop` consumes it. This keeps package dependencies one-way (`internal/observability` may import `ai`; `ai` never imports an internal package) and avoids an import cycle. The Agent remains unaware of the concrete provider and pricing source.

## Agent Loop Data Flow

For each provider call, the loop performs these steps:

1. Call the tracked provider.
2. Require the tracked client to return valid, fully enriched Usage. Missing platform/model identity, missing or invalid Usage, or non-finite/negative metrics returns an error and terminates the run.
3. Validate the returned message as it does today.
4. Append exactly one copied invocation record using the current call ordinal and explicit current phase.
5. For Thinking, use the response only in in-memory context and exclude its content from new messages and reporter events.
6. For Action, append the response to context and `NewMessages` as today, retaining its Usage.

Invocation sequence follows provider-call order, not tool-call or message ordinal. Every accepted call has one invocation. A provider error or rejected unmetered response terminates the run and may leave a final sequence ordinal without a ledger row, but no later call is allowed to continue past that gap.

An internal loop result contains both `NewMessages` and `Invocations`, allowing `agent.Agent.Run` to construct `agent.RunResult` without global state or context-value collectors. The existing public `Loop.Run` signature remains source-compatible and returns messages only; `Agent.Run` uses the internal detailed execution path to expose invocations.

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

`internal/cli/conversation.AppendRequest` gains `Invocations`. Its Runner passes `agent.RunResult.Invocations` alongside messages. The MySQL store validates phase, non-negative counts/prices/cost/latency, sequence ordering, and finite floating-point values before opening its transaction.

The existing optimistic conversation version update, message insertion, and invocation insertion execute in one transaction. A failure in any insert rolls the whole turn back. Existing `AppendTurn` callers that provide messages and no invocations remain valid. An invocation-only append is not introduced: a run with no completed message remains a failed, non-persisted turn, while its failed request latency is available in logs.

Database `DECIMAL` values are encoded from stable base-10 strings and decoded without routing through binary floating-point database columns. Application-facing values remain `float64`, matching the configuration and log APIs, while persisted decimal scale prevents database aggregation drift.

## Message Persistence and Privacy

Customer inputs and visible Action outputs continue to be persisted exactly once as conversation messages. Tool calls and tool results keep their existing behavior. The implementation must not add any of the following to durable storage:

- composed system prompts
- full accumulated provider request history snapshots
- available tool definitions
- Thinking response content
- API keys or authorization headers

Visible assistant message payloads may contain their own `usage` object. This duplicates the corresponding Action ledger metrics intentionally for local message inspection; all totals and billing reports must aggregate `agent_model_invocations`, never both sources.

## Failure Semantics

- A provider error has no usage or cost record because no trustworthy billing response exists. The tracker logs failure latency and the run terminates.
- A provider response without Usage is rejected with an explicit generation error after emitting `usage_missing`; it is not added to loop history or returned as a visible response.
- A provider response with negative tokens or otherwise invalid Usage is rejected with an explicit generation error after emitting `usage_invalid`.
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

- `ai.Message` Usage JSON round trips and omission when nil outside accepted loop responses.
- OpenAI-compatible Usage presence and prompt/completion token mapping.
- Anthropic Usage presence and input/output token mapping.
- Missing and invalid Usage terminate the run before the response is accepted.
- Tracker price calculation, free pricing, latency population, delegated errors, and concurrency independence.
- Platform pricing normalization and validation, including missing, negative, NaN, and infinite values.
- Agent invocation phase labels and sequence across Thinking and repeated Action calls.
- Every accepted loop call has exactly one invocation with a calculated cost; direct `agent` callers supplying an unmetered custom `ai.Client` receive an error rather than an uncosted response.
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
- Estimating token usage when the upstream does not return Usage.
- Mutable session total fields.
- A dashboard, billing API, or dedicated cost-report command.
