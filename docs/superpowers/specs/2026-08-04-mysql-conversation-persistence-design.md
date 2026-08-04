# MySQL Conversation Persistence Design

## Goal

Add internal multi-user, multi-conversation persistence to go-reagent while preserving the existing stateless `AgentRuntime` boundary. The new conversation layer loads a bounded history window, invokes one runtime run, and persists the current user input plus the messages produced by that run.

MySQL stores the complete conversation record. The initial implementation uses `github.com/PycMono/go-mysql-sdk` and keeps the existing one-shot CLI usable when persistence is disabled.

## Scope

This change introduces:

- an internal `ConversationRunner` that composes history loading, `AgentRuntime.Run`, and message persistence;
- a storage-neutral internal conversation store contract;
- a MySQL store implemented with `go-mysql-sdk`;
- full message retention with a configurable recent-message history window;
- multi-user ownership through `(user_id, conversation_id)`;
- optimistic version checks that reject concurrent writes instead of silently reordering them;
- optional CLI integration selected through configuration;
- explicit SQL migration files and focused unit, SQL-mock, and optional real-MySQL tests.

This change does not introduce:

- a public SDK or migration of the existing `internal` runtime API;
- HTTP, WebSocket, authentication, authorization, or tenant management;
- Redis, request queues, background workers, distributed locks, or automatic retries;
- summary generation, token-aware compaction, retention deletion, or archival;
- branching conversations or idempotent run replay.

The calling business service remains responsible for authenticating users and, until a later queue design is implemented, serializing requests for the same conversation. Different conversations may run concurrently.

## Architecture

The existing `internal/engine.AgentRuntime` remains stateless and unaware of persistence. A new package owns stateful conversation orchestration:

```text
AgentRunner
    | user ID, conversation ID, current input
    v
internal/conversation.Runner
    |-- Store.LoadWindow()
    |      `-- internal/conversation/mysql -> go-mysql-sdk -> MySQL
    |-- AgentRuntime.Run()
    |      `-- existing stateless engine
    `-- Store.AppendTurn()
           `-- optimistic version check and transactional insert
```

### `internal/conversation.Runner`

The runner:

1. validates the user ID, conversation ID, and current input;
2. loads or creates the owned conversation and reads a bounded history snapshot;
3. passes that history to the existing structured `schema.RunRequest`;
4. invokes the existing `engine.AgentRuntime` with the caller's Reporter;
5. persists the current user input and returned `NewMessages` as one ordered turn when persistence rules permit;
6. returns the original `schema.RunResult` and any runtime or persistence error.

It does not authenticate users, queue work, retry model calls, or interpret message content.

### `internal/conversation.Store`

The store interface is independent of GORM and MySQL. It exposes only conversation operations needed by the runner:

- load or create a conversation snapshot for `(userID, conversationID)`;
- load at most the configured safe history window;
- append one ordered turn against an expected version.

The snapshot contains an opaque internal conversation identifier, the current version, and decoded `[]schema.Message`. Domain errors include `ErrNotFound`, `ErrConflict`, and a distinguishable corrupt-message error.

### `internal/conversation/mysql`

The MySQL implementation accepts a `go-mysql-sdk` provider and its transaction manager. It owns GORM models, JSON encoding, query order, conditional version updates, and transaction rollback. Neither the runner nor the engine imports GORM.

### CLI adapter

When conversation persistence is enabled, `internal/app.AgentRunner` calls the conversation runner instead of calling `AgentRuntime` directly. The current prompt remains the user input. `AGENT_USER_ID` and `AGENT_CONVERSATION_ID` supply the identifiers for one CLI invocation.

When persistence is disabled, the existing one-shot stateless path remains unchanged and no MySQL connection is required.

## Data Model

The initial schema uses two tables.

### `agent_conversations`

| Column | Type | Rules |
|---|---|---|
| `id` | `BIGINT UNSIGNED` | auto-increment primary key |
| `user_id` | `VARCHAR(128)` | non-empty caller-owned user identifier |
| `conversation_id` | `VARCHAR(128)` | non-empty caller-owned conversation identifier |
| `version` | `BIGINT UNSIGNED` | non-null, default `0` |
| `created_at` | `DATETIME(6)` | non-null |
| `updated_at` | `DATETIME(6)` | non-null |

The table has a unique index on `(user_id, conversation_id)`. Every lookup is ownership-scoped by both values; one user cannot access another user's conversation merely by knowing its conversation ID.

