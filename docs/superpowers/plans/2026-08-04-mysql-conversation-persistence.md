# MySQL Conversation Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional internal MySQL-backed multi-user, multi-conversation persistence that loads a safe recent-message window, invokes the existing stateless runtime, and atomically saves each completed or partially completed turn.

**Architecture:** Keep `internal/engine.AgentRuntime` stateless and add `internal/conversation.Runner` as the orchestration boundary. A storage-neutral `conversation.Store` is implemented by `internal/conversation/mysql`, while `internal/driver/mysql` owns `go-mysql-sdk` configuration and Fx lifecycle. The CLI selects the old stateless path or the persisted path from configuration.

**Tech Stack:** Go 1.26, Uber Fx 1.23, `github.com/PycMono/go-mysql-sdk` v1.0.2, GORM/MySQL, `github.com/DATA-DOG/go-sqlmock` v1.5.2, standard `testing`.

## Global Constraints

- Preserve every pre-existing staged, modified, deleted, and untracked user file. Stage only the exact files listed in each task and inspect `git diff --cached --name-status` before every commit.
- Build on the current workspace contract; do not revert or overwrite the existing changes under `internal/context`, `README.md`, `cmd/ping`, `skills`, or `AGENTS.md`.
- Keep all new runtime and persistence APIs under `internal`; public SDK migration is outside this plan.
- MySQL is the source of truth for complete conversation history. Runtime receives only a configurable safe window, default `100` messages.
- Scope ownership by both `user_id` and `conversation_id`; never load a conversation by the caller's conversation ID alone.
- Use optimistic `version` checks. Return `conversation.ErrConflict`; do not retry, queue, or reorder concurrent turns.
- Do not add Redis, workers, distributed locks, summaries, token compaction, retention deletion, branching, or idempotent replay.
- Use the exact selected MySQL fields. `conn_lifetime=3600` means 3600 minutes, with no unit conversion.
- Do not call GORM `AutoMigrate`. Add explicit SQL migration files and leave execution to deployment.
- Persistence must remain optional. Disabled mode must not construct a MySQL connection and must preserve current stateless CLI behavior.
- Never edit or commit `config.json`; it contains local credentials. Use `config.example.json` and test fixtures only.
- Use the caller's context for history load, Runtime, and append. Do not replace cancellation with a background context.

---

## Execution Preflight

Before Task 1, capture the dirty-worktree and baseline-test state without changing it:

```bash
git status --short
git diff --name-only
git diff --cached --name-only
GOCACHE=/private/tmp/go-reagent-gocache go test ./... 2>&1 | tee /private/tmp/go-reagent-baseline-tests.txt
```

The current workspace may already contain unrelated failing `internal/context` tests. Record those failures and do not modify them under this plan. A newly failing package or test is a regression; a byte-for-byte matching baseline failure remains a disclosed external limitation.

---

