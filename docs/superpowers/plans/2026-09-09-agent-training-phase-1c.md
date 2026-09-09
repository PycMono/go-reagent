# Agent Training Phase 1C Implementation Plan

**Approval status:** Awaiting explicit user review and approval. Development is paused; this is review material, not authorization to execute.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add administrator-owned training conversations, recoverable candidate checkpoints, complete validation, manual publication, historical activation, and bounded recovery without a new event platform.

**Architecture:** A single durable operation slot in each training row serializes mutations, with short Agent admission locks and transactional CAS at entry/completion. Authors run in isolated writable candidate worktrees; validation runs on frozen read-only copies through the Phase 1B runtime capacity manager. Git provides immutable checkpoint history and files; MySQL atomically records version activation and training result.

**Tech Stack:** Go 1.26, existing Gin/Fx/GORM/MySQL, Git CLI, native Seatbelt/Bubblewrap business process boundary, existing pi tools/history/invocations, Phase 1A/1B contracts.

**Spec:** `docs/superpowers/specs/2026-09-04-agent-self-training-design.md`, §§4.3, 5.3, 6.3, 7, 8, 9.2, 10.1 and 11. Prerequisite plan: `docs/superpowers/plans/2026-09-09-agent-Agents-phase-1b.md`. This plan starts after its acceptance gate and does not assume proposed Phase 1B signatures already exist in the checkout.

## Global Constraints

- “第一版新增三张业务表”：0010 adds only `agent_training_sessions`; no checkpoint/event/job/idempotency/outbox tables.
- “所有操作通过单进程的 Agent 级准入互斥与数据库 CAS 设置持久操作槽”；do not hold Agent mutex across an entire author Turn.
- “训练默认禁网策略在 Phase 1C 业务执行边界实现；Phase 1A 不扩展网络策略接口。”
- “第一版不向模型提供 publish、模型配置编辑、评测或训练其他 Agent 的工具。”
- “candidate_partial=true 时拒绝校验进入 ready”。
- `session_ttl_seconds=86400`, positive integer, fixed from database UTC creation time; ordinary queries/chat do not renew it.
- Reuse Phase 1B `approved_interpreters=[]`, `smoke_timeout_seconds=30`, `validation_ttl_seconds=1800`, complete version schemas and `digest_version=1` JCS protocol.
- Limits: 1 MiB/file, 32 MiB/2,000 Bundle paths, 256 KiB/SKILL.md; referenced .sh/.py only, fixed approved interpreters/dependencies, no runtime install.
- Publication requires explicit human confirmation: “本版本仅完成结构及脚本校验，未经自动质量评测”。
- Production ordinary-chat admission remains independent of ongoing training; archived Agents admit no new execution.
- No production deployment or real customer-data mutation is part of implementing this plan.

## Verified seams and decisions

`conversation/runner.go` currently appends user+assistant/tool messages only after runtime returns; `agent_messages` has unique `(conversation_id,turn_version,ordinal)` and durable `run_id`, while invocation rows already record run IDs. Training must persist its input **before** tools run. Add a dedicated training transaction adapter that reserves one turn and writes ordinal 0 at admission, then appends only outputs at completion; do not call ordinary `AppendTurn` twice or duplicate input in runtime history. An interrupted input remains a durable historical run-ID tombstone even when there are no outputs.

Phase 1B normal-chat repository methods reject `conversation_type=training`. Preserve that invariant. Training reads go through a separate admin-owner-authorized path; a shared HTTP messages endpoint can dispatch on a tenant-owned conversation only after checking the corresponding training row's admin identity. Normal list/create/run/delete/rename stay chat-only.

`application/tool/chat/register.go` currently contributes only the clock tool, but configured extra handlers, Exa/MCP and built-in research subagents are assembled elsewhere. Never use the entire production registry for an author or smoke runtime. Existing SDK sandbox policy reports network=allow and currently has no business deny-network hook. Therefore use application-owned author tools and a business process runner, with `pi.Options.AllowWrite=false`, `AllowExec=false`, `BuiltinSubagent=false`, no MCP, and only explicit safe `Tools`; do not reinterpret the SDK's ordinary exec as offline.

