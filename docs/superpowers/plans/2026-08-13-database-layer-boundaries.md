# Database And Conversation Boundaries

## Goal

The database and Conversation business code follow the same dependency direction and registration style as `micro-framework`. No aliases or compatibility packages are retained.

## Package ownership

- `infrastructure/driver/mysql`: builds `sqlsdk.Provider` from configuration and exposes its transaction manager. It contains no entity, Repository, codec, or SQL query code.
- `domain/entity/conversation`: owns the three GORM table entities and the message JSON value objects. It has no dependency on `pi`, application, configuration, or infrastructure packages.
- `domain/repository/conversation`: declares explicit Conversation Repository operations.
- `infrastructure/persistence/conversation`: implements the Repository with GORM and directly uses the Domain table entities. It does not define duplicate private row models.
- `conversation`: orchestrates history loading, SDK execution, SDK/Domain mapping, and atomic turn appends.
- `application/register.go`: assembles `pi`, infrastructure, Conversation, transport, configuration, and lifecycle registration.

## Persisted entities

### `agent_conversations`

`Conversation` contains only database-backed metadata:

- `ID string`: internal Snowflake primary key.
- `UserID string` and `ConversationID string`: business ownership identity, jointly unique.
- `Version uint64`: optimistic concurrency version.
- `CreatedAt` and `UpdatedAt`.

It does not contain a `Messages` field or any `gorm:"-"` aggregate field.

### `agent_messages`

`Message` contains a string ID, the internal Conversation ID, turn version, ordinal, Run ID, role, JSON payload, and creation time. `MessagePayload` contains only historical content and tool-call data; role and execution metadata remain columns.

### `agent_model_invocations`

`ModelInvocation` contains a string ID, the internal Conversation ID, turn/run ordering, model identity, input/output Token counts, prices, cost, latency, and creation time. This table is the authoritative usage ledger.

All entity IDs are generated through `domain/repository.IIDService`, whose infrastructure implementation follows the `micro-framework` Snowflake string-ID service.

## Repository contract

```go
type IConversationRepository interface {
    FindByUserIDAndConversationID(ctx context.Context, userID, conversationID string) (*entity.Conversation, bool, error)
    Create(ctx context.Context, conversation *entity.Conversation) error
    ListMessagesByConversationID(ctx context.Context, conversationID string, messageLimit int) ([]*entity.Message, error)
    AppendTurn(
        ctx context.Context,
        userID string,
        conversationID string,
        expectedVersion uint64,
        messages []*entity.Message,
        invocations []*entity.ModelInvocation,
    ) error
}
```

Missing queries return `(nil, false, nil)`, matching the reference Repository convention. Business errors use the shared definitions in `common/errors`; the Repository does not declare local sentinel errors.

## Transaction boundary

`AppendTurn` resolves the user-owned Conversation, advances its version with an optimistic-lock condition, and inserts all messages and model invocation records in one transaction. Any failure rolls back the whole turn.

## Registration

Registration files use `Register` rather than business-layer `bootstrap.go` files:

- `infrastructure/register.go`
- `infrastructure/persistence/register.go`
- `infrastructure/serviceimpl/register.go`
- `conversation/register.go`
- `application/register.go`

The existing `pi` and transport bootstrap modules are outside this database/business refactor and remain unchanged.
