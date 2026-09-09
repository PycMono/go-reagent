# Multi-Agent Phase 1B Implementation Plan

**Approval status:** User approved implementation on 2026-09-09: “按照这个思路开发吧”. Execute together with `../specs/2026-09-09-multi-agent-platform-design.md`; current detailed spec governs conflicts.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver tenant-scoped Agents, immutable initial versions, an explicit Agent-directory-to-chat flow, and isolated per-conversation production runtimes.

**Architecture:** Keep business identity and version selection in the application, above `pi`. Persist Agents and complete version snapshots in MySQL, with Git and materialized read-only files on the persistent data disk. Replace the server singleton runtime with leased per-conversation runtimes; retain existing message, invocation, SSE, image and conversation management machinery.

**Tech Stack:** Go 1.26, Gin, Fx, GORM/go-mysql-sdk, MySQL, Git CLI, existing embedded HTML/CSS and vanilla JavaScript, Node built-in test runner; Phase 1A WorkspacePolicy and InspectWorkspace.

**Spec:** `docs/superpowers/specs/2026-09-04-agent-self-training-design.md`, §§1, 3, 4.1–4.2, 4.4, 5, 9.1, 9.3, 10.1 and Phase 1B of §11. Read §8.2 for follow-latest semantics. Training, activate/rollback, and the third table belong to Phase 1C; complete administrator UI and model-only releases belong to Phase 1D.

## Global Constraints

- “部署范围为单个服务进程 + MySQL + 持久化文件盘”；do not add distributed locking, event replay, or an Outbox.
- “第一版新增三张业务表”：`agents`, `agent_versions`, `agent_training_sessions`; Phase 1B creates only the first two.
- “可信宿主认证提供 `Principal{TenantID, UserID, Role}`，不接受请求正文或任意 Header 自报管理员/租户。”
- “未配置可信管理员认证则不注册管理页面和管理写 API。”
- “Agent目录和普通聊天在 Phase 1B 交付，管理员完整工作台在 Phase 1D 交付。”
- “摘要协议第一版固定为 `digest_version=1`”；use RFC 8785 JCS, not ordinary `json.Marshal` presented as JCS.
- “一个 Conversation 固定 Agent 身份”；first message creates the conversation, clicking an Agent never does.
- Shared initial/training validation configuration: `agent_training.approved_interpreters=[]`, `agent_training.smoke_timeout_seconds=30`, `agent_training.validation_ttl_seconds=1800`; interpreter entries pin extension, absolute interpreter path, version/content digest and preinstalled dependencies. Positive timeouts/TTL are required.
- `agent_runtime`: `max_instances=32`, `max_instances_per_tenant=8`, `validation_reserved_instances=2`, `validation_reserved_instances_per_tenant=1`, `idle_ttl_seconds=900`, `acquire_timeout_seconds=5`.
- Do not put tenant/Agent/version IDs, Git or database dependencies in `pi.RunRequest`.
- Existing unrelated changes, including concurrent Phase 1A changes, must be preserved. Execute this plan only after Phase 1A review and its applicable verification gate.

## Verified baseline and integration decisions

Read for this plan: `cmd/server/app.go`, `config/{config,platform,server_options}.go`, `infrastructure/driver/gingext/gingext.go`, `infrastructure/middleware/visitor.go`, `application/service/chat/{service,run_manager}.go`, `conversation/{runner,store}.go`, both conversation repository contracts and implementations, HTTP route/controller code, `frontend/static/js/pages/chat.js`, profile catalog/fixtures and migrations through 0006.

1. Server constructs exactly one `*pi.Agent` in `newApp`, then injects a `pi.Runner` into `conversation.NewRunner`. `chat.StartRun` builds Profile context, while `conversation.runner` loads history and appends the turn. Runtime replacement must reach this boundary; changing the chat selector alone cannot isolate tools or directories.
2. `conversation.runner.Run` auto-creates a missing conversation. This must not remain reachable from tenant Web chat: missing or unbound rows fail closed. Preserve explicit CLI/local usage through its own construction path, without deriving a tenant from an arbitrary Web request.
3. Both management SQL and AppendTurn currently key by `user_id,conversation_id`; internal message foreign keys use the conversation's internal `id`. Scope ownership before every operation, including rename, delete, cancel, history reads, append and runtime admission. No migration of message rows is needed.
4. `Visitor` stores an anonymous UserID in the Redis session and `bizctx`; there is no trusted TenantID, login endpoint, administrator role source, or authentication adapter. The repository cannot claim customer login is delivered by adding a Header parser.
5. Existing `providers.Options.APIKey` is a secret body. Never serialize it into a version. Configured platform IDs can serve as server-owned credential references, while effective model/protocol/base URL and parameters are snapshotted independently. Later changes to the selected default platform must not change a saved version.
6. Profile skills are discovered from `profiles/<code>/skills`, but shared skills live in `workspaces/chat/skills`. Existing profile-level `references/` and skill `templates/` are outside §5's final layout. Template assembly must relocate references into `documents/` and templates into per-skill `assets/`, rewrite exact Markdown paths, and verify every local reference. Do not silently drop them or include all workspace profiles.
7. `chat.js` currently insists on a default profile and falls back to it. That is incompatible with a valid empty tenant and explicit selection. The server currently renders chat only at `/`; add `/agents` and `/chat` explicitly.
8. Existing integration tests require `MYSQL_TEST_HOST`, `MYSQL_TEST_DATABASE`, `MYSQL_TEST_USER`, `MYSQL_TEST_PASSWORD` and optional `MYSQL_TEST_PORT`; skipped integration tests are not migration evidence.

