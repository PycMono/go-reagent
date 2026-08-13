# Database Layer Boundaries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split conversation MySQL persistence into a connection driver, domain entities, a domain repository interface, and an infrastructure repository implementation without retaining old package compatibility.

**Architecture:** `infrastructure/driver/mysql` owns only the SDK-backed connection. `conversation.Runner` consumes `domain/repository/conversation.Store`, whose values live in `domain/entity/conversation`; `infrastructure/persistence/conversation` implements that interface with GORM and the MySQL driver. Fx composes the driver, repository adapter, and use-case runner at the application boundary.

**Tech Stack:** Go 1.26, Uber Fx, GORM, `github.com/PycMono/go-mysql-sdk`, `go test`, `go vet`

## Global Constraints

- Do not retain type aliases, wrappers, deprecated exports, or old package compatibility.
- Define application errors centrally in `common/errors` using the same `CodeError`, `BizError`, `SysError`, `Wrap`, `Params`, and code-based `errors.Is` design as `micro-framework`.
- Preserve all database table names, columns, migration files, query semantics, optimistic concurrency, and transaction behavior.
- Keep GORM row models, codecs, and query helpers private to `infrastructure/persistence/conversation`.
- Keep `infrastructure/driver/mysql` independent of conversation entity and repository packages.
- Preserve unrelated worktree changes, including the untracked `.DS_Store`.
- The environment-gated MySQL round-trip test may skip without database variables; all other tests must pass.

---

## File Structure

### New files

- `domain/entity/conversation/store.go`: persistence-neutral `Key`, `Snapshot`, and `AppendRequest` values.
- `domain/repository/conversation/store.go`: `Store` interface.
- `domain/repository/conversation/store_test.go`: compile-time interface exercise.
- `common/errors/errors.go`: centralized coded error types and conversation error registrations.
- `common/errors/errors_test.go`: coded identity, classification, wrapping, and cause-chain behavior.
- `infrastructure/persistence/conversation/bootstrap.go`: Fx adapter registration.
- `infrastructure/persistence/conversation/model.go`: private GORM rows and JSON SQL value.
- `infrastructure/persistence/conversation/codec.go`: message serialization and validation.
- `infrastructure/persistence/conversation/invocation.go`: invocation-to-row conversion.
- `infrastructure/persistence/conversation/window.go`: complete-turn history window selection.
- `infrastructure/persistence/conversation/store.go`: MySQL repository implementation.
- Corresponding tests under `infrastructure/persistence/conversation` migrated from the current MySQL package.

### Modified files

- `conversation/store.go`: retain only use-case request and runner declarations.
- `conversation/runner.go`: consume the new domain entity and repository packages.
- `conversation/bootstrap.go`: inject the domain repository interface.
- `conversation/bootstrap_test.go`: populate and fake the domain repository interface.
- `application/bootstrap.go`: compose driver and persistence modules separately.
- `tests/integration/conversation_persistence_test.go`: use the new domain packages.
- `tests/integration/package_boundaries_test.go`: enforce the new onion dependency direction.

### Removed or relocated files

- `infrastructure/driver/mysql/bootstrap.go`: repository registration moves to persistence; the driver module remains connection-only.
- `infrastructure/driver/mysql/{codec,invocation,model,store,window}.go`: move to `infrastructure/persistence/conversation`.
- Their focused tests move with the implementation; connection tests remain in the driver.
- The obsolete `persistence/mysql` path disappears completely.

---

### Task 1: Establish the Domain Conversation Contract

**Files:**
- Create: `common/errors/errors.go`
- Create: `common/errors/errors_test.go`
- Create: `domain/entity/conversation/store.go`
- Create: `domain/repository/conversation/store.go`
- Create: `domain/repository/conversation/store_test.go`
- Modify: `conversation/store.go`
- Modify: `conversation/runner.go`
- Modify: `conversation/bootstrap.go`
- Modify: `conversation/runner_test.go`
- Modify: `conversation/bootstrap_test.go`
- Modify: `tests/integration/conversation_persistence_test.go`

**Interfaces:**
- Produces: `entity.Key`, `entity.Snapshot`, `entity.AppendRequest` in `domain/entity/conversation`.
- Produces: `repository.Store` in `domain/repository/conversation`.
- Produces: coded conversation errors in `common/errors`; not-found/conflict are `BizError`, corrupt-message is `SysError`.
- Consumes: `pi/ai.Message` and `pi/agent.ModelInvocation` as existing public SDK values.

- [ ] **Step 1: Add a failing domain repository contract test**

```go
type fakeStore struct{}

func (fakeStore) LoadOrCreate(context.Context, entity.Key, int) (entity.Snapshot, error) {
	return entity.Snapshot{}, nil
}
func (fakeStore) AppendTurn(context.Context, entity.AppendRequest) error { return nil }

var _ repository.Store = fakeStore{}
```

