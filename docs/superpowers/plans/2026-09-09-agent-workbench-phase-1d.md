# Agent Workbench Phase 1D Implementation Plan

**Approval status:** Awaiting explicit user review and approval. Development is paused; this is review material, not authorization to execute.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the administrator Agent/training workbench, complete model-only releases, and a verified migration/backup/end-to-end acceptance workflow.

**Architecture:** The workbench consumes the Phase 1B catalog and Phase 1C training/validation/publication services without moving state-machine rules into JavaScript. Model-only releases reuse immutable version preparation, validation and CAS activation, with platform-owned durable per-version preparation records for file/database recovery. Reconnection reads persisted snapshots and messages instead of resending operations.

**Tech Stack:** Go 1.26, Gin/Fx/MySQL, Git, embedded templates, vanilla JavaScript/CSS, Node tests and browser verification, existing native sandbox probes.

**Spec:** `docs/superpowers/specs/2026-09-04-agent-self-training-design.md`, §§8, 9.3, 9.4, 10.1 and 11. Prerequisite plans: `2026-09-09-agent-Agents-phase-1b.md` and `2026-09-09-agent-training-phase-1c.md` in this directory. Begin after their reviewed acceptance gates.

## Global Constraints

- “第一版新增三张业务表”：no fourth table, generic event platform, model registry, fine-tuning or automatic evaluation UI.
- “管理入口仅在服务端启用且有权限时显示；后端独立鉴权。”
- “本版本仅完成结构及脚本校验，未经自动质量评测”：explicit human publication confirmation, no invented scores.
- Model-only request contains `expected_row_version`, `base_version_id`, allowed `model_config`, `change_summary` and human confirmation.
- “重复旧 expected_row_version 请求返回 409 和当前版本”；no generic request-ID replay promise for model-only release.
- “每个 versions/<version-id>/ 都全量物化”：model-only releases reuse Git tree but create independent read-only version directory.
- “同批数据库一致性快照 + bundle.git 对象及全部 refs/tags + 已持久附件”；temporary runtime caches need not be backed up.
- Real host authentication, credential maintenance and customer deployment choices are host integrations. No account/login system is invented here.
- Production data mutation/deployment is not part of plan execution; drills use disposable databases/data directories and test principals.

## Verified boundaries and preparation-record decision

Current frontend chat code is a large single `chat.js` with independent reusable image/stream/content modules and Go embedded templates. Keep directory/customer chat working, but create focused workbench modules rather than embedding admin state into chat.js. Existing ordinary chat message visibility hides some file-reading tool output; Phase 1C training message projection must remain separate and preserve review-relevant tools.

Model-only releases have no TrainingSession and thus no operation_json slot. **Chosen implementation:** persist a small per-target-version preparation record at `<data-dir>/tenants/<tenant-id>/agents/<agent-id>/state/publications/<version-id>.json`, outside every author/chat/validation root. Write it atomically and durably before preparing files/tag; it contains tenant/agent/version IDs, proposed version number, expected Agent row_version, base version ID, new bundle/spec digests, target tag and phase, with no credential bodies. This is a bounded per-operation recovery record, removed once reconciled, not a new database table, event stream, or idempotency service. Add this platform-owned `state/publications/` directory to the spec's §5.1 layout and explain its recovery use in §9.4 before implementation review; it must never enter Bundle trees or model-visible directories.

Within the single-process deployment a common Agent **mutation admission registry** reserves one release/preparation operation while expensive file/validation work runs, and author/restore/config/publish admission consults it under the same Phase 1B AgentAdmission lock. The lock itself is not held across the entire operation, so production chat continues from the active pointer. Durable preparation records reconstruct blocked/resumable cleanup state before writes reopen after restart. Phase 1C training operation_json stays authoritative for training; shared admission inspects both sources. No idle training is cancelled merely for a model release; its next operation detects stale base after success.

## File ownership and shared interfaces

Task 1 owns model-only preparation/CAS and durable records; Task 2 owns HTTP/admin page contracts; Tasks 3/4 own admin catalog/training frontend separately; Task 5 owns historical versions/model selection UI; Task 6 owns backup/migration drills and integrated acceptance. Extend prior contracts only where named; do not reimplement digest/validation/state machine in controllers.

