# Agent Profile Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Web 聊天增加会话级 Agent Profile，使用户在首条消息前选择助手角色，并让该会话后续稳定使用对应身份规则与专属 Skills。

**Architecture:** Profile 是业务层只读 Catalog，由 `infrastructure/driver/agentprofile` 从 `workspaces/chat/profiles` 加载并校验，通过领域接口注入 `application/service/chat.Service`。会话持久化稳定的 `profile_code`，Run 时服务端依据会话读取 Profile 并构造 `pi.ContextBlock`，前端只在创建会话时提交选择，不修改 `pi` 公共契约。

**Tech Stack:** Go 1.26、Fx、Gin、GORM/MySQL、`gopkg.in/yaml.v3`、Go HTML templates、Vanilla JavaScript、CSS。

**Spec:** `docs/superpowers/specs/2026-08-17-agent-profile-chat-design.md`

## Global Constraints

- 不修改 `pi/` 目录及其公共 API。
- Profile 创建后不可修改；Run API 不接受 `profile_code`。
- 根 Workspace 的 AGENTS、通用 Skills 和现有工具继续对所有 Profile 可用。
- Profile 配置随仓库发布，无后台、在线训练或热更新。
- 既有会话通过迁移归入 `general`；运行遇到 Catalog 中不存在的 code 必须失败，不能静默降级。
- Profile API 返回全部已知 Profile；只有 `selectable=true` 的 Profile 可创建新会话。
- Profile Catalog 加载失败时服务启动失败；前端加载 Catalog 失败只禁止新建，不阻止已打开会话继续 Run。
- 前端保持 Go 模板 + Vanilla JS，静态资源由现有 Go 服务提供，不增加 Node 服务。
- 保留当前 SSE `message.delta` 流式渲染与 Cookie 用户隔离行为。
- 所有行为变更按 RED-GREEN-REFACTOR 执行，先跑聚焦测试，再跑全量和 race 验证。

---

### Task 1: Profile Domain、Catalog Driver 与 Workspace 数据

**Files:**
- Create: `domain/entity/agentprofile/profile.go`
- Create: `domain/repository/agentprofile/catalog.go`
- Create: `infrastructure/driver/agentprofile/catalog.go`
- Create: `infrastructure/driver/agentprofile/catalog_test.go`
- Create: `workspaces/chat/profiles/catalog.yaml`
- Create: `workspaces/chat/profiles/{general,writing,learning,health,legal,automotive,workplace,parenting}/AGENTS.md`
- Create: `workspaces/chat/profiles/{writing,learning,health,legal,automotive,workplace,parenting}/skills/*/SKILL.md`
- Modify: `application/web/register.go`
- Modify: `application/web/workspace_test.go`

**Interfaces:**
- Produces: `agentprofile.Profile`, `agentprofile.Starter`, `agentprofile.Catalog` with `List() []Profile`, `Find(string) (Profile, bool)`, and `DefaultCode() string`.
- Produces: `agentprofile.NewCatalog(pi.WorkDir) (agentprofilerepo.Catalog, error)` as an Fx provider.
- Profile contains `Code`, `Name`, `Description`, `Icon`, `Order`, `Selectable`, `Welcome`, `Starters`, `Instructions`, and `Skills`.

- [ ] **Step 1: Write failing Catalog behavior tests**

Add table-driven tests proving a valid fixture loads in `order/code` order, derives paths only from code, returns defensive copies, keeps non-selectable entries findable, and rejects unsupported version, duplicate/invalid code, invalid icon, missing default, missing/empty/non-UTF-8 AGENTS, symlink/path escape, and malformed Skills.

```go
func TestNewCatalogLoadsValidatedImmutableProfiles(t *testing.T) {
    catalog, err := NewCatalog(pi.WorkDir(validProfileWorkspace(t)))
    if err != nil { t.Fatal(err) }
    got := catalog.List()
    if got[0].Code != "general" || got[1].Code != "writing" { t.Fatalf("profiles = %#v", got) }
    profile, ok := catalog.Find("writing")
    if !ok || !strings.Contains(profile.Instructions, "写作") || profile.Skills[0].Location != "profiles/writing/skills/content-writing/SKILL.md" {
        t.Fatalf("writing profile = %#v, found=%v", profile, ok)
    }
    got[0].Name = "mutated"
    again, _ := catalog.Find("general")
    if again.Name == "mutated" { t.Fatal("catalog exposed mutable snapshot") }
}
```

