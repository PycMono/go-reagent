# Database Layer Boundaries Design

**Date:** 2026-08-13

## Goal

Refactor the existing conversation MySQL persistence code to follow the same
onion-architecture boundaries used by `micro-framework`:

- database connection and transaction access belong to
  `infrastructure/driver/mysql`;
- persisted conversation entities belong to `domain/entity/conversation`;
- the conversation persistence interface belongs to
  `domain/repository/conversation`;
- the MySQL implementation belongs to
  `infrastructure/persistence/conversation`.

This is a complete package migration. The old package locations and exported
conversation persistence types will not receive aliases, wrappers, or other
compatibility shims. All repository-owned callers and tests will move to the
new packages in the same change.

## Current State

The worktree already moves the former `persistence/mysql` package wholesale to
`infrastructure/driver/mysql`. That package currently mixes four concerns:

1. opening and closing the MySQL SDK provider;
2. domain-facing persistence request and result types, currently declared in
   `conversation/store.go`;
3. GORM row models;
4. repository queries, transactions, codecs, and history-window logic.

The refactor completes this partial move by separating those concerns instead
of treating the entire persistence package as a database driver.

## Package Design

### `infrastructure/driver/mysql`

This package owns only MySQL infrastructure access:

- SDK option mapping from `config.Config`;
- provider construction;
- Fx lifecycle shutdown;
- the concrete connection wrapper;
- database and transaction capabilities exposed to outer infrastructure
  packages.

It must not import conversation domain entity or repository packages. It does
not contain GORM table models, codecs, query logic, or repository registration.

### `domain/entity/conversation`

This package owns persistence-neutral conversation data structures:

- `Key`;
- `Snapshot`;
- `AppendRequest`.

These types may use the existing public SDK value types from `pi/ai` and
`pi/agent`, but they contain no GORM tags and do not import infrastructure.

The SQL row structs are not domain entities. They remain private to the MySQL
repository implementation because their columns, table names, JSON payload
representation, and timestamps are storage details.

### `domain/repository/conversation`

This package owns the persistence port:

- `Store` with `LoadOrCreate` and `AppendTurn`;
- `ErrNotFound`;
- `ErrConflict`;
- `ErrCorruptMessage`.

Method parameters and results use `domain/entity/conversation` types. The
package depends only on the standard library and inner/public domain value
packages. It never imports GORM, configuration, or infrastructure.

### `infrastructure/persistence/conversation`

This package owns the MySQL adapter for the conversation repository:

- the concrete `Store` implementation and constructor;
- private GORM row models and table names;
- message and invocation codecs;
- history-window selection;
- optimistic-version update and transactional append;
- Fx registration that provides the domain repository interface.

It depends on `domain/entity/conversation`,
`domain/repository/conversation`, and `infrastructure/driver/mysql`. Storage
errors continue to wrap the domain repository sentinel errors so callers can
use `errors.Is` without depending on infrastructure.

### `conversation`

The existing package remains the conversation use-case/orchestration package.
It owns `RunRequest`, `Runner`, validation, cloning, and the runner's Fx
registration. Its runner depends on `domain/repository/conversation.Store` and
constructs `domain/entity/conversation` values when loading or appending.

The existing `conversation.Store`, `conversation.Key`,
`conversation.Snapshot`, `conversation.AppendRequest`, and repository error
declarations are removed rather than aliased.

## Dependency Direction

The resulting dependency flow is:

```text
application
  -> conversation
       -> domain/repository/conversation
            -> domain/entity/conversation

application
  -> infrastructure/persistence/conversation
       -> domain/repository/conversation
       -> domain/entity/conversation
       -> infrastructure/driver/mysql

infrastructure/driver/mysql
  -> config
  -> go-mysql-sdk / GORM
```

The domain packages never import `conversation`, `config`, or
`infrastructure`. The MySQL driver never imports the persistence adapter.

## Dependency Injection

Registration is split by responsibility:

- `infrastructure/driver/mysql.Module` provides the MySQL connection;
- `infrastructure/persistence/conversation.Module` constructs the concrete
  store from the connection and provides it as
  `domain/repository/conversation.Store`;
- `conversation.Module` constructs the runner from that repository interface;
- `application.Module` includes both infrastructure modules before the
  conversation module.

Disabled conversation persistence keeps its current behavior: constructing the
connection does not contact MySQL, while actual repository operations fail
through the existing unavailable/disabled behavior.

## Runtime Behavior

No database schema or persistence semantics change:

1. `Runner` validates the request and asks the repository to load or create the
   owned conversation.
2. The MySQL adapter loads a bounded, complete-turn history window and decodes
   stored messages.
3. The runtime receives that history and produces new messages and model
   invocations.
4. `Runner` constructs an append entity request.
5. The MySQL adapter increments the expected version and inserts messages and
   invocations in one transaction.
6. A version mismatch remains `ErrConflict`; malformed stored payloads remain
   `ErrCorruptMessage`.

Table names, columns, migration files, validation rules, history ordering,
optimistic concurrency, and transaction atomicity remain unchanged.

## Migration Scope

The implementation will:

- retain only connection-related files and tests under
  `infrastructure/driver/mysql`;
- add conversation entity and repository packages under `domain`;
- move the repository implementation and its focused tests under
  `infrastructure/persistence/conversation`;
- update application, conversation, and integration-test imports;
- update package-boundary tests to enforce the new dependency direction;
- remove the obsolete root `persistence` package path if it becomes empty;
- avoid unrelated changes to database configuration, migrations, SDK-facing
  public packages, and runtime behavior.

## Verification

Verification covers both behavior and architecture:

- unit tests for connection option mapping and lifecycle behavior;
- unit tests for codecs, invocation encoding, history windows, query behavior,
  validation, optimistic conflicts, and transaction rollback;
- Fx graph tests for disabled persistence and runner registration;
- conversation persistence integration tests;
- dependency-boundary assertions preventing domain-to-infrastructure imports
  and preventing repository logic from returning to the driver package;
- `go test ./...`;
- `go vet ./...`;
- `git diff --check`.

The MySQL round-trip test remains environment-gated and may skip when its test
database variables are unavailable; all non-external tests must pass.