### `agent_messages`

| Column | Type | Rules |
|---|---|---|
| `id` | `BIGINT UNSIGNED` | auto-increment primary key |
| `conversation_pk` | `BIGINT UNSIGNED` | references `agent_conversations.id` |
| `turn_version` | `BIGINT UNSIGNED` | conversation version assigned to this append |
| `ordinal` | `INT UNSIGNED` | zero-based position inside the turn |
| `run_id` | `VARCHAR(128)` | nullable opaque runtime identifier |
| `role` | `VARCHAR(32)` | derived from the encoded message for inspection |
| `payload` | `JSON` | complete serialized `schema.Message` |
| `created_at` | `DATETIME(6)` | non-null |

The table has:

- a unique index on `(conversation_pk, turn_version, ordinal)`;
- a history index on `(conversation_pk, turn_version, ordinal)`;
- a foreign key from `conversation_pk` to `agent_conversations.id`.

`payload` is the authoritative persisted representation. `role` is derived from the same message during insertion for operational inspection. Decode validates that the payload is structurally valid and that its role matches the stored role; a mismatch is treated as corrupted data rather than silently repaired.

## Conversation Creation

The first call for a `(userID, conversationID)` pair creates an empty conversation with version `0`. Concurrent creation relies on the unique ownership index: a duplicate-key loser reloads the winning row. An empty conversation row may remain when runtime execution fails before any message is produced; cleanup is outside this scope.

## History Window

MySQL retains every persisted message. `conversation.history_message_limit` controls only how many recent messages are supplied to the model and defaults to `100`.

The store queries the newest `limit + 1` messages ordered by `(turn_version DESC, ordinal DESC)`. It uses the extra row to detect whether the oldest selected turn was cut in half:

- if the extra row has a different `turn_version`, discard only the extra row;
- if the extra row has the same `turn_version` as the oldest selected row, discard every selected row from that incomplete oldest turn;
- reverse the remaining rows into ascending `(turn_version, ordinal)` order before returning them.

The returned window therefore never exceeds the configured limit and never starts in the middle of a persisted turn. In particular, an Assistant tool call and its Tool result cannot be separated by history truncation. If one turn alone exceeds the limit, that turn is omitted from the history window rather than exceeding the bound.

This iteration does not estimate tokens or generate summaries.

## Append Transaction and Optimistic Concurrency

The snapshot version loaded before runtime execution is the expected version for persistence. The user input is ordinal `0`; `RunResult.NewMessages` occupy ordinals `1..N`. Every message in the append receives `turn_version = expectedVersion + 1`.

One `go-mysql-sdk` transaction performs:

```sql
UPDATE agent_conversations
SET version = version + 1, updated_at = CURRENT_TIMESTAMP(6)
WHERE id = ? AND version = ?;
```

The transaction requires exactly one affected row. Zero affected rows returns `ErrConflict`; the store does not retry or reorder the turn. After a successful conditional update, it batch-inserts all messages. Any serialization or insert failure rolls back both the messages and version update.

This optimistic check is a safety net, not a queue. Two callers may both run the model from the same snapshot, but only one can persist. The caller must serialize same-conversation requests until a later Redis queue design is implemented.

## Runtime and Persistence Error Semantics

The conversation runner follows these rules:

- Runtime success: append the current user input and all `NewMessages`.
- Runtime error with non-empty `NewMessages`: append the current user input and completed Assistant/Tool messages, then return the runtime error.
- Runtime error with no `NewMessages`: do not append the current input, allowing the caller to retry without creating an unanswered duplicate.
- History load failure: do not invoke Runtime.
- Append failure: return the persistence error and do not report the turn as persisted.
- Runtime and append failure together: return `errors.Join(runtimeErr, persistenceErr)` so both remain discoverable through `errors.Is` and `errors.As`.
- Version mismatch: return `ErrConflict` without automatic retry.
- Corrupt stored JSON or role mismatch: fail history loading with a data-corruption error; do not skip the row.

The original `schema.RunResult` is returned even when persistence fails, preserving current partial-result behavior. Reporter events may already have been emitted before a persistence error, so callers must use the returned error as the persistence outcome.

Canceled contexts are not replaced with a background context for saving. If cancellation prevents the append, the returned error exposes both cancellation/runtime and persistence failures. Durable recovery of such work belongs to the later queue design.

## Configuration