### Task 1: Conversation and MySQL Configuration Contract

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.ConversationConfig`, `config.MySQLConfig`, `config.DefaultHistoryMessageLimit`.
- Produces: validated `Config.Conversation` and `Config.MySQL` values for Tasks 2, 6, and 7.
- Preserves: all platform and bot configuration semantics when conversation persistence is disabled.

- [ ] **Step 1: Write the failing exact-configuration parsing test**

Add this focused test to `internal/config/config_test.go` using the existing `writeConfig` helper:

```go
func TestLoadParsesConversationAndMySQLConfiguration(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[{"id":"deepseek","protocol":"openai","baseURL":"https://example.test/v1/","apiKey":"key","model":"model"}],
		"conversation":{"enabled":true,"history_message_limit":100},
		"mysql":{
			"host":"127.0.0.1","port":3306,"database":"biz","user":"root","password":"123456",
			"max_open":100,"max_idle":10,"conn_lifetime":3600,"conn_timeout":3,
			"log_level":3,"slow_threshold":500
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Conversation.Enabled || cfg.Conversation.HistoryMessageLimit != 100 {
		t.Fatalf("Conversation = %#v", cfg.Conversation)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 || cfg.MySQL.Database != "biz" ||
		cfg.MySQL.User != "root" || cfg.MySQL.Password != "123456" || cfg.MySQL.MaxOpen != 100 ||
		cfg.MySQL.MaxIdle != 10 || cfg.MySQL.ConnLifetime != 3600 || cfg.MySQL.ConnTimeout != 3 ||
		cfg.MySQL.LogLevel != 3 || cfg.MySQL.SlowThreshold != 500 {
		t.Fatalf("MySQL = %#v", cfg.MySQL)
	}
}
```

- [ ] **Step 2: Run the focused test and verify red**

Run:

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/config -run '^TestLoadParsesConversationAndMySQLConfiguration$' -count=1
```

Expected: compilation fails because `Config` has no `Conversation` or `MySQL` fields.

- [ ] **Step 3: Add the configuration types and exact serialization tags**

Add to `internal/config/config.go`:

```go
const DefaultHistoryMessageLimit = 100

type ConversationConfig struct {
	Enabled             bool `json:"enabled" yaml:"enabled" toml:"enabled"`
	HistoryMessageLimit int  `json:"history_message_limit" yaml:"history_message_limit" toml:"history_message_limit"`
}

type MySQLConfig struct {
	Host          string `json:"host" yaml:"host" toml:"host"`
	Port          int    `json:"port" yaml:"port" toml:"port"`
	Database      string `json:"database" yaml:"database" toml:"database"`
	User          string `json:"user" yaml:"user" toml:"user"`
	Password      string `json:"password" yaml:"password" toml:"password"`
	MaxOpen       int    `json:"max_open" yaml:"max_open" toml:"max_open"`
	MaxIdle       int    `json:"max_idle" yaml:"max_idle" toml:"max_idle"`
	ConnLifetime  int    `json:"conn_lifetime" yaml:"conn_lifetime" toml:"conn_lifetime"`
	ConnTimeout   int    `json:"conn_timeout" yaml:"conn_timeout" toml:"conn_timeout"`
	LogLevel      int    `json:"log_level" yaml:"log_level" toml:"log_level"`
	SlowThreshold int    `json:"slow_threshold" yaml:"slow_threshold" toml:"slow_threshold"`
}
```

Extend `Config` with:

```go
Conversation ConversationConfig `json:"conversation" yaml:"conversation" toml:"conversation"`
MySQL        MySQLConfig        `json:"mysql" yaml:"mysql" toml:"mysql"`
```

- [ ] **Step 4: Write failing defaults, validation, and secret-redaction tests**

Add table-driven cases asserting:

```go
func TestLoadDefaultsConversationHistoryLimit(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Conversation.HistoryMessageLimit != DefaultHistoryMessageLimit {
		t.Fatalf("HistoryMessageLimit = %d", cfg.Conversation.HistoryMessageLimit)
	}
}
```

Add enabled-mode rejection cases for empty `host`, `database`, `user`, or `password`; non-positive `port`, `max_open`, `conn_lifetime`, or `conn_timeout`; negative `max_idle` or `slow_threshold`; `max_idle > max_open`; and `log_level` outside `1..4`. Include a password value `never-print-mysql-password` and assert it is absent from `errorText(err)`.

- [ ] **Step 5: Implement normalization and enabled-mode validation**

In `internal/config/validate.go`, call `c.Conversation.normalizeAndValidate(&c.MySQL)` from `Config.normalizeAndValidate`. Implement:

```go
func (c *ConversationConfig) normalizeAndValidate(mysql *MySQLConfig) error {
	if c.HistoryMessageLimit == 0 {
		c.HistoryMessageLimit = DefaultHistoryMessageLimit
	}
	if c.HistoryMessageLimit < 1 {
		return errors.New("conversation.history_message_limit 必须大于 0")
	}
	if !c.Enabled {
		return nil
	}
	return mysql.normalizeAndValidate()
}

func (m *MySQLConfig) normalizeAndValidate() error {
	m.Host = strings.TrimSpace(m.Host)
	m.Database = strings.TrimSpace(m.Database)
	m.User = strings.TrimSpace(m.User)
	switch {
	case m.Host == "":
		return errors.New("mysql.host 不能为空")
	case m.Port < 1 || m.Port > 65535:
		return errors.New("mysql.port 必须在 1 到 65535 之间")
	case m.Database == "":
		return errors.New("mysql.database 不能为空")
	case m.User == "":
		return errors.New("mysql.user 不能为空")
	case m.Password == "":
		return errors.New("mysql.password 不能为空")
	case m.MaxOpen < 1:
		return errors.New("mysql.max_open 必须大于 0")
	case m.MaxIdle < 0 || m.MaxIdle > m.MaxOpen:
		return errors.New("mysql.max_idle 必须在 0 到 mysql.max_open 之间")
	case m.ConnLifetime < 1:
		return errors.New("mysql.conn_lifetime 必须大于 0")
	case m.ConnTimeout < 1:
		return errors.New("mysql.conn_timeout 必须大于 0")
	case m.LogLevel < 1 || m.LogLevel > 4:
		return errors.New("mysql.log_level 必须在 1 到 4 之间")
	case m.SlowThreshold < 0:
		return errors.New("mysql.slow_threshold 不能小于 0")
	default:
		return nil
	}
}
```

Do not trim or include the password in any validation error.

- [ ] **Step 6: Run the configuration package and verify green**

Run:

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit only the configuration contract**

```bash
git add internal/config/config.go internal/config/validate.go internal/config/config_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: add conversation mysql configuration"
```

---

### Task 2: Storage-Neutral Conversation Runner

**Files:**
- Create: `internal/conversation/store.go`
- Create: `internal/conversation/runner.go`
- Create: `internal/conversation/runner_test.go`

**Interfaces:**
- Consumes: `engine.AgentRuntime`, `engine.Reporter`, `schema.RunRequest`, `schema.RunResult`, and `schema.Message`.
- Produces: `conversation.Key`, `conversation.Snapshot`, `conversation.AppendRequest`, `conversation.Store`, `conversation.RunRequest`, `conversation.Runner`, `conversation.NewRunner`.
- Produces errors: `conversation.ErrNotFound`, `conversation.ErrConflict`, and `conversation.ErrCorruptMessage` for the MySQL adapter.

- [ ] **Step 1: Define the desired contract in a failing runner test**

Create `internal/conversation/runner_test.go` with fakes implementing these exact signatures:

```go
type Key struct {
	UserID         string
	ConversationID string
}

type Snapshot struct {
	ConversationPK uint64
	Version        uint64
	Messages       []schema.Message
}

type AppendRequest struct {
	ConversationPK uint64
	ExpectedVersion uint64
	RunID           string
	Messages        []schema.Message
}

type Store interface {
	LoadOrCreate(context.Context, Key, int) (Snapshot, error)
	AppendTurn(context.Context, AppendRequest) error
}

type RunRequest struct {
	UserID         string
	ConversationID string
	RunID          string
	Input          schema.Message
	Context        []schema.ContextBlock
	Metadata       map[string]string
}

type Runner interface {
	Run(context.Context, RunRequest, engine.Reporter) (schema.RunResult, error)
}
```

The first test supplies one historical Assistant message, invokes `Runner.Run`, and asserts the fake runtime receives exactly that history plus the current input in their separate fields. Assert `AppendTurn.Messages` contains the current User input first and the returned Assistant message second, with snapshot ID/version and request RunID copied exactly.

- [ ] **Step 2: Run the focused test and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation -run '^TestRunnerLoadsRunsAndAppendsTurn$' -count=1
```

Expected: compilation fails because the package contract and `NewRunner` do not exist.

- [ ] **Step 3: Add the store contract and domain errors**

Create `internal/conversation/store.go` with the exact types above and:

```go
var (
	ErrNotFound       = errors.New("conversation not found")
	ErrConflict       = errors.New("conversation version conflict")
	ErrCorruptMessage = errors.New("conversation message is corrupt")
)
```

Keep the field names `ConversationPK`, `ExpectedVersion`, and `Messages` stable; the MySQL adapter and tests rely on them.

- [ ] **Step 4: Implement the minimal conversation runner**

Create `internal/conversation/runner.go` around:

```go
type runner struct {
	runtime      engine.AgentRuntime
	store        Store
	historyLimit int
}

func NewRunner(runtime engine.AgentRuntime, store Store, historyLimit int) Runner {
	return &runner{runtime: runtime, store: store, historyLimit: historyLimit}
}

func (r *runner) Run(ctx context.Context, request RunRequest, reporter engine.Reporter) (schema.RunResult, error) {
	key, err := validateRunRequest(request)
	if err != nil {
		return schema.RunResult{RunID: request.RunID}, err
	}
	snapshot, err := r.store.LoadOrCreate(ctx, key, r.historyLimit)
	if err != nil {
		return schema.RunResult{RunID: request.RunID}, err
	}
	result, runErr := r.runtime.Run(ctx, schema.RunRequest{
		RunID: request.RunID, History: append([]schema.Message(nil), snapshot.Messages...),
		Input: request.Input, Context: append([]schema.ContextBlock(nil), request.Context...),
		Metadata: cloneMetadata(request.Metadata),
	}, reporter)
	if runErr != nil && len(result.NewMessages) == 0 {
		return result, runErr
	}
	messages := make([]schema.Message, 0, 1+len(result.NewMessages))
	messages = append(messages, request.Input)
	messages = append(messages, result.NewMessages...)
	persistErr := r.store.AppendTurn(ctx, AppendRequest{
		ConversationPK: snapshot.ConversationPK,
		ExpectedVersion: snapshot.Version,
		RunID: request.RunID,
		Messages: messages,
	})
	return result, errors.Join(runErr, persistErr)
}
```

`validateRunRequest` must trim IDs into the returned `Key`, require a non-nil context, non-nil runtime/store, a positive history limit, `RoleUser`, non-empty supported text content, and no tool-call fields on current input. Return field-specific errors without echoing message content.

- [ ] **Step 5: Add failure-semantic and immutability tests**

Add focused tests for:

```text
runtime error + partial NewMessages -> append input and partial messages, return runtime error
runtime error + empty NewMessages   -> no append
load error                          -> runtime not called
append error + runtime error        -> errors.Is finds both
ErrConflict                         -> one append call and no retry
empty IDs / invalid input           -> load not called
mutated fake inputs after Run       -> captured History, Context, Metadata, and append slice remain unchanged
```

Use sentinel errors and `errors.Is`; do not compare joined error strings.

- [ ] **Step 6: Run runner tests and verify green**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the conversation boundary**

```bash
git add internal/conversation/store.go internal/conversation/runner.go internal/conversation/runner_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: add persistent conversation runner"
```

---

### Task 3: MySQL Schema, Message Codec, and Safe Window

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `migrations/0001_conversation_persistence.up.sql`
- Create: `migrations/0001_conversation_persistence.down.sql`
- Create: `internal/conversation/mysql/model.go`
- Create: `internal/conversation/mysql/codec.go`
- Create: `internal/conversation/mysql/window.go`
- Create: `internal/conversation/mysql/codec_test.go`
- Create: `internal/conversation/mysql/window_test.go`
- Create: `internal/conversation/mysql/migration_test.go`

**Interfaces:**
- Consumes: `schema.Message` and `conversation.ErrCorruptMessage`.
- Produces: GORM models `conversationRow`, `messageRow`; `encodeMessage`, `decodeMessage`, `safeWindow`; and executable migration SQL.
- Adds: `go-mysql-sdk` v1.0.2 and SQL-mock v1.5.2.

- [ ] **Step 1: Add the pinned persistence and test dependencies**

```bash
go get github.com/PycMono/go-mysql-sdk@v1.0.2
go get -t github.com/DATA-DOG/go-sqlmock@v1.5.2
```

Do not import `go-cache-sdk` or Redis packages.

- [ ] **Step 2: Write failing codec and window tests**

The codec round-trip table must include:

```go
[]schema.Message{
	{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("hello")}},
	{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}},
	{Role: schema.RoleTool, ToolCallID: "call-1", ToolName: "read", IsError: true, Content: []schema.ContentBlock{schema.TextBlock("failed")}},
}
```

For every item, assert `encodeMessage` then `decodeMessage` is `reflect.DeepEqual`. Add corrupt-JSON, unknown-role, and stored-role/payload-role mismatch cases, each requiring `errors.Is(err, conversation.ErrCorruptMessage)`.

For `safeWindow`, create rows ordered newest-first and assert:

```text
extra row belongs to older turn -> discard only extra, reverse remaining rows
extra row shares oldest selected turn -> discard that entire oldest selected turn
single turn exceeds limit -> return empty
at most limit rows -> preserve every row and return ascending order
```

- [ ] **Step 3: Run codec/window tests and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -run 'TestMessageCodec|TestSafeWindow' -count=1
```