### Task 1: Model-only releases with immutable evidence and recovery

**Files:** Create `application/service/agentversion/{model_release,model_release_test,release_admission,release_admission_test}.go`, `application/port/agentbundle/publication_intent.go`, `infrastructure/driver/agentbundle/{publication_intent,publication_intent_test}.go`, `infrastructure/persistence/agent/model_release.go` and tests; extend shared recovery and DTO/VO files.

**Interfaces:** `ReleaseModel(ctx context.Context, principal identity.Principal, agentID string, request dto.ModelConfigReleaseDTO) (vo.AgentVersionVO,error)`. DTO fields exactly match Global Constraints. `PublicationIntent{TenantID,AgentID,VersionID,BaseVersionID,Tag,BundleDigest,SpecDigest,Phase string; Version,ExpectedRowVersion uint64}`; `IntentStore.Prepare(ctx context.Context, intent PublicationIntent) error`, `List(ctx context.Context, tenantID,agentID string) ([]PublicationIntent,error)`, `Remove(ctx context.Context, intent PublicationIntent) error`. `ReleaseAdmission.Reserve(ctx,tenantID,agentID,versionID string) (release func(),err error)` runs under shared AgentAdmission and refuses live training operations; release is idempotent. Repository `CommitModelRelease(ctx,tenantID,agentID string,expectedRowVersion uint64,baseVersionID string,version agent.Version,evidence agentversion.Evidence) error` is one DB transaction.

- [ ] Write tests for administrator/tenant boundary; stale row_version/base rejects; unchanged effective model rejects; tool/runtime change or unknown field rejects; unconfirmed request rejects; same tree/new config gives new spec digest but old bundle digest; concurrent requests allocate only one committed release; repeated old CAS returns 409/current version. Verify existing version JSON/evidence is byte-identical after release.
- [ ] Run `go test ./application/service/agentversion ./infrastructure/persistence/agent ./infrastructure/driver/agentbundle -count=1`; record failures before implementation.
- [ ] Under shared admission authenticate and lock Agent, check enabled/current base/CAS and absence of running training mutation; reserve release marker before releasing lock. Copy current immutable Snapshot, replace only model configuration resolved from server allowlist, reject no effective difference and privilege change. Reuse bundle commit/tree and complete tool/runtime snapshot. Allocate server version ID and monotonic number; persist durable preparation record before Git/file side effects.
- [ ] Acquire shared validation quota and perform complete file, model, tool, SecretRef and smoke checks through Phase 1C Validator. Recompute bundle/spec digests and fresh bounded manual-review evidence; no training expiry applies, but evidence TTL still must be valid at final DB commit. Changed host defaults must not refill saved tool/runtime config. Reuse Git tree but fully materialize independent read-only version directory; no hard links to writable assets.
- [ ] Final transaction rechecks expected Agent row_version, current base, recorded intent/digests and evidence expiry/current operation policy; insert immutable version with source_training_session_id=NULL, publisher/time/summary/evidence; CAS active pointer. Do not alter idle training: subsequent admission marks it stale. Release reservation only after successful commit or confirmed cleanup; ordinary chat is never blocked on file preparation.
- [ ] Recovery scans platform intent records before management admission: DB row/pointer committed means retain version and remove intent; absent DB row means delete only intent-owned unreferenced version directory/tag and remove intent after verified cleanup. Never remove shared commit or another operation's assets. On corrupt/unknown intent retain evidence and block affected Agent mutation with diagnostic. DB commit plus lost response remains success in history; old expected CAS request returns conflict, not a duplicate version. Durable write/fsync/rename and crash-injection tests cover each phase.
- [ ] Run fault matrix at intent creation, tag, materialization, validation, DB commit, response and cleanup; execute MySQL CAS race tests. Commit task files after review.

### Task 2: Admin API/page contracts and secure entry points