An actual candidate is a Git worktree and its `.git` control file plus parent directory entry must be protected from all author mutation, including rename/unlink. Application file tools allow only root AGENTS.md and the fixed Bundle directories (plus root .tmp), reject other root entries and traversal, and use rooted operations. The business OS runner mounts/permits those exact targets, denies network and access to `bundle.git`/state/siblings, protects `.git` and root directory mutation, and owns all descendants. A read-only `.git` chmod alone is insufficient. If a supported OS cannot enforce this boundary or prove descendants stopped, author execution fails closed; do not add a permissive fallback. The server's model API call stays outside the offline author subprocess boundary.

The Phase 1B shared validator must already perform initial-version checks, smoke, reference verification and bounded evidence. Task 6 factors/reuses that implementation rather than creating a second digest/checker. Initial-version and model-only publication use the same validator without a training expiry cap.

## Ownership and concrete shared contracts

Tasks run sequentially. Task 1 owns training domain/repository/schema; Task 2 owns candidate Git operations; Task 3 owns business author tools/processes; Task 4 owns admission and durable training messages; Task 5 owns training execution/restore/config; Task 6 owns validation; Task 7 owns publish/activate; Task 8 owns recovery; Task 9 owns API/Fx and acceptance. Each task updates only its named tests and contract dependencies. Existing Phase 1B files may be extended after its review gate; no concurrent `pi` changes are assumed.

Use aliases `identity`, `agent`, `training`, `agentrepo`, `trainingrepo`, `bundle`, `agentruntime`, `conversationentity` for corresponding packages from this plan/Phase 1B. `Scope{TenantID,AgentID,TrainingID,AdminUserID string}` is an application-authorized identity, never unmarshalled from a client. `Expected{RowVersion uint64; RequestID,RequestDigest string}` contains a digest calculated from strict normalized request content. `Operation` has ID, kind, request digest, state (`running`, `completed`, `interrupted`, `failed`), start/finish timestamps, bounded result and optional prepared version ID/tag/digests; it is one object, not a history array. `Session` has every §4.3 field. Status constants are exactly active, validating, ready, published, cancelled, stale, expired.

### Task 1: Third table, status invariants and transactional operations

**Files:** Create `domain/entity/agenttraining/{session,operation,session_test}.go`, `domain/repository/agenttraining/repository.go`, `infrastructure/persistence/agenttraining/{repository,repository_test,migration_test,repository_integration_test}.go`, `migrations/0010_agent_training_sessions.{up,down}.sql`; extend `config/agent_training.go`, `config.example.json` and persistence registration.

**Interfaces:** Repository exposes ownership-scoped reads and a narrowly typed transaction entry point. The callback runs against the same MySQL transaction, never an independently injected connection:

```go
type Repository interface {
    Find(context.Context, Scope) (training.Session, error)
    List(context.Context, identity.Principal, string, string, int) ([]training.Session, string, error)
    WithAgentTx(context.Context, string, string, func(Tx) error) error // tenant, agent
}
type Tx interface {
    NowUTC() (time.Time, error)
    AgentForUpdate() (agent.Agent, error)
    SessionForUpdate(Scope) (training.Session, error)
    CreateSession(training.Session, conversationentity.Conversation) error
    SaveSession(training.Session, uint64) error
    SetTrainingPointer(string, *string, uint64) error // expected current session, new pointer, agent row_version
}
```

- [ ] Write transition table tests asserting terminal states have no outgoing edges, ready editing clears evidence, publication only accepts ready and explicit confirmation, and expired timestamps alone do not release a running slot. Add MySQL tests for two live sessions rejected but two terminal rows allowed, cross-tenant/Agent pointer rejected, unique conversation/source IDs, and source NULL allowed for multiple initial/model-only versions.
- [ ] Run `go test ./domain/entity/agenttraining ./infrastructure/persistence/agenttraining ./config -count=1`; record failures before implementation.
- [ ] Add `active_slot` stored generated column: `CASE WHEN status IN ('active','validating','ready') THEN 1 ELSE NULL END`, unique `(tenant_id,agent_id,active_slot)`, CHECK legal statuses, unique `(tenant_id,agent_id,id)` and conversation ID. Add composite Agent active-training FK, version source-session FK and same-tenant/base-version/conversation constraints where valid indexes exist. Keep the training Conversation internal ID as the FK target, with API returning its public conversation ID. Do not add reverse training_session_id to Conversation.
- [ ] Implement NowUTC from DB, creation expiry as DB now+configured TTL, CAS increments for each session transition and pointer updates. Match Phase 1B ID collation/lengths. 0010 down removes new FKs first and never deletes Git refs; stop writes before operational rollback. Check stale slot/pointer mismatch fails new admission until recovery reconciles it.
- [ ] Run unit and real disposable-MySQL tests; commit only task files after passing.