Expected: compilation fails because the MySQL persistence package does not exist.

- [ ] **Step 4: Add the explicit migrations**

Create `migrations/0001_conversation_persistence.up.sql` with:

```sql
CREATE TABLE IF NOT EXISTS agent_conversations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(128) NOT NULL,
    conversation_id VARCHAR(128) NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_conversations_owner (user_id, conversation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_pk BIGINT UNSIGNED NOT NULL,
    turn_version BIGINT UNSIGNED NOT NULL,
    ordinal INT UNSIGNED NOT NULL,
    run_id VARCHAR(128) NULL,
    role VARCHAR(32) NOT NULL,
    payload JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_messages_order (conversation_pk, turn_version, ordinal),
    CONSTRAINT fk_agent_messages_conversation FOREIGN KEY (conversation_pk)
        REFERENCES agent_conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

The unique order index also serves reverse and forward history scans; do not create a redundant index with the same columns. The down migration drops `agent_messages` first, then `agent_conversations`.

- [ ] **Step 5: Add focused GORM models and JSON value type**

In `model.go`, define table names and columns explicitly:

```go
type conversationRow struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Version        uint64    `gorm:"column:version"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (conversationRow) TableName() string { return "agent_conversations" }

type messageRow struct {
	ID             uint64      `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationPK uint64      `gorm:"column:conversation_pk"`
	TurnVersion    uint64      `gorm:"column:turn_version"`
	Ordinal        uint32      `gorm:"column:ordinal"`
	RunID          *string     `gorm:"column:run_id"`
	Role           string      `gorm:"column:role"`
	Payload        jsonPayload `gorm:"column:payload;type:json"`
	CreatedAt      time.Time   `gorm:"column:created_at"`
}