**Files:** Modify `infrastructure/controller/http/agent/{controller,controller_test}.go`, `infrastructure/controller/http/{register,register_test}.go`, `infrastructure/controller/http/page/{controller,controller_test,renderer}.go`; add `common/vo/admin.go` and relevant DTO strict-decoding tests.

**Interfaces:** Register POST `/api/v1/agents/:agentID/model-config-releases`; use existing catalog/templates/versions/activate and Phase 1C training APIs. Admin pages are `/admin/agents`, `/admin/agents/new`, `/admin/agents/:agentID`, `/admin/training-sessions/:trainingID`. Safe page bootstrap contains tenant display label, authenticated role, feature availability and target IDs only. Allowed model-selection options come from a new admin-only GET `/api/v1/agent-model-options` returning server-approved IDs/capabilities/parameters and availability reasons, never credentials or arbitrary binary paths. This supporting read endpoint must be recorded in spec §9.1 before implementation review.

- [ ] Test no admin pages/write routes in anonymous mode; ordinary host role denied; forged headers ignored; cross-tenant deep links fail; other admin's training denied; unknown keys and stale CAS mapped correctly; model options redact credential bodies and allowlist-only values. Verify direct service calls still enforce admin.
- [ ] Run `go test ./infrastructure/controller/http/... -count=1`; observe new failures.
- [ ] Wire new endpoint to ReleaseModel with strict bounded JSON decoder, expected version metadata in conflict response, explicit human confirmation and summary. Cap free text and response evidence/diff sizes consistent with Phase 1B/C contracts. Do not reflect raw filesystem paths, stack traces or secrets.
- [ ] Render guarded admin pages and customer directory's management link only when server capability and role both permit. Deep links load scoped detail, never a guessed fallback Agent. All state changes remain POST/PATCH; page GET never creates training or publication. Authentication redirect is supplied by host integration, not a fake login form.
- [ ] Provide model options from the existing snapshot resolver's safe allowlist projection, same source used to validate submissions. Add spec route note and run tests. Commit.

### Task 3: Administrator Agent catalog and detail workspace

**Files:** Create `frontend/templates/pages/admin-agents.html`, `admin-agent-create.html`, `admin-agent-detail.html`; create `frontend/static/js/pages/{admin-agents,admin-agent-detail,admin-api}.js`, `frontend/static/css/pages/admin-agents.css`, `frontend/static/js/pages/admin_agents_test.mjs`; modify renderer tests.

**Interfaces:** `admin-api.js` exports `requestJSON(url,options)`, parsing existing API envelope and typed conflict/error metadata. Catalog module consumes AgentPageVO/TemplateVO; detail retains `row_version` from last successful GET for subsequent PATCH/create-training. No duplicate client-side business status authority.

- [ ] Add Node tests with mocked fetch: template presentation copied into draft without changing template; repeated submit disabled; same template can create multiple Agents; empty/error states distinct; search/page cursor maintained; stale CAS reload does not overwrite entered changes; archive conflict shows active training link only if authorized.
- [ ] Run `node --test frontend/static/js/pages/admin_agents_test.mjs`; observe failures.
- [ ] Build admin list with enabled/archived/incomplete state, name/duties search and pagination. Create form uses templates and allowed model options, optional independent presentation overrides, no tenant or secret entry. On success navigate to detail; response failure offers reload/history check, not blind automatic retry of non-idempotent creation.
- [ ] Detail shows Agent presentation, current version and model, status, authorized training history and create-training action with expected Agent.row_version. Presentation edit changes only allowed fields; archive uses CAS and displays backend live-training conflict. Incomplete creation offers only supported recovery/archival actions; do not invent an unimplemented retry endpoint.
- [ ] Render all names/markdown paths/diffs via safe text nodes or approved sanitized content helpers. Keep tenant/environment label visible from server metadata and never client-selected. Verify mobile/desktop empty, loading, long-name and pagination layouts in browser.
- [ ] Run Node plus page renderer tests; commit.

### Task 4: Training workbench and snapshot-based reconnect