Configuration adds an optional conversation block and the exact MySQL fields selected for this project:

```json
{
  "conversation": {
    "enabled": true,
    "history_message_limit": 100
  },
  "mysql": {
    "host": "127.0.0.1",
    "port": 3306,
    "database": "biz",
    "user": "root",
    "password": "123456",
    "max_open": 100,
    "max_idle": 10,
    "conn_lifetime": 3600,
    "conn_timeout": 3,
    "log_level": 3,
    "slow_threshold": 500
  }
}
```

The MySQL fields map to `go-mysql-sdk.Options`:

| Configuration | SDK field | Unit |
|---|---|---|
| `host` | `Host` | address |
| `port` | `Port` | TCP port |
| `database` | `Database` | database name |
| `user` | `User` | username |
| `password` | `Password` | secret |
| `max_open` | `MaxOpen` | connections |
| `max_idle` | `MaxIdle` | connections |
| `conn_lifetime` | `Lifetime` | minutes |
| `conn_timeout` | `Timeout` | seconds |
| `log_level` | `LogLevel` | GORM log level |
| `slow_threshold` | `SlowThreshold` | milliseconds |

`conn_lifetime: 3600` intentionally means 3600 minutes, or 60 hours, because `go-mysql-sdk` multiplies `Lifetime` by `time.Minute`.

When `conversation.enabled` is false, MySQL fields and conversation environment variables are optional. When enabled, the MySQL host, database, user, and password, a positive history limit, `AGENT_USER_ID`, and `AGENT_CONVERSATION_ID` are required. Validation fails during application startup. Password values must never appear in logs or returned configuration errors.

The `go-mysql-sdk` provider is created only in enabled mode and participates in Fx lifecycle management. Provider construction failures are converted into ordinary startup errors without exposing the DSN or password. On shutdown, the underlying `sql.DB` obtained through the GORM handle is closed.

## Schema Migration

The implementation adds explicit, reviewable SQL migration files for the two tables, indexes, and foreign key. Application startup never calls GORM `AutoMigrate` and never modifies production schema implicitly. Running migrations remains a deployment responsibility.

## Testing

### Conversation runner unit tests

Fakes establish that:

1. the loaded history and caller input reach Runtime in the correct fields;
2. success persists the input followed by every `NewMessages` item;
3. a runtime error with partial messages persists the partial turn and returns the runtime error;
4. a runtime error without messages performs no append;
5. a load failure prevents Runtime execution;
6. runtime and append failures remain individually discoverable after `errors.Join`;
7. a conflict is returned without retry;
8. caller-owned request and result containers are not mutated.

### MySQL store tests

The store accepts injected provider and transaction abstractions. SQL-mock tests verify:

- ownership-scoped lookup and concurrent get-or-create behavior;
- complete JSON round trips for user, assistant, tool-call, tool-result, and error messages;
- `limit + 1` queries, ascending restoration, and incomplete-oldest-turn removal;
- conditional version updates, affected-row checks, batch order, commit, and rollback;
- strict isolation when two users reuse the same conversation ID;
- corrupt JSON and role mismatch failures.

An optional real-MySQL integration suite is enabled through an explicit test environment variable. Default unit tests require no local database. Migration checks validate the table names, JSON column, unique ownership index, message-order index, and foreign key.

### Configuration and compatibility tests

Tests verify:

- the exact JSON MySQL configuration above parses and maps to `go-mysql-sdk.Options` without unit conversion;
- `conn_lifetime=3600` remains 3600 SDK minutes;
- disabled persistence preserves the existing stateless CLI path and does not create a database provider;
- enabled persistence rejects missing MySQL fields, non-positive history limits, or missing user/conversation environment variables;
- configuration errors and logs do not contain the password;
- existing Runtime, Provider, Tool, Reporter, Workspace, and integration behavior remains green.

## Acceptance Criteria

- The second successful run for one `(userID, conversationID)` receives messages saved by the first run.
- Different users and different conversations never share history.
- MySQL retains complete messages while Runtime receives at most the safe configured window.
- A persisted history window never begins with an orphaned tool result or another fragment of an incomplete turn.
- User input and all completed output messages are committed atomically with the conversation version.
- Concurrent stale appends return `ErrConflict` and leave no partial rows.
- The existing CLI still runs without MySQL when persistence is disabled.
- Redis, queues, summaries, and public SDK APIs are absent from this implementation.