### External inputs and safe execution boundary

Implement the interfaces, fake-auth tests, explicitly configured single-tenant anonymous mode, and all independent work without inventing a login system. A real multi-customer rollout requires the host to supply a trusted `Authenticator` implementation, its session/token verification and role mapping, and its unauthenticated-page redirect behavior. No concrete host protocol is present in this checkout. `host` mode without this injected adapter fails startup. Anonymous mode requires an explicitly configured tenant and never enables management routes. Do not auto-select anonymous mode when host authentication fails.

Migration of existing records requires either an explicit deployment-owner assertion of single-customer history or a host-confirmed mapping keyed by **internal conversation ID** to TenantID. Missing, duplicate/conflicting, or unknown mappings stop backfill. An anonymous cookie is not evidence of historical organization membership. Actual 0008 application needs a stop-write window; actual 0009 application needs evidence old Profile clients have retired. Prepare code/runbooks before requesting these external facts; do not execute irreversible data changes by assumption.

## File ownership and dependency order

Tasks execute in order. Task 1 owns identity/config authentication; Task 2 owns new domain and persistence schema; Task 3 owns bundle and snapshot protocols; Task 4 owns catalog/bootstrap; Task 5 owns tenant conversation persistence; Task 6 owns runtime capacity/factory; Task 7 owns chat application integration; Task 8 owns HTTP/Fx; Task 9 owns frontend; Task 10 owns staged migration tooling and final evidence. Only Tasks 1/6 touch runtime configuration, in sequence. Phase 1A owns `pi` and its policy internals; consume its final public API rather than creating a second policy/inspection implementation.

### Task 1: Trusted principal boundary and explicit anonymous deployment

**Files:** Create `application/identity/principal.go`, `infrastructure/middleware/principal.go`, `infrastructure/middleware/principal_test.go`, `config/identity.go`; modify `config/config.go`, `config/validate.go`, `config.example.json`, `infrastructure/driver/gingext/gingext.go`, existing visitor tests.

**Interfaces:** New application contracts (context keys private to the package):

```go
type Role string
const (RoleUser Role = "user"; RoleAdmin Role = "admin")
type Principal struct { TenantID, UserID string; Role Role }
func WithPrincipal(context.Context, Principal) context.Context
func FromContext(context.Context) (Principal, bool)
func (p Principal) RequireAdmin() error
// Implemented by the embedding host; no raw-header implementation supplied.
type Authenticator interface {
    Authenticate(*http.Request) (Principal, error)
}
```

- [ ] Add table-driven middleware tests: verified user and admin; absent/invalid host credential; forged `X-Tenant-ID`/`X-Role` ignored; malformed or empty IDs; anonymous visitor becomes `RoleUser` in configured tenant; host failure never falls back to Visitor. For an authenticated test request, assert `identity.FromContext` equals the adapter result even when the body and headers claim another tenant.
- [ ] Run `go test ./infrastructure/middleware ./config -count=1`; new missing symbols/behavior must fail before implementation.
- [ ] Add explicit configuration `{ "identity": { "mode": "anonymous", "tenant_id": "default" } }` to the example with Go comments explaining this is a deployment choice. Supported modes are `anonymous` and `host`; unknown mode, empty anonymous tenant, and missing host adapter fail startup. In anonymous mode retain existing Redis-backed visitor issuance then attach trusted fixed TenantID plus its UserID. In host mode call only the supplied Authenticator. Use returned errors to issue 401/403 without logging credentials. Health/static routes need no new anonymous session; protect application pages/API with principal middleware.
- [ ] Test route capability metadata derives from server enablement **and** `RoleAdmin`; direct service admin calls also invoke `RequireAdmin`. Verify no Header/body field can grant role.
- [ ] Rerun the targeted tests and review the host integration contract. Commit these files only after tests pass.

### Task 2: Agents and immutable version persistence, additive 0007

**Files:** Create `domain/entity/agent/agent.go`, `domain/entity/agent/version.go`, `domain/repository/agent/repository.go`, `infrastructure/persistence/agent/{repository,repository_test,migration_test,repository_integration_test}.go`, `migrations/0007_agent_Agents.{up,down}.sql`; modify `infrastructure/persistence/register.go` only for providers.

**Interfaces:** `agent.Agent` has all §4.1 fields, `Presentation{Icon,Welcome string; Starters []Starter; Order int}`, and `Starter{Title,Prompt string}`. `agent.Version` has all §4.2 fields with `json.RawMessage` for persisted snapshots/evidence and nullable source ID. `agentrepo.ListQuery{TenantID,Keyword,Cursor string; Limit int; IncludeArchived bool}`, `ListPage{Items []agent.Agent; NextCursor string; DefaultAgentID *string}`; repository methods:

```go
type Repository interface {
    Find(context.Context, string, string) (agent.Agent, error) // tenant, agent
    List(context.Context, ListQuery) (ListPage, error)
    FindVersion(context.Context, string, string, string) (agent.Version, error)
    ListVersions(context.Context, string, string, uint64, int) ([]agent.Version, error)
    FindBootstrap(context.Context, string, string) (agent.Agent, error)
    CreateInitial(context.Context, agent.Agent, agent.Version) error
    UpdatePresentation(context.Context, agent.Agent, uint64) error // expected row_version
}
```

- [ ] Write SQL-mock tests asserting tenant is in the initial SELECT, version requires tenant+agent+version, initial creation rolls back both rows on pointer failure, and all list queries use bounded limits. Add real-MySQL tests for duplicate seed vs two NULL bootstrap keys, cross-agent active version rejection, duplicate version integer, and duplicate non-NULL source session ID.
- [ ] Run `go test ./infrastructure/persistence/agent -count=1`; expect missing schema/repository failures.
- [ ] Implement 0007 with explicit names and indexes from §10.1. `agents` is created before `agent_versions`; add `(tenant_id,id,active_version_id)` FK after both exist against version `(tenant_id,agent_id,id)`. `agent_versions` references `(tenant_id,agent_id)` on agents. Add `tenant_id`, `agent_id`, `agent_version_id` as nullable Conversation columns; `conversation_type NOT NULL DEFAULT 'chat'`, `follow_latest NOT NULL DEFAULT TRUE`. No active-training/source-training FK yet. Keep existing `profile_code` and old unique index. Match existing identifier lengths/collations so composite FKs are legal; business IDs use existing 32-character IDs, tenant/user up to 128 characters, and identifier comparisons use a consistent case-sensitive collation.
- [ ] Implement `CreateInitial` in one transaction: insert Agent with null active pointer; insert immutable v1; set pointer and increment row_version using expected state. Repository exposes no version UPDATE method. `UpdatePresentation` updates only name/description/presentation/status, uses row_version CAS, and checks `active_training_session_id IS NULL` before archiving. Lists sort extracted numeric presentation order then ID; compute default over the full eligible tenant set independently of current search/page.
- [ ] Add bounded descending version pagination at storage level (`version < before`, newest first, limit+1); the service signs and validates its cursor in Task 4.
- [ ] Run unit tests and real MySQL tests in a disposable schema. Document server version and exact outcomes. 0007 down removes added FKs before dropping tables/columns and never deletes Git assets. Commit only this task's files.

### Task 3: Immutable snapshot schema, digest protocol and Git materialization

**Files:** Create `application/service/agentversion/{snapshot,digest,snapshot_test,digest_test}.go`, `application/port/agentbundle/bundle.go`, `infrastructure/driver/agentbundle/{git,materialize,inspect,git_test,materialize_test}.go`; modify `config/platform.go` for reference resolution, with `config/agent_snapshot_test.go`.

**Interfaces:** Define `Snapshot{SchemaVersion int; Model ModelConfig; Tools ToolPolicy; Runtime RuntimeConfig}`. `ModelConfig` stores effective protocol/base URL/model ID/reasoning/capabilities and `SecretRef`, never APIKey; `ToolPolicy` stores exact registered implementation references, effective args and MCP configuration references; `RuntimeConfig` stores effective limits, compaction, sandbox/policy and tool flags. Schema version 1 uses explicit fields and strict decoders, including duplicate-key rejection before unmarshalling. `Entry{Path,Mode string; Size int64; ContentSHA256 string}` uses required lower-case JSON names.

```go
func ParseSnapshot([]byte) (Snapshot, error)
func BundleDigest([]Entry) (string, error)
func SpecDigest(string, Snapshot) (string, error)
type BundleRef struct { Commit, Tag, Digest string }
type Store interface {
    CreateInitial(context.Context, string, string, string, string) (BundleRef, error)
    // tenant, agent, version, source directory; destination paths derived internally
    Verify(context.Context, string, string, BundleRef) error
    MaterializeVersion(context.Context, string, string, string, BundleRef) (string, error)
    MaterializeChat(context.Context, string, string, string, string, BundleRef) (string, error)
    // tenant, agent, conversation, version; returns isolated WorkDir
}
```