- [ ] **Step 2: Run Catalog tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/driver/agentprofile ./application/web`

Expected: FAIL because the Profile domain and Catalog driver do not exist and Workspace Profile files are absent.

- [ ] **Step 3: Implement immutable validated Catalog and eight Profile bundles**

Implement the exact public contract:

```go
type Starter struct { Title string; Prompt string }
type Skill struct { Name string; Description string; Location string }
type Profile struct {
    Code, Name, Description, Icon, Welcome, Instructions string
    Order int
    Selectable bool
    Starters []Starter
    Skills []Skill
}
type Catalog interface {
    List() []agentprofile.Profile
    Find(code string) (agentprofile.Profile, bool)
    DefaultCode() string
}
```

Parse `catalog.yaml` with `yaml.Decoder.KnownFields(true)`, validate the spec's lengths/code/icon/default rules, `EvalSymlinks` every Profile directory/file and require containment under the resolved Workspace, read non-empty UTF-8 AGENTS, call existing `skills.Discover(profileDir)`, reject diagnostics, and convert Skill locations to Workspace-relative slash paths. Register the Catalog in `application/web.Register` after `NewChatWorkDir`. Add all eight approved Catalog entries and concise role AGENTS/Skill bodies; `general` has no dedicated Skill directory.

- [ ] **Step 4: Run Catalog and Workspace tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/driver/agentprofile ./application/web`

Expected: PASS with all eight Profiles loaded and only the four root Skills discovered by the common Harness.

- [ ] **Step 5: Commit Catalog task**

```bash
git add domain/entity/agentprofile domain/repository/agentprofile infrastructure/driver/agentprofile application/web/register.go application/web/workspace_test.go workspaces/chat/profiles
git commit -m "feat: add chat agent profile catalog"
```

### Task 2: Conversation Profile Persistence and Filtering

**Files:**
- Create: `migrations/0004_agent_profiles.up.sql`
- Create: `migrations/0004_agent_profiles.down.sql`
- Modify: `domain/entity/conversation/conversation.go`
- Modify: `domain/entity/conversation/entity_test.go`
- Modify: `domain/repository/conversation/management.go`
- Modify: `domain/repository/conversation/conversation_test.go`
- Modify: `infrastructure/persistence/conversation/repository.go`
- Modify: `infrastructure/persistence/conversation/repository_test.go`
- Modify: `infrastructure/persistence/conversation/management.go`
- Modify: `infrastructure/persistence/conversation/management_test.go`
- Modify: `infrastructure/persistence/conversation/migration_test.go`
- Modify: `infrastructure/persistence/conversation/repository_integration_test.go`

**Interfaces:**
- Produces: `Conversation.ProfileCode string` with GORM `column:profile_code;size:64;not null;default:general`.
- Extends: `conversationrepo.ListQuery` with `ProfileCode string`.
- Persistence normalizes an empty ProfileCode to `general` on `Create`; it never updates ProfileCode afterward.

- [ ] **Step 1: Write failing migration, entity, create, load, and list-filter tests**

```go
func TestAgentProfileMigrationDefinesConversationProfile(t *testing.T) {
    up, _ := os.ReadFile("../../../migrations/0004_agent_profiles.up.sql")
    for _, want := range []string{"ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general'", "idx_agent_conversations_user_profile_updated", "user_id, profile_code, updated_at, id"} {
        if !strings.Contains(string(up), want) { t.Fatalf("up migration missing %q", want) }
    }
}
```

Update SQL mock fixtures so `Find` returns `profile_code`, `Create` persists explicit values and defaults empty to `general`, and `ListByUserID(ProfileCode:"writing")` emits `WHERE conversations.profile_code = ?` while preserving cursor order and count.