**Files:** Create `frontend/templates/pages/admin-training.html`, `frontend/static/js/pages/{admin-training,training-state,training-diff}.js`, `frontend/static/css/pages/admin-training.css`, `frontend/static/js/pages/{training_state_test,training_reconnect_test}.mjs`; reuse existing chat-image/chat-stream/chat-message-content modules without copying their internals.

**Interfaces:** `training-state.js` exports `applySnapshot(state,sessionVO)` and `availableActions(sessionVO)`; state includes authoritative row_version/head/partial/status/current operation, connection status and local draft. UI actions generate one request_id/run_id per **explicit** action and preserve it only for querying/retrying that same operation; changing input generates a new ID. Reconnection always GETs session/messages/diff and polls active operation.

- [ ] Node tests cover active/validating/ready/terminal action visibility, operation busy disablement, partial candidate validation refusal, stale CAS refresh, exact same-ID retry vs new-input ID, other-admin denial, bounded diff truncation and expired/stop-blocked messages. Reconnect test asserts fetch log contains no POST `/runs` after stream disconnect.
- [ ] Run `node --test frontend/static/js/pages/training_*_test.mjs`; observe failures.
- [ ] Compose training conversation, current files diff, checkpoint list/restore, model config selector, validation evidence and actions: 校验、继续训练、恢复、发布、取消. Display base/current version, partial marker and expiry from server; no quality score/fine-tune control. Use existing image input if service supports it; never infer an upload persistence platform beyond existing URL representation.
- [ ] Run send handler submits explicit run_id plus expected row_version once; stream renders current connection only. Disconnect fetches authoritative operation and persisted messages, displays interrupted result where reported, and does not auto-retry tools. Poll while operation is active, stop on terminal/navigation, and keep request cancellation distinct from training cancellation.
- [ ] ready editing/restore/config actions explain evidence invalidation and let backend perform transition. Restore selection stays within this session's checkpoint list and keeps later history visible. Show diagnostic paths/categories with bounded diff and safe escaping; clearly separate ingestion-safe partial draft from ready publication evidence.
- [ ] Publish dialog requires an unchecked explicit human confirmation using exact spec wording plus summary. Submit expected row_version/request_id; duplicate result navigates to already published version. Cancel shows stopping until backend confirms terminal; timeout cannot locally free the session slot or enable another writer.
- [ ] Browser-test full reconnect and two-admin/two-tenant scenarios using test host adapter. Verify keyboard focus, narrow screens, long diffs, readonly terminal history and loading/error accessibility. Run Node/renderer tests and commit.

### Task 5: Version history, activation and model-only release UI

**Files:** Create `frontend/static/js/pages/{admin-agent-versions,model-release}.js`, `frontend/static/js/pages/model_release_test.mjs`; extend admin-agent-detail template/styles and controller rendering tests.

**Interfaces:** Version page consumes descending AgentVersionPageVO with signed cursor and safe current version metadata. Model release form captures expected_row_version/base_version_id at open, submits allowed model_config/change_summary/human confirmation to Task 1 endpoint. It never PATCHes old versions or treats host currentPlatform change as a release.

- [ ] Test new version inserted between pages never repeats displayed older items; foreign cursor rejects; changed current version while dialog open returns conflict and requires explicit review; exact model no-op cannot submit as meaningful release; lost response queries history/current pointer instead of issuing blind second release. Confirm original tool/runtime snapshot displayed as preserved metadata.
- [ ] Run `node --test frontend/static/js/pages/model_release_test.mjs`; observe failures.
- [ ] Show version number/time/publisher/change summary/requested model and bounded validation evidence, with “未自动质量评测” label. Activate invokes existing historical API with current CAS and backend revalidation; unavailable old reference shows concrete redacted reason, never falls back to template/current defaults.
- [ ] Model form fetches admin model options, initializes from current version, accepts only supported parameters and explicit manual confirmation. Busy/quota/CAS errors preserve draft without claiming publish. Success updates detail/version history; customer follow_latest changes on next turn according to existing backend, no invented event fanout.
- [ ] Browser verify idle training remains after model release then becomes stale on next operation; running training blocks conflicting release; archived Agent refuses release; successful new model version does not alter historical evidence/files. Commit after tests.