- [ ] **Step 2: Run the contract test and verify the new packages are missing**

Run: `go test ./domain/repository/conversation -count=1`

Expected: FAIL because `domain/entity/conversation` and `domain/repository/conversation` do not yet provide the declared contract.

- [ ] **Step 3: Implement the domain values and interface**

```go
// domain/entity/conversation/store.go
type Key struct {
	UserID         string
	ConversationID string
}

type Snapshot struct {
	ConversationPK uint64
	Version        uint64
	Messages       []ai.Message
}

type AppendRequest struct {
	ConversationPK  uint64
	ExpectedVersion uint64
	RunID           string
	Messages        []ai.Message
	Invocations     []agent.ModelInvocation
}
```

```go
// domain/repository/conversation/store.go
type Store interface {
	LoadOrCreate(context.Context, entity.Key, int) (entity.Snapshot, error)
	AppendTurn(context.Context, entity.AppendRequest) error
}
```

Create the centralized error machinery and registrations in `common/errors`:

```go
var (
	ErrConversationNotFound       = NewBizError(10101, "conversation not found")
	ErrConversationConflict       = NewBizError(10102, "conversation version conflict")
	ErrConversationCorruptMessage = NewSysError(10103, "conversation message is corrupt")
)
```

Tests assert code/message identity, business-versus-system classification,
distinct `errors.Is` identity, and that `Wrap` preserves both the coded error
and its concrete cause.

- [ ] **Step 4: Migrate the conversation runner and its fakes**

Change the runner dependency to `repository.Store`, construct `entity.Key` in
`validateRunRequest`, and construct `entity.AppendRequest` before append. Update
the disabled graph test to provide a domain repository fake directly until the
real persistence module is registered in Task 2. Keep the old persistence
declarations temporarily so the not-yet-migrated MySQL implementation continues
to compile during this task; they are deleted, not aliased, in Task 2.

After Task 2, `conversation/store.go` retains only:

```go
type RunRequest struct {
	UserID         string
	ConversationID string
	RunID          string
	Input          ai.Message
	Context        []agent.ContextBlock
	Metadata       map[string]string
}

type Runner interface {
	Run(context.Context, RunRequest, agent.Reporter) (agent.RunResult, error)
}
```

- [ ] **Step 5: Run the domain and use-case tests**

Run: `go test ./domain/... ./conversation/... ./tests/integration -run 'Conversation|RepositoryErrors|Package' -count=1`

Expected: PASS. The old MySQL implementation still compiles against the
temporary declarations, while all migrated runner code and acceptance fakes use
the new domain contract.

- [ ] **Step 6: Commit the domain contract**

```bash
git add common/errors domain/entity/conversation domain/repository/conversation conversation tests/integration/conversation_persistence_test.go
git commit -m "refactor: extract conversation domain repository"
```

---

### Task 2: Split the MySQL Driver from the Repository Implementation

**Files:**
- Modify: `infrastructure/driver/mysql/connection.go`
- Modify: `infrastructure/driver/mysql/bootstrap.go`
- Move: `infrastructure/driver/mysql/model.go` -> `infrastructure/persistence/conversation/model.go`
- Move: `infrastructure/driver/mysql/codec.go` -> `infrastructure/persistence/conversation/codec.go`
- Move: `infrastructure/driver/mysql/invocation.go` -> `infrastructure/persistence/conversation/invocation.go`
- Move: `infrastructure/driver/mysql/window.go` -> `infrastructure/persistence/conversation/window.go`
- Move: `infrastructure/driver/mysql/store.go` -> `infrastructure/persistence/conversation/store.go`
- Move: `infrastructure/driver/mysql/bootstrap.go` repository registration -> `infrastructure/persistence/conversation/bootstrap.go`
- Move the matching codec, invocation, migration, store, window, and integration tests to `infrastructure/persistence/conversation`.
- Modify: `application/bootstrap.go`
- Modify: `conversation/bootstrap_test.go`

**Interfaces:**
- Consumes: `repository.Store` and the `entity` request/result values from Task 1.
- Consumes: `mysql.Connection`, which implements `UseDB(context.Context) *gorm.DB` and `Transaction(context.Context, func(context.Context) error) error`.
- Produces: `persistence.NewStore(DBProvider, TransactionManager) repository.Store`.
- Produces: `persistence.Module`, which registers the domain repository interface.

- [ ] **Step 1: Move the implementation tests first and update their imports**

Update test package declarations to `package conversation` and replace old
domain references such as:

```go
entity.Key{UserID: "user", ConversationID: "conversation"}
entity.AppendRequest{ConversationPK: 1, ExpectedVersion: 0, Messages: messages}
errors.Is(err, repository.ErrConflict)
```

Keep direct connection construction in the integration test through:

```go
connection, err := mysql.NewConnection(lifecycle, cfg)
store := NewStore(connection, connection)
```