- [ ] **Step 2: Run persistence tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./domain/entity/conversation ./domain/repository/conversation ./infrastructure/persistence/conversation`

Expected: FAIL because `ProfileCode`, migration 0004, and filter SQL are missing.

- [ ] **Step 3: Implement migration and persistence mapping**

Use the exact migration:

```sql
ALTER TABLE agent_conversations
    ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general' AFTER name,
    ADD INDEX idx_agent_conversations_user_profile_updated
        (user_id, profile_code, updated_at, id);
```

Down migration drops the index first, then the column. Add ProfileCode to entity/list row/select/group/mapping, append an exact-match filter only when the normalized query value is non-empty, and default empty ProfileCode before `Create`.

- [ ] **Step 4: Run persistence tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./domain/entity/conversation ./domain/repository/conversation ./infrastructure/persistence/conversation`

Expected: PASS, including integration tests when `MYSQL_TEST_DSN` is configured; otherwise the existing integration skip remains explicit.

- [ ] **Step 5: Commit persistence task**

```bash
git add migrations/0004_agent_profiles.* domain/entity/conversation domain/repository/conversation infrastructure/persistence/conversation
git commit -m "feat: persist conversation agent profiles"
```

### Task 3: Profile API and Conversation Application Contracts

**Files:**
- Modify: `common/dto/chat.go`
- Modify: `common/vo/chat.go`
- Modify: `application/service/chat/service.go`
- Modify: `application/service/chat/service_test.go`
- Modify: `application/service/chat/register.go`
- Modify: `infrastructure/controller/http/chat/controller.go`
- Modify: `infrastructure/controller/http/chat/controller_test.go`
- Modify: `infrastructure/controller/http/register.go`
- Modify: `infrastructure/controller/http/register_test.go`

**Interfaces:**
- Produces: `CreateConversationDTO{ProfileCode string}` and adds `ProfileCode string` to `ListConversationsQuery`.
- Produces: `AgentProfileVO`, `AgentProfileStarterVO`, `AgentProfileCatalogVO` and `ConversationVO.ProfileCode`.
- Changes: `Service.CreateConversation(ctx, userID, dto.CreateConversationDTO)` validates a selectable Profile.
- Produces: `Service.ListAgentProfiles() *vo.AgentProfileCatalogVO`.

- [ ] **Step 1: Write failing service and controller API tests**

```go
func TestServiceCreatesConversationWithSelectableProfile(t *testing.T) {
    service := NewService(repo, ids, runner, catalogWith("writing", true))
    got, err := service.CreateConversation(context.Background(), "visitor-1", dto.CreateConversationDTO{ProfileCode:"writing"})
    if err != nil { t.Fatal(err) }
    if repo.created.ProfileCode != "writing" || got.ProfileCode != "writing" { t.Fatalf("created=%#v got=%#v", repo.created, got) }
}
```

Add cases that reject missing/unknown/non-selectable Profile on create, reject unknown list filters, allow filtering by non-selectable known Profile for old conversations, return all Profiles from `GET /api/v1/agent-profiles` without internal Instructions/Skills, bind JSON create bodies, and include ProfileCode in list/create responses.

- [ ] **Step 2: Run application and HTTP tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./application/service/chat ./infrastructure/controller/http/chat ./infrastructure/controller/http`

Expected: FAIL because Catalog is not injected and Profile endpoints/DTOs do not exist.

- [ ] **Step 3: Implement strict create/list contracts and Profile endpoint**

Add `catalog agentprofilerepo.Catalog` to `Service`, change `NewService(repository, ids, runner, catalog)`, validate normalized codes via `catalog.Find`, require `Selectable` only for creation, and pass ProfileCode to persistence list query. Map only public Catalog fields:

```go
type AgentProfileVO struct {
    Code, Name, Description, Icon, Welcome string
    Selectable bool
    Starters []AgentProfileStarterVO
}
type AgentProfileCatalogVO struct {
    Items []*AgentProfileVO `json:"items"`
    DefaultProfile string `json:"default_profile"`
}
```

Controller binds `CreateConversationDTO` from JSON and registers `GET /api/v1/agent-profiles` outside the conversations group. Run DTO remains unchanged.

- [ ] **Step 4: Run application and HTTP tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./application/service/chat ./infrastructure/controller/http/chat ./infrastructure/controller/http`

Expected: PASS with 400 for invalid Profile and no Profile internals in JSON.

- [ ] **Step 5: Commit API task**