### Task 6: Migration/backup drills and complete product acceptance

**Files:** Create `docs/agent-Agents-operations.md`, `docs/agent-Agents-phase-1d-evidence.md`, `cmd/agent-verify/main.go` and focused tests only if existing migration command cannot host read-only verification; extend `docs/agent-Agents-migration.md`, `docs/agent-training-recovery.md` and config Go comments. No backup scheduler or generic storage platform.

**Interfaces:** Read-only verification command walks DB versions/session refs by tenant through system operator scope, executes `git fsck`, verifies all version/head/checkpoint references and digest protocols, and prints bounded per-Agent findings. It must not activate versions, repair messages or run candidate scripts as an incidental verify action. Reuse Phase 1B/C parsers/checkers.

- [ ] Write integration scenarios using disposable MySQL+data directory: restore consistent DB/Git backup, missing Git object, corrupt spec digest, absent SecretRef, partial model-only intent, interrupted training publication, 0008 down duplicate-owner conflict, 0009 retirement code/schema compatibility. Verify no unsafe automatic fix and unaffected Agent remains usable.
- [ ] Run focused tests first, implement read-only command by reusing existing helpers, and rerun. Do not duplicate digest code in an operations script.
- [ ] Document deployment-owned backup frequency/retention and coordinated stop-write procedure; no invented production values. Inventory same-batch MySQL snapshot, all bundle.git refs/tags, any actually persisted attachments, and required host credential-reference inventory (secret backup remains with host). Runtime caches need not be copied. Publication preparation records are either reconciled before consistent backup or included in platform-state inventory so restore can explicitly reconcile them.
- [ ] Perform restore drill on an isolated fresh directory/database: restore batch, prove old processes absent, `git fsck`, verify version/checkpoint/content/config digests, resolve allowed model/tool/secret references, rebuild materialized directories, then run fake-provider smoke conversations. Missing objects block corresponding activation, never load shared templates. Record database/server version and real executed commands with credentials redacted.
- [ ] Complete staged migration drill from 0006 through 0010 following Phase 1B gates, including supplied test ownership map, bootstrap idempotence and rollback conflict. Do not automatically apply 0009 to an environment with old clients. Document external facts still needed for actual customer cutover.
- [ ] Run full product scenario: directory→choose Agent→first message; create a second Agent from same template; train one Agent across multiple turns; safe partial checkpoint and repair; inspect diff/history; explicit validate/publish; ordinary next-turn version refresh; rollback; model-only release; expiry/cancel/reconnect; two tenants isolated. Confirm images/SSE/history rename/search/delete still function and no messages/files move on Agent switch.
- [ ] Run final checks after all implementations settle:

```bash
go test ./... -count=1
go test -race ./application/service/agenttraining ./application/service/agentversion ./application/service/agentruntime ./application/service/chat -count=1
node --test frontend/static/js/pages/*_test.mjs
git diff --check
```

Execute real disposable MYSQL_TEST_* and native sandbox tests, plus browser scenarios at desktop and narrow widths. Record skipped environments and missing trusted-host integration separately; unit tests alone do not prove customer login or backup readiness. Commit reviewed artifacts and code; no production deployment is implied.

## Self-review and completion boundary

- [ ] §9.3 Agent management, workbench, reconnect and history authorization: Tasks 2–4.
- [ ] §9.4 immutable model-only release, shared validation, CAS conflict and file/DB recovery: Task 1; presentation and loss-of-response behavior: Task 5.
- [ ] §8.4 coordinated backup/restore and no fallback to templates: Task 6.
- [ ] §10.1 migration staging and rollback conflicts: Task 6 builds on Phase 1B/C tooling rather than new tables.
- [ ] §11 acceptance includes unrelated tenant/Agent isolation, no automatic-quality claims, no event-replay promise, no secret bodies in version evidence, and accurate integration limitations.

The spec now includes the platform publication-intent directory and admin-safe model-options read endpoint. The complete proposal must receive explicit user approval before any implementation resumes. External identity/deployment facts still constrain real rollout after approval.