func (messageRow) TableName() string { return "agent_messages" }
```

`jsonPayload` implements `driver.Valuer` and `sql.Scanner`, accepts MySQL `[]byte` or `string`, copies source bytes, and rejects all other database types.

- [ ] **Step 6: Implement strict message encoding and safe-window normalization**

`encodeMessage` marshals the entire message and returns `messageRow.Role = string(message.Role)`. `decodeMessage` unmarshals, permits only User/Assistant/Tool roles, and joins `conversation.ErrCorruptMessage` with the exact decode or mismatch cause.

Implement:

```go
func safeWindow(rows []messageRow, limit int) []messageRow {
	if limit < 1 || len(rows) == 0 {
		return nil
	}
	selected := append([]messageRow(nil), rows...)
	if len(selected) > limit {
		extra := selected[limit]
		selected = selected[:limit]
		oldestTurn := selected[len(selected)-1].TurnVersion
		if extra.TurnVersion == oldestTurn {
			for len(selected) > 0 && selected[len(selected)-1].TurnVersion == oldestTurn {
				selected = selected[:len(selected)-1]
			}
		}
	}
	slices.Reverse(selected)
	return selected
}
```

Callers must supply newest-first rows with at most `limit+1` entries.

- [ ] **Step 7: Add migration-content assertions**

`migration_test.go` reads `../../../migrations/0001_conversation_persistence.up.sql` and asserts it contains both tables, `JSON NOT NULL`, `uq_agent_conversations_owner`, `uq_agent_messages_order`, and `fk_agent_messages_conversation`, while not containing `AutoMigrate`.

- [ ] **Step 8: Run focused persistence representation tests**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -run 'TestMessageCodec|TestSafeWindow|TestConversationMigration' -count=1
go mod tidy
git diff --check
```

