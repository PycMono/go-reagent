# Database Layer Boundaries Design

**Date:** 2026-08-13

## Goal

Conversation persistence follows the `micro-framework` onion architecture without compatibility shims:

- `infrastructure/driver/mysql` provides the MySQL SDK Provider and transaction manager.
- `domain/entity/conversation` owns database-backed entities.
- `domain/repository/conversation` owns the Repository interface.
- `infrastructure/persistence/conversation` owns GORM queries and transactions.
- `conversation` owns business orchestration and SDK/Domain mapping.

## Stored information

The feature persists only the information required to resume and account for a Conversation:

1. Conversation identity, ownership, optimistic version, and timestamps.
2. Ordered historical user, assistant, and tool messages.
3. Every completed model invocation's Token counts, price, cost, model, phase, and latency.

These are represented by `Conversation`, `Message`, and `ModelInvocation`. Each maps to one table and has a string Snowflake primary key. Child entities use a string `ConversationID` foreign key. `Conversation` does not contain transient history fields.

Message JSON is a SQL value object containing Content and ToolCall data only. Token Usage is not duplicated in message JSON; `agent_model_invocations` is the authoritative usage ledger.

## Repository

The Repository exposes explicit business operations to find/create Conversation metadata, list bounded message history, and append one turn atomically. It returns `(nil, false, nil)` for missing metadata, matching the reference Repository convention.

`AppendTurn` updates the expected Conversation version and inserts messages and invocation ledger records in one transaction. A version mismatch uses `common/errors.ErrConflict`. The Repository defines no local error sentinels.

## MySQL provider

`mysql.NewProvider` maps configuration directly into `sqlsdk.Options`, following `micro-framework/infrastructure/driver/mysql/mysql.go`. `mysql.NewTransactionManager` exposes the transaction capability of that Provider. When Conversation persistence is disabled, construction returns a non-connecting disabled Provider so the CLI remains usable without MySQL.

## Dependency rules

- Domain packages never import `pi`, application, config, or infrastructure packages.
- The MySQL driver never imports Conversation entities, repositories, or persistence code.
- Persistence directly uses Domain entities and defines no duplicate row models.
- The removed `persistence/mysql` path and business-layer `bootstrap.go` compatibility files stay absent.

## Registration

Database and business components use layered `Register` values:

- `infrastructure/serviceimpl.Register`
- `infrastructure/persistence.Register`
- `infrastructure.Register`
- `conversation.Register`
- `application.Register`

The `pi` and transport bootstrap modules are outside this database/business change.