### Task 2: Candidate worktrees, safe checkpoint history and restore

**Files:** Extend `application/port/agentbundle/bundle.go`; create `infrastructure/driver/agentbundle/{candidate,checkpoint,restore,candidate_test,checkpoint_test,restore_test}.go`.

**Interfaces:** Extend Phase 1B Store with `CreateCandidate(ctx, tenantID, agentID, trainingID string, base BundleRef) (string,error)`, `Checkpoint(ctx, Candidate, CheckpointMetadata) (Checkpoint,error)`, `ListCheckpoints(ctx,Candidate,cursor string,limit int) ([]Checkpoint,string,error)`, `Restore(ctx,Candidate,checkpointID string) (Checkpoint,error)`, `Diff(ctx,Candidate,maxBytes int) (Diff,error)`. `Candidate{TenantID,AgentID,TrainingID,Head string}`; `CheckpointMetadata{RunID,ActorID string; At time.Time; Partial bool}`; `Checkpoint{ID,Head string; Metadata CheckpointMetadata}`; `Diff{Text string; Truncated bool; ChangedPaths int}`.

- [ ] Add temporary-Git tests: a valid draft with semantic Skill error can checkpoint; binary executable/secret/escape cannot; .gitignore cannot hide files; root .tmp replacement is rejected; restoring older checkpoint keeps later immutable refs; other-session checkpoint rejects; symlink target bytes and executable modes affect digest; failed Turn checkpoint preserves Partial=true.
- [ ] Run `go test ./infrastructure/driver/agentbundle -count=1` and observe missing behavior.
- [ ] Create candidate from explicit base commit with platform-owned Git metadata. Use `refs/training/<trainingID>/head` plus never-reassigned `refs/training/<trainingID>/checkpoints/<checkpointID>`. Enumerate actual filesystem, apply safe-ingestion subset (type/path/size/secret/native-binary/reserved paths), and stage exactly the checked list through NUL-separated plumbing; do not `git add -A`. No semantic success required for a draft. Metadata stores only specified IDs/time/partial, no prompts or credentials.
- [ ] Handle checkpoint crash windows by writing immutable checkpoint ref before head ref; finish DB candidate_head CAS under the operation slot. Recovery can inspect operation target and refs without guessing that a dirty tree was committed. Dirty unsafe files stay in candidate for repair with bounded diagnostics and partial flag, never silently dropped.
- [ ] Restore only a ref enumerated under this session, after writer death confirmation. Materialize a replacement candidate from that commit through protected temporary directory, restore Partial metadata, invalidate validation, and CAS recorded HEAD. Preserve old directory for bounded diagnostic cleanup if swap fails; never run destructive restore while author processes hold files.
- [ ] Run tests and native filesystem attack cases; commit.

### Task 3: Offline author tools and process ownership

**Files:** Create `application/tool/agenttraining/{files,exec,registry,files_test,registry_test}.go`, `application/port/trainingprocess/process.go`, `infrastructure/driver/trainingprocess/{runner,seatbelt,bubblewrap,runner_test,process_test}.go`; extend `application/service/agentruntime/factory.go` and its tests for purpose-specific construction.

**Interfaces:** `trainingprocess.Policy{CandidateRoot,TempRoot string; WritablePaths []string; Deadline time.Time}`; `Supervisor.Start(ctx, Policy, []string) (Process,error)`, `Process.Wait(ctx) error`, `Process.StopAndConfirm(ctx) error`, `Supervisor.CloseAdmission()`. Author registry constructor `NewTools(root string, supervisor Supervisor) ([]ai.Tool,error)` builds explicit read/write/edit/apply_patch/controlled-exec tools; implementation may reuse rooted filesystem primitives only when they can enforce the fixed root-entry policy. `AuthorRuntime` bundles the pi runtime and supervisor; Stop closes admission before confirming death.