Expected: PASS, and `go.mod` lists every directly imported dependency as direct.

- [ ] **Step 9: Commit the schema and persistence representation**

```bash
git add go.mod go.sum migrations/0001_conversation_persistence.up.sql migrations/0001_conversation_persistence.down.sql internal/conversation/mysql/model.go internal/conversation/mysql/codec.go internal/conversation/mysql/window.go internal/conversation/mysql/codec_test.go internal/conversation/mysql/window_test.go internal/conversation/mysql/migration_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: define conversation mysql schema"
```

---

### Task 4: MySQL Load-or-Create and History Loading

**Files:**
- Create: `internal/conversation/mysql/store.go`
- Create: `internal/conversation/mysql/store_test.go`

**Interfaces:**
- Consumes: `conversation.Store`, `conversation.Key`, `conversation.Snapshot`, GORM, `safeWindow`, and `decodeMessage`.
- Produces: `mysql.NewStore(DBProvider, TransactionManager) conversation.Store` and `(*Store).LoadOrCreate`.
- Defines adapter interfaces: `DBProvider.UseDB(context.Context) *gorm.DB` and `TransactionManager.Transaction(context.Context, func(context.Context) error) error`.

- [ ] **Step 1: Write the failing ownership-scoped load test**

Use `go-sqlmock` with GORM's MySQL dialector configured with `SkipInitializeWithVersion: true`. Expect a conversation query containing both `user_id = ?` and `conversation_id = ?`, followed by a newest-first `LIMIT 101` message query. Return two JSON rows and assert the snapshot contains the internal ID, version, and ascending decoded messages.

Use these local test adapters so later transaction tests exercise GORM's actual transaction path:

```go
type contextDBProvider struct {
	db  *gorm.DB
	key struct{}
}

func (p *contextDBProvider) UseDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(p.key).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return p.db.WithContext(ctx)
}

func (p *contextDBProvider) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return callback(context.WithValue(ctx, p.key, tx))
	})
}
```

- [ ] **Step 2: Run the focused load test and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -run '^TestStoreLoadOrCreateLoadsOwnedHistory$' -count=1
```

Expected: compilation fails because `Store` and `NewStore` do not exist.

- [ ] **Step 3: Implement the store constructor and existing-row load**

Create:

```go
type DBProvider interface {
	UseDB(context.Context) *gorm.DB
}

type TransactionManager interface {
	Transaction(context.Context, func(context.Context) error) error
}

type Store struct {
	provider     DBProvider
	transactions TransactionManager
}

func NewStore(provider DBProvider, transactions TransactionManager) conversation.Store {
	return &Store{provider: provider, transactions: transactions}
}
```

`LoadOrCreate` validates non-nil context, dependencies, non-empty key fields, and positive limit. A focused `loadOwnedConversation` helper queries `agent_conversations` with both ownership fields and converts `gorm.ErrRecordNotFound` to an error wrapping `conversation.ErrNotFound`; `LoadOrCreate` consumes that sentinel to enter the create path and propagates all other errors. Once the row exists, query `agent_messages` by `conversation_pk`, ordered `turn_version DESC, ordinal DESC`, with `Limit(limit+1)`. Pass the rows through `safeWindow`, decode all rows, and return newly allocated messages.

- [ ] **Step 4: Add and implement concurrent first-use creation tests**

Cover:

```text
record not found -> INSERT conversation version 0 -> return empty snapshot
INSERT returns duplicate/race error -> ownership-scoped reload succeeds -> use winner
INSERT fails and reload fails -> return the original insert error joined with reload context
two users reuse the same conversation ID -> SQL expectations contain distinct user IDs
```

Do not inspect MySQL driver error number. On any create error, re-query the ownership key; accept the winner only when that re-query succeeds.

- [ ] **Step 5: Add safe-window and corrupt-row store tests**

Return `limit+1` rows where the extra row shares the oldest selected turn and assert the incomplete turn is absent. Return malformed JSON and a role mismatch and assert `errors.Is(err, conversation.ErrCorruptMessage)`.

- [ ] **Step 6: Run MySQL load tests and verify green**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -run 'TestStoreLoadOrCreate|TestStoreLoadWindow' -count=1
```

Expected: PASS with `mock.ExpectationsWereMet()` in every SQL-mock test.

- [ ] **Step 7: Commit the load path**

```bash
git add internal/conversation/mysql/store.go internal/conversation/mysql/store_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: load mysql conversation history"
```

---

### Task 5: Transactional Turn Append and Conflict Detection

**Files:**
- Modify: `internal/conversation/mysql/store.go`
- Modify: `internal/conversation/mysql/store_test.go`