- [ ] Create digest fixtures checking RFC 8785 key ordering (including non-ASCII/UTF-16 order), canonical numbers, array order, duplicate/unknown fields, missing-vs-null, effective defaults, executable mode, raw newline bytes, relative symlink bytes, and tree UTF-8 byte path order. Assert ordinary JSON object ordering is insufficient using a fixture where JCS order differs. Include expected canonical bytes and known SHA-256 values generated independently with an RFC 8785 conforming implementation.
- [ ] Run `go test ./application/service/agentversion ./infrastructure/driver/agentbundle ./config -count=1`; verify failures before implementation.
- [ ] Implement strict snapshot schema and conforming JCS via a maintained implementation verified with RFC test vectors; inspect license/API before adding its exact version to go.mod/go.sum. Resolve and freeze defaults only at creation. Resolve `platform:<configured-id>` SecretRef to current secret at runtime, while using the saved protocol/model/base URL/parameters. Exact tool implementation IDs must map to server-owned implementations; missing or changed incompatible implementation returns a specific unavailable-reference error. Never copy unrestricted host MCP `CWD` or credential bodies into a snapshot.
- [ ] Implement Git operations via `exec.CommandContext` argument arrays, controlled Git environment and literal server-derived paths. Validate IDs as single safe path components, reject reserved path components and escaping/special-file trees, inspect the complete tree using Phase 1A's read/diagnostic API plus the spec's Bundle checks. Exclude only platform metadata, not arbitrary .gitignore matches. Record commit/tag/content digest; Git OID is not the digest. Reject dirty/untracked version sources instead of silently hashing only tracked files.
- [ ] Materialize through temporary sibling directory and rename only after verification. No writable hard links. Root/behavior files read-only; chat `scratch` and `.tmp` writable under a dedicated per-conversation copy. Restrict OS processes with Phase 1A so read boundaries also prevent access to sibling caches and host data. Ensure `.tmp` and `scratch` were not supplied by Bundle content. Verify before exposing the new directory; on failure retain existing published directory. Check the version digest on load, not just at creation.
- [ ] Test two copies cannot read each other's scratch through native process boundaries; edit of shared template changes neither copy; corrupt commit/missing blob/symlink escape fails. Pure permissions tests do not replace the actual Phase 1A OS sandbox probe.
- [ ] Run targeted tests and commit. Do not implement training checkpoints or publication endpoints in this task.

### Task 4: Catalog, template assembly, bootstrap and initial creation

**Files:** Create `application/service/agentcatalog/{service,template,bootstrap,create,cursor,service_test,bootstrap_test,cursor_test}.go`, `common/dto/agent.go`, `common/vo/agent.go`; modify `infrastructure/driver/agentprofile/catalog.go` only to expose template source information if needed.

**Interfaces:** `agentcatalog.Service` consumes identity, `agentrepo.Repository`, `agentbundle.Store`, server snapshot resolver and the existing immutable Profile catalog. Public methods:

```go
List(context.Context, identity.Principal, dto.ListAgentsQuery) (*vo.AgentPageVO, error)
Get(context.Context, identity.Principal, string) (*vo.AgentVO, error)
Templates(context.Context, identity.Principal) ([]vo.AgentTemplateVO, error)
Create(context.Context, identity.Principal, dto.CreateAgentDTO) (*vo.AgentVO, error)
Patch(context.Context, identity.Principal, string, dto.PatchAgentDTO) (*vo.AgentVO, error)
Versions(context.Context, identity.Principal, string, dto.ListVersionsQuery) (*vo.AgentVersionPageVO, error)
Bootstrap(context.Context, string, string) error // tenantID, trusted operator ID
```

DTO creation takes `template_code`, name/description/presentation overrides and allowed model/tool selections; does not accept TenantID, created_by, filesystem paths, digest or arbitrary tool binaries. Patch takes expected_row_version plus spec-permitted optional fields and rejects empty patches. VOs expose presentation/state/selectable and safe version metadata, never credential bodies. Page returns `items`, `next_cursor`, nullable `default_agent_id`.

- [ ] Test two Agents from same template get different IDs and NULL bootstrap keys; repeated bootstrap yields exactly eight seeded Agents and eight initial versions; failure after Git preparation retries without exposing incomplete Agent; template modification leaves existing Agent unchanged. Test cross-tenant lookup and ordinary-user writes fail.
- [ ] Run `go test ./application/service/agentcatalog -count=1` and observe failures.
- [ ] Assemble each seed's root AGENTS.md from generic base plus that profile's instructions. Copy its own skills and the shared skills actually referenced by those instructions/skills. Build an explicit, reviewed dependency map for all eight template codes from current Markdown references; reject unresolved dependencies and duplicate destinations. Relocate profile `references/x.md` to `documents/x.md`, each skill `templates/x` to that skill's `assets/x`, rewrite local references in AGENTS/Skills/documents, and assert they resolve inside the resulting Bundle. Do not runtime-inject original Profile instructions afterward.
- [ ] Bootstrap key is the built-in code; template_code is provenance, not unique. On seed already complete, verify referenced assets and skip; do not overwrite changed Agent presentation. Serialize bootstrap per tenant in this single-process deployment and rely on DB unique keys. Use stable prepared IDs/recovery metadata on disk to recover preparation before DB commit; after commit DB is authoritative. Remove only verified unreferenced task-owned preparation artifacts.
- [ ] Add the shared validation defaults from Global Constraints to config.example.json and strict config validation. Create initial validation evidence with head, digest_version, bundle/spec digests, validator_version, validation_policy_digest, checks/diagnostics/smoke summary, database-UTC validated_at/valid_until, and `review_mode=manual`; initial evidence is not capped by a training deadline. Policy digest covers effective limits, pinned interpreter/dependency list, sandbox/smoke environment, timeout and evidence TTL using the same JCS protocol. Run the same complete structural/script/reference validator that Phase 1C will reuse: 1 MiB per file, 32 MiB/2,000 paths total, 256 KiB per SKILL.md, only referenced approved .sh/.py scripts, no network or business side effects. Empty interpreter allowlist rejects script-containing templates. Do not label this model-quality evaluation. Build version 1 from allowlisted effective snapshot and verified files before `CreateInitial`. Management calls recheck role/tenant even if HTTP middleware ran.
- [ ] Sign opaque cursors using a configured server cursor secret, HMAC-SHA256 and strict bounded decoding. Include cursor kind, tenant, agent for version pages, and last version; list cursors also bind search/status filters and order/ID. Reject malformed/cross-context cursors; version pages use `version < last_version`, so new higher versions never duplicate prior items. Limit defaults 20, max 100, nonpositive explicit values rejected. Do not reuse current unsigned chat cursor as a security token.
- [ ] Run catalog and bundle tests; record eight-template dependency/inspection results. Commit this task.