```bash
git add common/dto/chat.go common/vo/chat.go application/service/chat infrastructure/controller/http
git commit -m "feat: expose conversation agent profiles"
```

### Task 4: Profile Context and Skill Injection at Run Time

**Files:**
- Modify: `application/service/chat/run_manager.go`
- Modify: `application/service/chat/run_manager_test.go`

**Interfaces:**
- Consumes: `Conversation.ProfileCode` and `agentprofile.Catalog.Find`.
- Produces: two `pi.ContextBlock` entries named `agent-profile` and `agent-profile-skills`, priorities `900` and `800`.

- [ ] **Step 1: Write failing run-context tests**

```go
func TestRunUsesPersistedProfileContext(t *testing.T) {
    repo.foundValue = &conversationentity.Conversation{ConversationID:"chat-1", ProfileCode:"writing"}
    // Start and finish the run, then inspect the captured conversation.RunRequest.
    if got := request.Context; len(got) != 2 || got[0].Name != "agent-profile" || got[0].Priority != 900 || !strings.Contains(got[0].Content, "写作") || !strings.Contains(got[1].Content, "profiles/writing/skills/content-writing/SKILL.md") {
        t.Fatalf("context = %#v", got)
    }
}
```

Add cases proving a non-selectable Profile still runs, a missing Catalog code returns internal error before creating an active run, `general` gets an empty-but-explicit Skill catalog block, and no client Profile field exists in `StartRunDTO`.

- [ ] **Step 2: Run run-manager tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./application/service/chat -run 'TestRun.*Profile|TestRunLifecycle'`

Expected: FAIL because StartRun discards the loaded conversation and does not inject Context.

- [ ] **Step 3: Implement server-owned Profile context**

Retain the owned conversation returned by `FindByUserIDAndConversationID`, resolve its Profile without checking `Selectable`, and build deterministic content:

```go
[]pi.ContextBlock{
    {Name:"agent-profile", Priority:900, Content: profile.Instructions},
    {Name:"agent-profile-skills", Priority:800, Content: renderProfileSkills(profile.Skills)},
}
```

The Skill block uses the existing `<available_skills><skill>...` catalog shape with XML escaping and Workspace-relative locations. Pass these blocks in `conversation.RunRequest.Context`; do not alter the history, input, SSE sequence, or rename behavior.

- [ ] **Step 4: Run chat and conversation tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./application/service/chat ./conversation`

Expected: PASS and existing Context forwarding/SSE lifecycle tests remain green.

- [ ] **Step 5: Commit run-context task**

```bash
git add application/service/chat/run_manager.go application/service/chat/run_manager_test.go
git commit -m "feat: apply agent profile context to chat runs"
```

### Task 5: Agent Profile Chat UI

**Files:**
- Modify: `frontend/templates/pages/chat.html`
- Modify: `frontend/templates/components/conversation-sidebar.html`
- Modify: `frontend/static/js/pages/chat.js`
- Modify: `frontend/static/css/pages/chat.css`
- Modify: `infrastructure/controller/http/page/renderer_test.go`

**Interfaces:**
- Consumes: `GET /api/v1/agent-profiles`, conversation `profile_code`, and strict `POST /api/v1/conversations {profile_code}`.
- Preserves: existing conversation search, cursor pagination, message history, SSE streaming, cancellation, rename/delete, and mobile sidebar.

- [ ] **Step 1: Write failing rendered-page contract test**

```go
func TestChatPageRendersAgentProfileControls(t *testing.T) {
    body, err := renderer.Render("chat.html", map[string]any{"Title":"Reagent"})
    if err != nil { t.Fatal(err) }
    for _, id := range []string{"profilePicker", "profileStarters", "sessionProfile", "profileFilter"} {
        if !strings.Contains(body, `id="`+id+`"`) { t.Fatalf("page missing %s", id) }
    }
}
```