**Interfaces:**
- Consumes: `conversation.AppendRequest`, `encodeMessage`, `TransactionManager`, and `DBProvider` from Task 4.
- Produces: `(*Store).AppendTurn(context.Context, conversation.AppendRequest) error`.
- Guarantees: conditional version update and all message inserts commit or roll back together.

- [ ] **Step 1: Write the failing successful-append transaction test**

Expect SQL-mock operations in this order:

```text
BEGIN
UPDATE agent_conversations SET version=version+1 ... WHERE id=? AND version=? -> 1 affected row
INSERT agent_messages containing ordinal 0 User, ordinal 1 Assistant, turn_version expected+1
COMMIT
```

Invoke `AppendTurn` with `ConversationPK=11`, `ExpectedVersion=7`, `RunID="run-8"`, and two messages. Assert no error and all SQL expectations are satisfied.

- [ ] **Step 2: Run the focused append test and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -run '^TestStoreAppendTurnCommitsMessagesAndVersion$' -count=1
```

Expected: FAIL because `AppendTurn` is not implemented.

- [ ] **Step 3: Implement validation, encoding, and one transaction**

Build every `messageRow` before opening the transaction. Empty message lists, zero conversation IDs, unknown roles, or invalid JSON serialization fail without a SQL call. Use a nil `run_id` pointer when RunID is empty.

Inside `Transaction`:

```go
result := db.Model(&conversationRow{}).
	Where("id = ? AND version = ?", request.ConversationPK, request.ExpectedVersion).
	Updates(map[string]any{
		"version":    gorm.Expr("version + 1"),
		"updated_at": time.Now().UTC(),
	})
if result.Error != nil {
	return result.Error
}
if result.RowsAffected != 1 {
	return conversation.ErrConflict
}
if err := db.Create(&rows).Error; err != nil {
	return err
}
return nil
```

Every row uses `TurnVersion = ExpectedVersion + 1` and `Ordinal = uint32(index)`.

- [ ] **Step 4: Add conflict and rollback tests**

Add exact cases:

```text
UPDATE affects zero -> ROLLBACK -> errors.Is(ErrConflict), no INSERT
UPDATE succeeds, INSERT fails -> ROLLBACK
UPDATE fails -> ROLLBACK
encoding fails before transaction -> no BEGIN
transaction manager returns sentinel -> sentinel preserved
```

- [ ] **Step 5: Run the complete MySQL store test set**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/conversation/mysql -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the atomic append path**

```bash
git add internal/conversation/mysql/store.go internal/conversation/mysql/store_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: append mysql conversation turns"
```

---

### Task 6: MySQL Connection Lifecycle and Fx Registration

**Files:**
- Create: `internal/driver/mysql/connection.go`
- Create: `internal/driver/mysql/connection_test.go`
- Create: `internal/driver/mysql/register.go`
- Create: `internal/conversation/mysql/register.go`
- Create: `internal/conversation/register.go`
- Create: `internal/conversation/register_test.go`
- Modify: `internal/register.go`

**Interfaces:**
- Consumes: `config.Config`, `go-mysql-sdk.Options`, `sqlsdk.Provider`, `transaction.Manager`, `conversation.Store`, and `conversation.Runner`.
- Produces: `driver/mysql.Connection`, which implements `conversation/mysql.DBProvider` and `TransactionManager`.
- Produces Fx modules: `driver/mysql.Register`, `conversation/mysql.Register`, and `conversation.Register`.

- [ ] **Step 1: Write failing disabled-mode and exact-mapping connection tests**

Test an internal constructor with an injected opener:

```go
type sdkProvider interface {
	sqlsdk.Provider
	transaction.Manager
}

type opener func(*sqlsdk.Options) (sdkProvider, error)
```

Assertions:

```text
conversation disabled -> opener call count 0, non-nil disabled Connection
conversation enabled -> opener receives DB="mysql" and every selected field unchanged
Lifetime == 3600 and Timeout == 3
opener error text containing configured password -> returned startup error omits password
```

- [ ] **Step 2: Run the focused connection tests and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/driver/mysql -count=1
```

Expected: compilation fails because the driver package does not exist.

- [ ] **Step 3: Implement the optional connection wrapper**

`Connection` holds one `sdkProvider` and implements:

```go
var ErrDisabled = errors.New("mysql conversation persistence is disabled")

func (c *Connection) UseDB(ctx context.Context) *gorm.DB {
	if c == nil || c.provider == nil {
		return nil
	}
	return c.provider.UseDB(ctx)
}

func (c *Connection) Transaction(ctx context.Context, callback func(context.Context) error) error {
	if c == nil || c.provider == nil {
		return ErrDisabled
	}
	return c.provider.Transaction(ctx, callback)
}
```

`toSDKOptions` maps the exact config fields and sets `DB: "mysql"`. `openSDKProvider` calls `sqlsdk.NewTransProvider` behind `defer/recover`; both an error and panic become a sanitized `初始化 MySQL 连接失败` error without the DSN, password, or raw panic text.