- [ ] **Step 2: Run the moved tests and verify the implementation package is absent**

Run: `go test ./infrastructure/persistence/conversation -count=1`

Expected: FAIL because the repository implementation files have not moved yet.

- [ ] **Step 3: Move and update the repository implementation**

Set every moved implementation file to `package conversation`. Alias domain
packages consistently:

```go
conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
```

The constructor and error mapping become:

```go
func NewStore(provider DBProvider, transactions TransactionManager) conversationrepo.Store {
	return &Store{provider: provider, transactions: transactions}
}

if errors.Is(err, gorm.ErrRecordNotFound) {
	return conversationRow{}, fmt.Errorf("mysql conversation: %w", commonerrors.ErrConversationNotFound)
}
```

All other SQL, validation, codecs, transactions, and window behavior remain
byte-for-byte equivalent except for package/type qualification.

Delete `conversation.Store`, `conversation.Key`, `conversation.Snapshot`,
`conversation.AppendRequest`, `conversation.ErrNotFound`,
`conversation.ErrConflict`, and `conversation.ErrCorruptMessage` after the last
implementation import has moved. Do not replace them with aliases.

- [ ] **Step 4: Restrict the driver module to the connection**

```go
// infrastructure/driver/mysql/bootstrap.go
var Module = fx.Options(fx.Provide(NewConnection))
```

Register the adapter separately:

```go
// infrastructure/persistence/conversation/bootstrap.go
func newRegisteredStore(connection *mysql.Connection) conversationrepo.Store {
	return NewStore(connection, connection)
}

var Module = fx.Options(fx.Provide(newRegisteredStore))
```

- [ ] **Step 5: Compose both infrastructure modules**

Update `application.Module` to include:

```go
mysql.Module,
conversationpersistence.Module,
conversation.Module,
```

Update the disabled-persistence Fx graph test to include both MySQL and
conversation persistence modules and populate `conversationrepo.Store`.

- [ ] **Step 6: Run focused driver, persistence, conversation, and Fx tests**

Run: `go test ./infrastructure/driver/mysql ./infrastructure/persistence/conversation ./conversation ./application ./tests/integration -count=1`

Expected: PASS; the external MySQL round-trip test may report SKIP when its
environment variables are not set.

- [ ] **Step 7: Commit the infrastructure split**

```bash
git add application conversation infrastructure/driver/mysql infrastructure/persistence/conversation persistence/mysql
git commit -m "refactor: split mysql driver and conversation persistence"
```

---

### Task 3: Enforce Architecture Boundaries and Verify the Repository

**Files:**
- Modify: `tests/integration/package_boundaries_test.go`
- Modify: any stale import found by repository-wide search.

**Interfaces:**
- Consumes: final package layout from Tasks 1 and 2.
- Produces: regression checks that reject domain-to-outer-layer imports and conversation persistence code in the MySQL driver.

- [ ] **Step 1: Add failing dependency-boundary cases**

Add domain packages to the dependency table with a predicate rejecting service
and infrastructure packages:

```go
{
	pkg: modulePath + "/domain/repository/conversation",
	forbidden: func(dependency string) bool {
		return dependency == modulePath+"/conversation" ||
			strings.HasPrefix(dependency, modulePath+"/application") ||
			strings.HasPrefix(dependency, modulePath+"/config") ||
			strings.HasPrefix(dependency, modulePath+"/infrastructure") ||
			strings.HasPrefix(dependency, "gorm.io/")
	},
},
```

Add a source-layout test that rejects conversation persistence implementation
files in `infrastructure/driver/mysql` and rejects the old `persistence/mysql`
directory.

- [ ] **Step 2: Run the boundary tests and verify stale paths fail**

Run: `go test ./tests/integration -run 'Package|Boundary|Legacy' -count=1`

Expected: FAIL if any old import/path or misplaced persistence implementation remains.

- [ ] **Step 3: Remove every stale package reference**

Run these read-only searches and update each result that refers to live code:

```bash
rg -n 'go-reagent/persistence/mysql|conversation\.(Store|Key|Snapshot|AppendRequest|ErrNotFound|ErrConflict|ErrCorruptMessage)' --glob '*.go' .
rg -n 'domain/(entity|repository)/conversation|infrastructure/(driver/mysql|persistence/conversation)' --glob '*.go' .
```

Expected: the first search returns no live Go references; the second search
shows only dependency-direction-compliant imports.

- [ ] **Step 4: Run formatting and all verification**

Run:

```bash
gofmt -w application conversation domain infrastructure tests/integration
go test ./...
go vet ./...
git diff --check
```

Expected: all commands exit 0. The environment-gated MySQL integration test may skip explicitly.

- [ ] **Step 5: Commit the architecture guard**

```bash
git add tests/integration/package_boundaries_test.go application conversation domain infrastructure
git commit -m "test: enforce database layer boundaries"
```