- [ ] **Step 2: Run page tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/controller/http/page`

Expected: FAIL because Profile controls are not rendered.

- [ ] **Step 3: Implement Profile state, accessible controls, and responsive layout**

Load Profile Catalog and conversations concurrently during initialization. Add state for `profiles`, `defaultProfileCode`, `selectedProfileCode`, `profileFilter`, and `profileLoadFailed`. New chat clears current conversation without writing to MySQL, selects the default, renders selectable cards in a stable desktop 4x2/mobile 2-column grid, updates welcome/starters, and only creates on first submit using JSON `{profile_code:selectedProfileCode}`. Existing chats show a non-interactive header badge and list metadata; the sidebar filter includes “全部助手” plus all known Profiles and reloads with `profile_code`. Clicking starter text sets/focuses the textarea without sending. If Catalog load fails, disable only new-chat send and show an actionable toast/inline state; existing selected chats can still run.

Use a JavaScript allow-list mapping the eight Catalog icon keys to familiar text-safe symbols or existing inline icon templates; never inject server HTML/SVG. Keep all dynamic text assigned with `textContent`.

- [ ] **Step 4: Run page tests and static syntax checks and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/controller/http/page ./infrastructure/controller/http`

Run: `node --check frontend/static/js/pages/chat.js`

Expected: both commands PASS.

- [ ] **Step 5: Commit UI task**

```bash
git add frontend/templates frontend/static/js/pages/chat.js frontend/static/css/pages/chat.css infrastructure/controller/http/page/renderer_test.go
git commit -m "feat: add agent profile chat experience"
```

### Task 6: Documentation and End-to-End Verification

**Files:**
- Modify: `docs/web-chat.md`
- Modify: `README.md`
- Modify: `config.example.json` only if the documented Workspace path is not already `workspaces/chat`

**Interfaces:**
- Documents: Profile Catalog extension procedure, immutable conversation binding, API examples, migration order, and disable-vs-delete lifecycle.

- [ ] **Step 1: Update operator and extension documentation**

Document exact startup requirement `agent.workspace_dir=./workspaces/chat`, migration `0004_agent_profiles.up.sql`, `GET /api/v1/agent-profiles`, required `profile_code` create body, optional list filter, and the safe extension sequence: add Catalog entry, AGENTS, optional Skills, tests, then deploy. State that a referenced Profile must first be set `selectable:false` and can be deleted only after a data migration removes all references.

- [ ] **Step 2: Run focused and full automated verification**

Run: `gofmt -w domain/entity/agentprofile domain/repository/agentprofile infrastructure/driver/agentprofile domain/entity/conversation domain/repository/conversation infrastructure/persistence/conversation common/dto/chat.go common/vo/chat.go application/service/chat infrastructure/controller/http application/web`

Run: `git diff --check`

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./domain/... ./application/service/chat ./infrastructure/driver/agentprofile ./infrastructure/persistence/conversation ./infrastructure/controller/http/... ./application/web ./conversation`

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./...`

Run: `GOCACHE=/tmp/go-reagent-go-cache go test -race ./...`

Run: `GOCACHE=/tmp/go-reagent-go-cache go build -o /tmp/go-reagent-server ./cmd/server`

Expected: all commands PASS; no file under `pi/` appears in `git diff --name-only`.

- [ ] **Step 3: Apply and verify MySQL migration**

Against the configured local `harness` database, apply migration 0004 through the repository's documented migration workflow, then verify `SHOW COLUMNS FROM agent_conversations LIKE 'profile_code'` reports `varchar(64)`, `NO`, default `general`, and `SHOW INDEX` contains `idx_agent_conversations_user_profile_updated`. Create/list one `writing` conversation and verify its `profile_code` is persisted.

- [ ] **Step 4: Run browser verification on desktop and mobile**

Start `CONFIG_PATH=./config.json GOCACHE=/tmp/go-reagent-go-cache go run ./cmd/server`, open the logged URL, and verify: 8 selectable Profiles render; default is `general`; starter fills only; first send creates the selected Profile; badge/list metadata/filter are correct; refresh retains binding; streaming deltas render without duplicate assistant messages; switching to another Profile requires new chat; invalid/failed Catalog handling does not break an existing conversation. Capture desktop `1440x900` and mobile `390x844` screenshots and confirm no overlap, clipping, blank content, or layout shift.

- [ ] **Step 5: Commit documentation and final verification record**

```bash
git add README.md docs/web-chat.md config.example.json
git commit -m "docs: describe agent profile chat"
```