When enabled, register an Fx `OnStop` hook that calls `provider.UseDB(ctx).DB()` and closes the returned `*sql.DB`. When disabled, register no close hook.

- [ ] **Step 4: Verify lifecycle close and safe failure behavior**

Use `sqlmock.New()` and a GORM MySQL dialector to build a fake provider. Start and stop an `fxtest.App`; expect the underlying SQL DB to close once. Add nil-GORM and `DB()` failure cases and require ordinary errors rather than panics.

- [ ] **Step 5: Register driver, store, and configured runner**

Use these module boundaries:

```go
// internal/driver/mysql/register.go
var Register = fx.Options(fx.Provide(NewConnection))

// internal/conversation/mysql/register.go
func newRegisteredStore(connection *driver.Connection) conversation.Store {
	return NewStore(connection, connection)
}
var Register = fx.Options(fx.Provide(newRegisteredStore))

// internal/conversation/register.go
func newRegisteredRunner(runtime engine.AgentRuntime, store Store, cfg *config.Config) Runner {
	return NewRunner(runtime, store, cfg.Conversation.HistoryMessageLimit)
}
var Register = fx.Options(fx.Provide(newRegisteredRunner))
```

Extend `internal.Register` in dependency order: config, MySQL driver, context/provider/tools/dispatch/engine, MySQL conversation store, conversation runner, app.

- [ ] **Step 6: Add Fx graph tests for disabled mode**

Build an Fx graph with persistence disabled, fake Runtime dependencies, and populate `*driver.Connection`, `conversation.Store`, and `conversation.Runner`. Assert start/stop succeeds without contacting MySQL. Add a registered-runner test asserting history limit `100` reaches a fake Store call.

- [ ] **Step 7: Run driver, conversation, and Fx graph packages**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/driver/mysql ./internal/conversation ./internal/conversation/mysql -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit lifecycle and registration**

```bash
git add internal/driver/mysql/connection.go internal/driver/mysql/connection_test.go internal/driver/mysql/register.go internal/conversation/mysql/register.go internal/conversation/register.go internal/conversation/register_test.go internal/register.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: wire conversation mysql persistence"
```

---

### Task 7: Optional Persisted CLI Execution

**Files:**
- Modify: `internal/app/runner.go`
- Modify: `internal/app/runner_test.go`
- Modify: `internal/app/module_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Prompt`, `conversation.Runner`, `engine.AgentRuntime`, and `engine.Reporter`.
- Changes: `NewAgentRunner` returns `(*AgentRunner, error)` and validates CLI conversation identity only when enabled.
- Preserves: start-once, cancellation, completion callback, reporter forwarding, and process exit behavior.

- [ ] **Step 1: Write failing routing tests**

Add two tests around injected runtime and conversation fakes:

```text
disabled configuration -> AgentRuntime.Run called once; ConversationRunner.Run never called
enabled configuration + env IDs -> ConversationRunner.Run called once; AgentRuntime.Run never called directly
```

The enabled assertion requires:

```go
conversation.RunRequest{
	UserID: "user-1",
	ConversationID: "conversation-1",
	Input: schema.Message{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("test prompt")}},
}
```

and the exact injected Reporter.

- [ ] **Step 2: Run the focused routing tests and verify red**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/app -run 'TestAgentRunnerUsesStatelessRuntimeWhenPersistenceDisabled|TestAgentRunnerUsesConversationRunnerWhenPersistenceEnabled' -count=1
```

Expected: compilation fails because `NewAgentRunner` has no config or conversation runner dependency.

- [ ] **Step 3: Update construction and route one execution**

Add fields for `conversation.Runner`, enabled mode, trimmed user ID, and trimmed conversation ID. Change the constructor to:

```go
func NewAgentRunner(
	runtime engine.AgentRuntime,
	conversationRunner conversation.Runner,
	cfg *config.Config,
	prompt config.Prompt,
	reporter engine.Reporter,
) (*AgentRunner, error)
```

When enabled, read and trim `AGENT_USER_ID` and `AGENT_CONVERSATION_ID`; reject either empty value with an error naming only the environment variable. When disabled, do not require either variable.

Inside the existing goroutine, build the current User message once. Route to `conversationRunner.Run` with the two IDs when enabled; otherwise call the existing `runtime.Run` with empty History and Context. Preserve all locking, Stop, and lifecycle callback behavior.

- [ ] **Step 4: Add constructor, failure, and compatibility tests**

Cover:

```text
enabled + missing AGENT_USER_ID -> constructor error
enabled + missing AGENT_CONVERSATION_ID -> constructor error
disabled + both env vars missing -> constructor succeeds
conversation runner error -> completion callback receives same error and lifecycle exits 1
Stop cancellation -> selected runner observes context cancellation before Stop returns
second Start -> existing rejection remains unchanged
```

Update `module_test.go` supplies for the new constructor and exercise both exit codes with a fake `conversation.Runner` while enabled.

- [ ] **Step 5: Run the app package and verify green**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the CLI adapter**

```bash
git add internal/app/runner.go internal/app/runner_test.go internal/app/module_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat: persist configured cli conversations"
```

---

### Task 8: End-to-End Contract, Example Configuration, and Verification

**Files:**
- Create: `tests/integration/conversation_persistence_test.go`
- Create: `internal/conversation/mysql/store_integration_test.go`
- Modify: `config.example.json`
- Create: `docs/conversation-persistence.md`

**Interfaces:**
- Consumes: the complete conversation runner, MySQL Store, migration, configuration, and CLI selection from Tasks 1-7.
- Produces: an executable two-run acceptance test, an opt-in real-MySQL test, safe deployment documentation, and example configuration.

- [ ] **Step 1: Write the in-process two-run acceptance test**

Create an in-memory Store in `tests/integration/conversation_persistence_test.go` that implements the real `conversation.Store`, including version increments and copies. Use a fake Runtime that records each `schema.RunRequest.History` and returns `answer-1`, then `answer-2`.

Run the same `(user-1, conversation-1)` twice and assert:

```text
first Runtime history is empty
second Runtime history is [first user input, answer-1]
different user with conversation-1 receives empty history
different conversation for user-1 receives empty history
```

- [ ] **Step 2: Run the acceptance test and verify green**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./tests/integration -run '^TestConversationRunnerPersistsAndIsolatesHistory$' -count=1
```