### Task 5: Tenant-owned conversation binding from storage through history

**Files:** Modify `domain/entity/conversation/conversation.go`, `domain/repository/conversation/{conversation,management}.go`, `infrastructure/persistence/conversation/{repository,management}.go` and their existing tests, `conversation/{store,runner}.go` and tests, `common/{dto,vo}/chat.go`; create `application/identity/owner.go`.

**Interfaces:** Introduce `identity.Owner{TenantID,UserID string}` and propagate it explicitly rather than hidden SQL context. Rename both repository lookups to `FindOwned(ctx, owner, conversationID)`. `AppendTurn(ctx, owner, conversationID, expectedVersion, messages, invocations)` preserves existing atomic transaction. List/Message queries contain Owner and mandatory conversation type. Rename/Delete accept Owner. Add `UpdateBoundVersion(ctx, owner, conversationID, oldVersionID, newVersionID string) error` with CAS and same-agent check. Add TenantID to the **application** `conversation.RunRequest`; `pi.RunRequest` stays unchanged.

- [ ] Extend repository SQL mocks and integration tests: identical user+public conversation IDs in different tenants remain separate; cross-tenant find/messages/append/rename/delete reject; training rows excluded from every normal-chat method; append cancellation still persists final messages with existing bounded timeout. Internal message IDs cannot bypass an owner join.
- [ ] Run `go test ./conversation ./infrastructure/persistence/conversation -count=1`; new tenant behavior must fail first.
- [ ] Add entity binding fields. Keep profile_code during compatibility. Creation writes all new fields; old NULL rows are readable only by the explicit migration compatibility path after authoritative mapping, never inferred for arbitrary tenant requests. All normal repositories filter tenant+user+conversation_type='chat'. Keep agent_messages using internal conversation ID.
- [ ] Remove automatic missing-conversation creation from the Web runtime path. Retain the existing exact constructor `conversation.NewRunner(runtime pi.Runner, repository conversationrepo.IConversationRepository, historyLimit int, limits governor.Limits) conversation.Runner`; Task 7 calls it once per admitted execution with a lease Runner. History/mapping/atomic persistence remains in this package. If a non-Web caller needs lazy creation, give it an explicit legacy constructor/option and test it is not wired into server Fx.
- [ ] Add `agent_id`, `agent_version_id`, `follow_latest`, tenant and conversation type to persistence; `ConversationVO` includes safe Agent name/icon/status/presentation from a tenant-checked join/batch lookup. Retain original Agent display for archived histories even when absent from selectable pages. Add a detail query independent of current list pagination for deep links.
- [ ] Run conversation/repository tests and all compile checks to identify remaining old signatures. Fix consumers and fixtures within this task; commit.

### Task 6: Bounded, leased per-conversation runtime manager

**Files:** Create `application/service/agentruntime/{manager,admission,factory,manager_test,admission_test,factory_test}.go`, `config/agent_runtime.go`; modify `config/{config,validate}.go`, `config.example.json`.

**Interfaces:** `Key{Kind,TenantID,AgentID,ConversationID,VersionID,OperationID,SpecDigest string}`; kind is `chat`, `training`, or `validation` (only chat execution wired now). `Request{Key Key; Version agent.Version}`; version is already tenant/agent checked by caller and rechecked against key. `Lease{Runner pi.Runner; Release func()}` uses idempotent Release. `Factory.Create(ctx, Request) (ManagedRuntime,error)`; `ManagedRuntime` provides `Run`, `Start(context.Context) error`, `Stop(context.Context) error`. `Manager.Acquire(ctx, Request) (*Lease,error)` and `Manager.Close(ctx) error`.