- [ ] Write registry tests proving no publish/config/other-agent/production write/MCP/subagent tools, no environment secret inheritance, and failed mutation through traversal/symlink/.git/root rename. OS tests execute network connect attempts, nested children and candidate writes; writes to AGENTS/skills and .tmp succeed, .git/bundle.git/state/other sessions fail.
- [ ] Run `go test ./application/tool/agenttraining ./infrastructure/driver/trainingprocess ./application/service/agentruntime -count=1`; confirm tests fail before implementation.
- [ ] Implement Linux deny-network namespace and restrictive bind mounts, and macOS deny-network Seatbelt profile with explicit writable literal/subpath targets, protected parents and platform metadata. Precreate protected directory roots. Restrict inherited environment and interpreter paths. Keep host model transport separate. Do not expose unrestricted SDK exec or stdio MCP as a fallback.
- [ ] Track all writable descendants using an enforceable OS lifetime boundary; reject detachment/breakaway mechanisms or contain them. Killing only the immediate PID is not proof. Stop returns a typed blocked error when death cannot be established, retaining runtime quota and operation ownership. Deadline is bounded by remaining training TTL and tool timeout. The actual capability probe must succeed on each supported OS; unsupported execution fails closed.
- [ ] Configure training Factory requests with Phase 1B training key and no cross-operation reuse: candidate workdir, explicit author Tools, builtins disabled, snapshotted model, ordinary tenant/global quota. Validation factory uses read-only copy and separate purpose; it never gets author Tools.
- [ ] Run native probes and race tests; report platform skips separately. Commit.

### Task 4: Session creation, durable input and bounded idempotency

**Files:** Create `application/service/agenttraining/{service,create,admit,messages,create_test,admit_test,messages_test}.go`, `infrastructure/persistence/agenttraining/turn.go`, associated tests; extend training repository Tx and common DTO/VO packages.

**Interfaces:** `Create(ctx, principal, agentID string, expectedAgentRowVersion uint64) (SessionVO,error)`; `Get(ctx,principal,trainingID string) (SessionVO,error)`. Extend Tx with `BeginTurn(scope Scope, runID string, input conversationentity.Message) (uint64,error)` and `FinishTurn(scope Scope,runID string,turnVersion uint64,outputs []*conversationentity.Message,invocations []*conversationentity.ModelInvocation) error`; both execute within the operation transaction. Training repository also exposes `HasRun(ctx,scope,runID) (bool,error)` and `History(ctx,scope,beforeTurn uint64,limit int) ([]*conversationentity.Message,error)`.

- [ ] Test create with stale Agent.row_version returns conflict and authorized active ID; repeated old create cannot make another session; concurrent same Agent training rejected; different Agents allowed. Test pre-execution crash leaves one durable input/run_id; same current ID+same normalized digest returns status; same ID+different payload conflicts; historical ID from stored input never reexecutes after slot overwritten.
- [ ] Run `go test ./application/service/agenttraining ./infrastructure/persistence/agenttraining -count=1`; observe failures.
- [ ] Under Phase 1B AgentAdmission and WithAgentTx verify principal admin, tenant, enabled, base and active pointer/expiry; create Session and type=training Conversation together and set pointer CAS. Persist initialization operation before Git creation; materialization failure cancels and releases only after process absence; restart reuses same recorded session ID.
- [ ] Run admission transaction persists operation running plus input at new conversation turn_version ordinal 0 before returning permission to start runtime. Increment conversation version once at BeginTurn; FinishTurn appends outputs at ordinal 1 onward and invocations exactly once, scoped by session operation/run ID and same reserved turn. History for model excludes this current turn; pass its input once as pi.RunRequest.Input. Keep ordinary chat AppendTurn unchanged.
- [ ] Store request digest using strict normalized request and shared JCS. On historical run ID, return saved status where provable or explicit 409 cannot replay; no new execution. Expected session row_version gates historical restore/config requests after slot replacement. Bound operation result fields and messages independently; no operation history array.
- [ ] Add training-only message reads with tenant+admin+training+conversation checks. Existing `/conversations/:id/messages` dispatches through this service for training; normal create/list/rename/delete/run reject training. Reuse unfiltered message mapping for training so file reads/tool outputs needed for review are not removed by ordinary chat visibility filtering.
- [ ] Run tests/race tests and commit.

### Task 5: Author Turns, partial candidates, restore and config changes

**Files:** Create `application/service/agenttraining/{run,checkpoint,restore,config,run_test,restore_test,config_test}.go`; extend `application/service/agentruntime` purpose-specific factory tests.