Expected: PASS using the production conversation runner and only fake boundaries.

- [ ] **Step 3: Add an opt-in real-MySQL integration test**

Put `//go:build integration` at the top of `store_integration_test.go`. Require `MYSQL_TEST_HOST`, `MYSQL_TEST_DATABASE`, `MYSQL_TEST_USER`, and `MYSQL_TEST_PASSWORD`; call `t.Skip` if any are absent. Connect through the production driver, read `migrations/0001_conversation_persistence.up.sql`, split its two semicolon-terminated statements, and execute each idempotent `CREATE TABLE IF NOT EXISTS` statement against the dedicated test database. Insert a unique test user/conversation prefix, verify create/append/load and stale-version conflict, then delete only rows belonging to that exact generated ownership pair in `t.Cleanup`.

Run only when a dedicated test database is available:

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test -tags=integration ./internal/conversation/mysql -run '^TestMySQLStoreRoundTrip$' -count=1
```

Never run the down migration from the integration test.

- [ ] **Step 4: Extend the safe example configuration**

Add to `config.example.json`:

```json
"conversation": {
  "enabled": false,
  "history_message_limit": 100
},
"mysql": {
  "host": "127.0.0.1",
  "port": 3306,
  "database": "biz",
  "user": "root",
  "password": "",
  "max_open": 100,
  "max_idle": 10,
  "conn_lifetime": 3600,
  "conn_timeout": 3,
  "log_level": 3,
  "slow_threshold": 500
}
```

Keep the example disabled because the password is intentionally blank.

- [ ] **Step 5: Document setup and exact behavior**

Create `docs/conversation-persistence.md` covering:

```text
apply migrations/0001_conversation_persistence.up.sql through deployment tooling
set conversation.enabled=true and fill the selected mysql fields
export AGENT_USER_ID and AGENT_CONVERSATION_ID for the one-shot CLI
all messages persist; only history_message_limit safe messages reach Runtime
conn_lifetime is minutes; 3600 equals 60 hours
ErrConflict is a safety failure; the caller serializes same-conversation work
Redis queue, summaries, and public SDK are outside this release
disable conversation persistence to retain the old no-MySQL path
```

Do not copy real keys or passwords from `config.json`.

- [ ] **Step 6: Run focused new-feature verification**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./internal/config ./internal/driver/mysql ./internal/conversation ./internal/conversation/mysql ./internal/app ./tests/integration -count=1
```

Expected: PASS.

- [ ] **Step 7: Run repository-wide verification and compare baseline**

```bash
GOCACHE=/private/tmp/go-reagent-gocache go test ./... -count=1
git diff --check
git status --short
```

Expected: every new or modified feature package passes. If `go test ./...` still fails, compare it with `/private/tmp/go-reagent-baseline-tests.txt`; do not alter unrelated user changes to manufacture a green result. Report every remaining baseline failure by package and test name.

- [ ] **Step 8: Commit acceptance coverage and documentation**

```bash
git add tests/integration/conversation_persistence_test.go internal/conversation/mysql/store_integration_test.go config.example.json docs/conversation-persistence.md
git diff --cached --name-status
git diff --cached --check
git commit -m "docs: add conversation persistence setup"
```

- [ ] **Step 9: Final audit**

```bash
git log --oneline --decorate -8
git diff HEAD~8..HEAD --stat
rg -n "go-cache-sdk|redis|AutoMigrate" internal migrations go.mod
rg -n "123456|never-print-mysql-password" --glob '!docs/superpowers/**' --glob '!**/*_test.go' .
```

Expected: production code and `go.mod` contain no Redis dependency or `AutoMigrate`; the selected example password appears only in tests/specification, never in runtime config or logs; commits contain only scoped files.