- [ ] Write deterministic fake-clock/factory tests for same-key reuse; different conversation IDs produce separate directories and processes; failed creation releases reservations; cancellation while waiting; configured acquire timeout; shutdown drains/stops; active runtime never evicted; idle TTL; ordinary global and per-tenant caps subtract reserved slots. Use blocking fake creation to prove concurrent creates reserve capacity **before** expensive factory work.
- [ ] Run `go test ./application/service/agentruntime ./config -count=1`; observe failures.
- [ ] Store creating/idle/leased entries under one admission mutex; reserve both global and tenant counts atomically. Same-key waiters share one creating promise, but a running chat lease cannot execute concurrently. Reclaim idle releasable entries before bounded wait; close them before releasing their occupied capacity. Queue same-class eligible requests FIFO, skipping a tenant whose quota is exhausted so it does not block another tenant. Training is ordinary quota; validation may use reserved plus ordinary capacity. No preemption. A stop failure keeps the slot quarantined/occupied until process death is confirmed.
- [ ] Validate exact spec defaults and limits, including positive reservations smaller than corresponding cap, tenant cap <= global cap, per-tenant reservation <= global reservation. A busy acquisition is a typed retryable error that HTTP maps to 503.
- [ ] Factory resolves saved snapshot and verifies spec digest without refilling current defaults. Use MaterializeChat, then `pi.New` with saved platform/tools/MCP options, restricted workspace and writable prefixes `scratch`, `.tmp`; Force `AllowWrite=false` for production chat and validation as required by §6.1; saved policy may restrict further but cannot enable built-in file mutation tools for those purposes. No host workspace paths, inherited arbitrary MCP CWD, or shared subprocesses. Call Start on successful creation, Stop on eviction/shutdown; rollback creation and files when Start fails. Static tools from Fx must be demonstrably stateless or constructed per runtime.
- [ ] Run manager tests under race detector and verify two actual sandboxed runtimes cannot reach sibling caches. Commit targeted files; do not modify Phase 1A policy internals.

### Task 7: Chat admission, fixed Agent identity and follow-latest loading

**Files:** Modify `application/service/chat/{service,run_manager,register}.go` and tests; create `application/service/chat/{binding,binding_test}.go`, `application/service/agentruntime/agent_lock.go`; modify `conversation/register.go` as needed to remove server singleton dependency.

**Interfaces:** Change public chat service methods to accept `identity.Principal`. Add `GetConversation(ctx, principal, conversationID) (*vo.ConversationVO,error)`. `CreateConversationDTO` contains AgentID plus deprecated ProfileCode, mutually exclusive; legacy code resolves only `(tenant, bootstrap_key)`. New conversations set follow_latest=true. Shared `AgentAdmission.With(ctx, tenantID, agentID string, fn func() error) error` serializes archive and run admission (Phase 1C later consumes the same lock).

- [ ] Write tests: explicit selected Agent binds active version; missing/archived/wrong tenant Agent rejects; both identity fields reject; legacy Profile maps only current tenant seed; detail restores original Agent; wrong owner/history type rejects; archive wins before admission and prevents a new turn; admitted turn completes after archive; cancel key includes tenant.
- [ ] Run `go test ./application/service/chat ./conversation -count=1`; observe failing behavior.
- [ ] Replace Profile context lookup/injection with catalog/immutable version binding. Under Agent admission and per-conversation active-run protection, verify enabled and version references; acquire a ready runtime lease before emitting run_started. Scope active-run keys by tenant, user, conversation. Reuse existing input/image validation and SSE listener/terminal persistence behavior.
- [ ] For follow_latest, compare DB active pointer on every turn. Build/verify the target runtime and atomically CAS conversation version only after it is ready; identity remains unchanged. If target load fails, leave prior binding untouched and report the load failure (do not silently run another Agent). If active pointer changed while preparing, release and retry a bounded admission attempt. Existing admitted turns finish on their lease. `follow_latest=false` retains version.
- [ ] Construct `conversation.NewRunner(lease.Runner, repository, historyLimit, savedLimits)` for the admitted execution; `historyLimit` is the configured existing conversation history limit and `savedLimits` is `governor.Limits` decoded from the validated immutable `RuntimeConfig` for that leased version; pass tenant ownership and history only through application request. Release in all terminal paths. Retain invocation tracing and accounting, but emit bounded template/code metadata rather than unbounded Agent ID metric labels; business span can carry IDs.
- [ ] Preserve rename/search/delete/image behavior. Deleting a running conversation must coordinate cancel and terminal persistence so a deleted row is not auto-recreated; match existing observable semantics and test explicitly.
- [ ] Run chat/conversation suite and race tests for concurrent start/cancel/archive/version preparation. Commit.

### Task 8: HTTP API, page guards and server lifecycle wiring

**Files:** Create `infrastructure/controller/http/agent/{controller,controller_test}.go`; modify `infrastructure/controller/http/{register,register_test}.go`, `infrastructure/controller/http/chat/{controller,controller_test}.go`, `infrastructure/controller/http/page/{controller,controller_test,renderer}.go`, `cmd/server/app.go`, server/infrastructure registration tests.