**Interfaces:** `Run(ctx,principal,trainingID string, dto.TrainingRunDTO) (OperationVO,error)`; DTO has run_id, expected_row_version, content and existing image input shape. `Restore(ctx,principal,trainingID,checkpointID string, dto.TrainingMutationDTO) (SessionVO,error)`; mutation has request_id and expected_row_version. `PatchConfig(ctx,principal,trainingID string,dto.TrainingConfigDTO) (SessionVO,error)` accepts allowed model selection/parameters only, not tool privilege expansion. Read endpoints `Diff` and `Checkpoints` always authorize Scope.

- [ ] Test ready→active clears evidence before any writable tool starts; concurrent run/restore/config conflicts; failed but safe changes produce partial checkpoint; unsafe changes remain dirty/partial and uncommitted; successful repair clears partial only after ingestion/residual checks; restore gets checkpoint's partial flag. Verify cancelled model output still persists with bounded uncancelled finalization context.
- [ ] Run `go test ./application/service/agenttraining -count=1`; observe failures.
- [ ] Admit and persist input via Task 4, acquire a training lease using operation key, load prior history, call pi runner directly with mapped history/current input. Persist outputs/invocations through FinishTurn rather than ordinary runner. Close author tool scheduling, stop/confirm all writers, then inspect/checkpoint; update operation result/head/partial and invalidate evidence in final CAS. No success result or released slot before writer death.
- [ ] Preserve safe partial checkpoint on failed/cancelled Turn; unsafe file diagnostics identify path/category without secret body. Repeated successful repair must validate the whole actual tree, not merely latest diff, before clearing partial. Resume only from active and no live operation.
- [ ] Restore and config operations use the same durable slot and request digest; ready first invalidates evidence. Restore stops writers then uses Task 2; configuration strict-decodes allowlisted effective model settings into saved candidate snapshot and recalculates digest. Neither client nor model may widen tool/sandbox/interpreter policy.
- [ ] Bind each run's context deadline to database-derived remaining TTL; completion rechecks database UTC. Expired completion delegates to Task 8 and cannot write successful ready/published states.
- [ ] Run tests and commit.

### Task 6: Frozen validation and evidence binding

**Files:** Create `application/service/agenttraining/{validate,validate_test}.go`; factor Phase 1B shared checks into `application/service/agentversion/{validate,policy,evidence,validate_test,policy_test}.go`; create business smoke runner tests under trainingprocess.

**Interfaces:** `agentversion.Validator.Validate(ctx, ValidationRequest) (Evidence,error)`; request contains tenant/agent IDs, BundleRef, effective Snapshot, purpose key, optional training expiry and a fixed effective policy. `Evidence` has head/digest_version/bundle_digest/spec_digest/validator_version/validation_policy_digest/check results/diagnostics/smoke/validated_at/valid_until/review_mode. `Validate(ctx,principal,trainingID string,dto.TrainingMutationDTO) (OperationVO,error)` controls session transitions.

- [ ] Test dirty actual tree vs recorded HEAD, partial candidate, changed config, expired session, model/secret/tool unavailable, unknown digest protocol, unknown/missing evidence field, policy changes and clock at exact valid_until. Smoke tests include no script allowlist, unreferenced script, forbidden extension, network attempt, timeout and unsafe file type.
- [ ] Run `go test ./application/service/agentversion ./application/service/agenttraining ./infrastructure/driver/trainingprocess -count=1`; observe new failures.
- [ ] Author admission is closed and all writers confirmed dead before freezing. Ingestion checkpoint occurs only according to explicit user mutation workflow; validation never auto-adopts an unexplained dirty tree. Reject candidate_partial, verify actual clean HEAD plus saved snapshot and digest, reserve validation capacity, then transition validating under durable slot/CAS.
- [ ] Build immutable validation copy and run shared Phase 1B checker/smoke with AllowWrite=false, only scratch/.tmp writable, no network, exact pinned interpreter/dependencies, fixed bounded environment and no business side effects. Reject model/tool/SecretRef unavailability with redacted reason. Initial-version validation uses this same code, without training expiry cap.
- [ ] Hash effective validation policy using same JCS version, including all check limits, interpreter/dependency references, smoke sandbox/environment, timeout and evidence TTL. At completion use DB UTC and `valid_until=min(now+TTL,expires_at)`; CAS head/config/row version and unexpired session. Success becomes ready, failure/interruption active with old evidence cleared. Always close validation runtime and confirm process death before releasing capacity.
- [ ] Run fixture parity tests proving initial/train validation calculate identical digests for identical files/config; commit.

### Task 7: Human publication and historical activation

**Files:** Create `application/service/agentversion/{publish,activate,prepare,publish_test,activate_test}.go`; create `infrastructure/persistence/agenttraining/publication.go` and tests; extend training Tx and bundle Store prepared-version methods.

**Interfaces:** `Publish(ctx,principal,trainingID string,dto.PublishTrainingDTO) (vo.AgentVersionVO,error)`; DTO has request_id, expected_row_version, human confirmation boolean and change_summary. `Activate(ctx,principal,agentID,versionID string,expectedAgentRowVersion uint64) (vo.AgentVO,error)`. `PreparedVersion{ID,Tag,BundleDigest,SpecDigest string; Version uint64}` persisted into operation before file writes. Tx exposes `CommitPublication(scope Scope,expectedSessionRowVersion,expectedAgentRowVersion uint64,version agent.Version,evidence Evidence) error` and repository exposes `FindPublishedByTraining(ctx,scope) (agent.Version,error)`.

- [ ] Test same source training produces one version under races/retries; published retries return result before expiry checks; stale base refuses; missing manual confirmation refuses; unchanged file+config refuses; dirty tree/ref failure invalidates to active; policy/TTL-only staleness triggers revalidation; validation capacity timeout preserves ready; expiry during preparation prevents commit.
- [ ] Run `go test ./application/service/agentversion ./infrastructure/persistence/agenttraining -count=1`; observe failures.
- [ ] In durable publish slot recheck admin, ready, base active pointer, no writer, clean tree, partial=false, recomputed digests and external references. Exact evidence bindings+current validator/policy and DB now strictly before both expiries permit reuse. Unknown protocol rejects without conversion. Dirty/missing/mismatched evidence resets active and requires explicit validation; only matching candidate with expired/policy-old evidence may revalidate automatically within the same publish slot. Acquire validation quota before ready→validating; busy preserves ready and completes failed operation for retry.
- [ ] Allocate monotonic version number under Agent lock/transaction, persist target version ID/tag/digests in operation before Git work. Prepare independently materialized read-only version and unique tag. Final transaction rechecks both CAS values, clean fixed candidate binding, fixed operation policy, base and DB expiries, inserts immutable version, switches Agent active pointer, sets session published/result_version_id and clears matching active pointer. Unique source_session guards permanent retry. Transport failure after commit does not undo publication.
- [ ] Compensate only this operation's unreferenced version directory/new tag; never delete reused commit or any referenced historical version. Inject failures before Git, after tag, after materialization, before commit, after commit/response; Task 8 recovery must settle every case.
- [ ] Activate verifies same-tenant/Agent historical row, immutable digest protocol/files/config and available references before CAS pointer change; never rewrites historical version/evidence. Refuse while a conflicting running training operation owns the Agent; idle training can become stale on its next admission. New chats use pointer; existing follow_latest is Phase 1B behavior. Lost activation response with stale CAS returns conflict/current pointer, not duplicate version.
- [ ] Run transaction/fault/race tests; commit.

### Task 8: Expiry, cancellation and restart reconciliation

**Files:** Create `application/service/agenttraining/{cancel,expire,recover,cancel_test,recover_test}.go`, `infrastructure/driver/agentbundle/reconcile.go`, `docs/agent-training-recovery.md`; extend Fx lifecycle startup/shutdown wiring only in Task 9.

**Interfaces:** `Cancel(ctx,principal,trainingID string,dto.TrainingMutationDTO) (SessionVO,error)`; `Recover(ctx) error` is server-owned, not an HTTP endpoint. `ReconcileScope(ctx,Scope) (SessionVO,error)` verifies pointer/status/refs under admission. Repository startup scan is tenant-scoped per batch with an explicit system recovery authority, not a user API accepting arbitrary tenants.