**Interfaces:** Register user GET `/api/v1/agents`, `/api/v1/agents/:agentID`; GET `/api/v1/conversations/:id` for authenticated deep-link restoration; retain existing conversation endpoints. Register POST/PATCH agents, GET templates and GET versions only when trusted host management capability is enabled, with per-request admin authorization. Do not register activate until Phase 1C can perform its complete revalidation and atomic switch.

- [ ] Add Gin tests using injected principals and fake services: anonymous management routes absent; ordinary host user denied management; admin tenant-scoped; unknown JSON keys rejected; cross-tenant IDs 404; missing auth 401; stale CAS 409; runtime busy 503; catalog empty 200 with null default. Host authentication page redirect remains delegated to host adapter/middleware.
- [ ] Run `go test ./infrastructure/controller/http/... ./cmd/server ./infrastructure -count=1`; expect missing routes/wiring failures.
- [ ] Add `/` redirect to `/agents`, `/agents` directory page and `/chat`. `/chat` without either ID redirects to directory. If conversation_id exists, load owned detail first; compare optional agent_id and reject mismatch. Invalid agent or unavailable direct selection renders a clear target error with return link, not a substitute Agent. Archived owned conversation renders read-only.
- [ ] Wire repo, catalog, bundle store, snapshot resolver, manager and factory through Fx. Replace server `newApp` singleton pi.Agent provider/newAgentRunner; lifecycle owns Manager.Close. Do not instantiate all Agents or read all Bundle directories to answer list requests. Run startup preflight for config/storage/host auth; bootstrap is explicit provisioning, not an implicit scan/seed for every tenant on each GET. CLI `pi.New` behavior remains under its existing assembly.
- [ ] Retain compatibility `/agent-profiles` only behind the explicit compatibility setting and return tenant seed projections; no cross-tenant global default. Register its removal with 0009 rollout. Expose safe environment/customer label and management availability in page bootstrap data, not secrets.
- [ ] Run route/Fx tests plus `go test ./cmd/... ./infrastructure/... -count=1`. Commit after confirming the new dependency graph starts with test factories.

### Task 9: Agent directory and explicit chat target restoration

**Files:** Create `frontend/templates/pages/agents.html`, `frontend/static/{js,css}/pages/agents.{js,css}` (JS at `frontend/static/js/pages/agents.js`, CSS at `frontend/static/css/pages/agents.css`), `frontend/static/js/pages/agent-navigation.js`, `frontend/static/js/pages/agent_navigation_test.mjs`; modify `frontend/templates/pages/chat.html`, `frontend/templates/components/conversation-sidebar.html`, `frontend/static/js/pages/chat.js`, `frontend/static/css/pages/chat.css`, renderer tests.

**Interfaces:** Pure module `resolveChatTarget(search)` returns `{agentID,conversationID}` or a validation error; `assertConversationAgent(conversation, agentID)` rejects conflict. Directory consumes AgentPageVO with pagination and optional recommendation. Chat consumes Agent detail or owned conversation detail independently of list pages.

- [ ] Write Node tests against navigation/state helpers: absent target requests directory; single Agent never auto-selects; deep link loads details not first list item; mismatched IDs reject; archived history read-only; empty list is a valid state; fetch failure is distinct; target disappearing before creation preserves unsent user input and never selects another Agent. Keep existing image/stream test fixtures running.
- [ ] Run `node --test frontend/static/js/pages/*_test.mjs`; navigation tests fail first.
- [ ] Build directory cards with icon/name/duties and explicit “开始聊天” link to `/chat?agent_id=...`; add search, loading/error/empty states and load-more pagination. Recommendation uses default_agent_id purely as display; fetch its detail if absent from current page. No creation POST on page load or click. Render user text via textContent/template escaping, not HTML concatenation.
- [ ] Initialize chat target from URL/server bootstrap. Show target presentation while conversation is still absent. Only send handler invokes CreateConversation with agent_id; after success replace URL with conversation_id, then start run. Preserve selected images/content across a creation error; refresh target availability without resending automatically. For historical URL load detail first, then messages and paginated sidebar independently.
- [ ] Replace default-profile fallback paths (initial load, delete current chat, new chat, switch). “切换Agent” returns to directory and clears local unsent attachment state through existing cancellation/navigation handling; it does not move messages or server files. Sidebar uses ConversationVO Agent fields even for archived or off-page Agents. Preserve rename/delete/history search/image rendering/SSE cancellation.
- [ ] Verify browser flows with fake provider and seeded test tenant: directory search/page, choose Agent, first-message-only creation, refresh, direct history URL, conflict, cross-tenant rejection, archive read-only, empty tenant, image turn and switch. Capture evidence using available browser skill; do not claim browser verification from Node tests alone.
- [ ] Run frontend tests and page renderer tests. Commit.

### Task 10: Backfill tooling, staged 0008/0009 and Phase 1B gate

**Files:** Create `cmd/agent-migrate/main.go`, `application/service/agentcatalog/{backfill,backfill_test}.go`, `migrations/0008_agent_conversation_ownership.{up,down}.sql`, `migrations/0009_retire_profile_code.{up,down}.sql`, `docs/agent-Agents-migration.md`; add migration integration cases in agent and conversation persistence test files. Remove production Profile field reads only in the final compatibility-retirement code stage.