- [ ] Test running expiry keeps live slot/pointer until confirmed death; DB now==expiry rejects completion; repeated cancel never resets expiry; terminal published returns before expiry; interrupted run input is retained and never replayed; pointer/active-slot mismatch fails new training. Test DB committed publication with missing response repairs session result, DB uncommitted cleans prepared-only files and invalidates evidence.
- [ ] Run `go test ./application/service/agenttraining ./application/service/agentversion ./infrastructure/driver/agentbundle -count=1`; observe failures.
- [ ] Implement lazy expiry at detail/write/publish/create/archive/startup boundaries, plus live-operation context deadline. Signal stop immediately but retain nonterminal status/slot/pointer until all writer descendants are gone. Then transactionally mark expired/cancelled, clear evidence and clear pointer only if matching this session. Stop uncertainty reports blocked reason and does not admit replacement work.
- [ ] Require exclusive single-process data-directory ownership and old process-death verification before recovery. An advisory lock alone does not prove an orphan descendant died. Preserve unidentified live work and fail admission. Rebuild candidate from durable HEAD, retain uncheckpointed directory only for diagnostics, mark interrupted author/validator active with cleared evidence. No automatic rerun.
- [ ] Publication reconciliation first checks source version/result pointer: committed is authoritative, even after session expiry. Otherwise verify operation preparation records, clean only unreferenced artifacts, restore active/revalidation requirement. Validate Git refs and same-agent pointer invariants; missing object prevents activation instead of reading templates.
- [ ] Document bounded stop/recovery operations, candidate diagnostic retention, orphan cleanup and same-batch DB/Git backup requirements for Phase 1D drill. Run fault matrix tests; commit.

### Task 9: Training HTTP contracts, server registration and acceptance

**Files:** Create `infrastructure/controller/http/agenttraining/{controller,controller_test}.go`, `common/dto/agenttraining.go`, `common/vo/agenttraining.go`; modify HTTP/Fx registration, shared message controller dispatch, Agent archive admission and server lifecycle tests.

**Interfaces:** Register exactly §9.2 training routes and §9.1 historical activate when trusted management is enabled. Training API output includes conversation_id, status, base, head/digests/partial, row_version/expiry, bounded validation and current/recent operation. GET list/diff/checkpoints remains tenant+creating-admin scoped; metadata list cannot leak another administrator's training messages.

- [ ] Add HTTP tests for all write authorization/state errors, duplicate request semantics, training IDs from other tenant/admin, ordinary Chat run/delete/list exclusions, bounded pages/diffs, unknown JSON and raw client tenant/role injection. Assert anonymous deployments register no management routes, even if forged Headers claim admin.
- [ ] Run `go test ./infrastructure/controller/http/... ./application/service/... -count=1`; observe new failures.
- [ ] Controllers strictly decode DTOs and call scoped services; SSE is live display only. Disconnect never automatically creates a replacement Run; GET operation/message/diff restores current state. Connect training events to existing VO/listener machinery without advertising Last-Event-ID replay. Errors distinguish conflict, expiry, unavailable reference, unsafe candidate, stop-blocked and retryable quota busy; redact secrets.
- [ ] Wire recovery before accepting management writes and manager shutdown before releasing data-directory ownership. Archive checks lazy expiry and nonterminal training via shared admission, so no status/pointer race bypasses invariants. Production chat does not wait for a training model Turn.
- [ ] Run gate commands:

```bash
go test ./... -count=1
go test -race ./application/service/agenttraining ./application/service/agentversion ./application/service/agentruntime ./infrastructure/persistence/agenttraining -count=1
git diff --check
```

Execute real MYSQL_TEST_* tests in a disposable schema and native offline/filesystem/death probes on supported OSes; mocks/skips are not substitutes. Exercise create→two training turns→partial repair→checkpoint restore→validate→publish→ordinary follow-latest→activate old version with fake model. Keep another tenant/Agent active to prove isolation. Commit only reviewed implementation; no real production cutover.

## Self-review and handoff

- [ ] Three-table invariant and 0010 generated-slot FK tests: Task 1.
- [ ] Safe draft checkpoint vs complete publish validation, persistent refs and restore metadata: Tasks 2/5/6.
- [ ] Offline tools, Git metadata protection, process death and no model publication: Task 3.
- [ ] Durable input, bounded retries, history ownership and no auto replay: Tasks 4/5/9.
- [ ] Manual confirmation, evidence TTL/policy bindings, source uniqueness, atomic publish and no-op rejection: Tasks 6/7.
- [ ] Exact expiry boundaries, shutdown uncertainty, stale base and Git/DB recovery: Task 8.
- [ ] No workbench, model-only endpoint, event platform or fourth table introduced here. Phase 1D builds UI and the independent model-only release path on these concrete services.

The host authentication protocol remains an external integration requirement; test with injected principals, keep explicitly configured single-tenant anonymous directory/chat mode usable, and do not invent accounts or silently grant management. The user authorized continuation; proceed after phase review without another generic execution-choice prompt.