**Interfaces:** Operator command modes `check`, `bootstrap`, `backfill`, `verify`; flags `--tenant-id`, `--confirmed-single-tenant-history`, `--mapping-file`, `--dry-run`. Backfill mapping is JSON array `[{"conversation_id":"<internal row id>","tenant_id":"<trusted tenant>"}]`; reject unknown fields and conflicting entries. Output counts/IDs/reasons only, no messages or credentials.

- [ ] Add tests against a disposable MySQL database: nullable 0007 fixture with all eight legacy profile codes; unknown code; no trusted ownership evidence; partial prior backfill; cross-tenant/version mismatch; failed Git digest; repeated bootstrap/backfill; 0008 NOT NULL/FK rejection; cross-tenant duplicate owner IDs causing 0008 down refusal without any deletion.
- [ ] Run `go test ./application/service/agentcatalog ./infrastructure/persistence/agent ./infrastructure/persistence/conversation -count=1`; distinguish configured integration execution from skips.
- [ ] Implement dry-run planning first. Resolve each legacy row's tenant from the explicit confirmed source and its profile from that tenant's bootstrap_key. Resolve the initial complete version, verify Git/bundle/spec digests and same-agent ownership, then update only still-null binding fields with compare-and-swap. Already populated consistent rows are skipped; inconsistent rows stop, not overwritten. Audit all rows after backfill, including fully populated rows.
- [ ] Write 0008 scripts/preflight with individually resumable schema operations based on information_schema. SQL DDL is not transactional. Require stopped writers and zero invalid/null rows. Create `uq_agent_conversations_tenant_owner(tenant_id,user_id,conversation_id)` before dropping `uq_agent_conversations_owner`; set ownership columns NOT NULL and add same-tenant/same-agent FKs/indexes. down first checks duplicate old owner keys and signals failure if present; otherwise restore old unique index, then remove new index/FKs/NOT NULL changes. Never delete conflicting tenant data.
- [ ] Keep 0009 out of the ordinary automatic migration path until compatibility retirement is explicitly recorded. Verify no running binary/clients depend on profile_code; apply code release that stops selecting/writing/filtering it, then remove column/legacy profile indexes. down restores only schema/nullable provenance where recoverable; it cannot recover original Profile choices for arbitrary newly created Agents, and runbook must state this rather than invent mappings.
- [ ] Write exact rollout sequence: backup DB+Git; 0007; deploy compatibility code; explicit per-tenant bootstrap; ownership-backed dry-run/backfill/verify; stop writers; 0008; restart agent_id frontend; observe/retire old clients; final code and 0009. Include schema checkpoint/resume checks, failed-preparation cleanup and backup references. Fresh databases follow the same bootstrap/verification gate; 0007 through 0009 cannot be blindly applied before bootstrap.
- [ ] Run final verification once implementation is settled:

```bash
go test ./... -count=1
go test -race ./application/service/agentruntime ./application/service/chat ./conversation -count=1
node --test frontend/static/js/pages/*_test.mjs
git diff --check
```

Run real MySQL migration tests using the existing MYSQL_TEST_* environment against an isolated disposable database, and run the native sandbox/browser checks from Tasks 6/9. Record failures/skips accurately, plus externally unavailable host login and unexecuted production cutover separately. Commit reviewed task files; do not execute a production cutover or assert actual tenant mapping without supplied deployment evidence.

## Self-review and phase handoff

- [ ] §1/§3: Tasks 1, 5, 7, 8 cover tenant/user authority, ordinary anonymous mode and per-request admin checks; external host login protocol is explicitly missing, not fabricated.
- [ ] §4.1–4.2: Tasks 2–4 cover all fields, immutable full snapshots, secrets, complete digest protocol, seeds and duplicate-template creation. §4.3 remains Phase 1C.
- [ ] §5: Tasks 3 and 6 cover Git-backed full snapshots, per-conversation caches, sandbox inheritance, temporary directories and all process/tenant quota rules.
- [ ] §9.1: Tasks 4/8 deliver catalog/create/update/template/version-list APIs. Activate is explicitly Phase 1C because partial activation would bypass its publication safety requirements.
- [ ] §9.3: Tasks 8/9 cover directory, explicit first-message creation, historic binding, conflict/empty/archive/error states and retained chat functions; complete admin workstation stays Phase 1D.
- [ ] §10.1: Tasks 2/5/10 implement staged compatibility, trusted historical ownership, 0008 safe rollback and conditional 0009. No third table before Phase 1C/0010.
- [ ] §11: Record tests proving no history/file sharing, protected read-only production files, default-null empty catalog, idempotent bootstrap, digest stability and quota fairness. Record exact host/MySQL/OS/browser integration limits rather than equating mock success with rollout success.

The user must first review the complete product and technical design and explicitly approve development. Until then, this plan is review material only. After approval and the Phase 1A gate, implementation can proceed task-by-task. Phase 1C consumes these concrete contracts and must not assume stub endpoints exist.
