# Exa-Only Agent External Information Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route every Agent-initiated public information lookup through Exa MCP, remove Open-Meteo and `get_weather`, and keep current-time calculation local and network-free.

**Architecture:** Keep `pi/mcp` as the only HTTP boundary for Agent public-information tools and expose Exa's `web_search_exa` and `web_fetch_exa` directly. Remove the domain weather wrapper and Open-Meteo driver; rewrite the weather Skill to search through Exa. Refactor `get_current_time` to accept an IANA timezone and calculate from the injected clock without a location service.

**Tech Stack:** Go 1.26, Fx, Pi ToolRuntime and ExtensionRuntime, MCP Streamable HTTP protocol `2025-03-26`, Workspace Markdown Skills, Go `testing`/`httptest`.

**Spec:** `docs/superpowers/specs/2026-08-19-exa-only-agent-external-information-design.md`

## Global Constraints

- “All network queries through Exa MCP” means all public information retrieval initiated by the Agent; model APIs, Exa MCP transport, MySQL, Redis, browser traffic, and business integrations remain outside this boundary.
- `pi/mcp` remains generic and must not import weather or other domain concepts.
- The Web application requires an enabled Exa server exposing `web_search_exa` and `web_fetch_exa` under their remote names.
- `EXA_API_KEY` is read only from the environment and must never be written to configuration, logs, tests, or commits.
- There is no Open-Meteo, alternative search provider, or model-memory fallback for current public information.
- `get_current_time` performs no network I/O and accepts an IANA timezone such as `Asia/Shanghai`.
- Ordinary tests never access public networks; the existing Exa smoke test remains opt-in.
- Preserve every pre-existing user change. In particular, `workspaces/chat/AGENTS.md`, `workspaces/chat/skills/weather-assistance/SKILL.md`, and `application/web/workspace_test.go` are already modified and must be patched incrementally rather than replaced.
- Do not stage or commit the user's pre-existing `skills/repository-development/SKILL.md` deletion or any unrelated worktree change.

## File Map

**Modify:**

- `application/tool/chat/current_time.go` - pure local time calculation from an IANA timezone.
- `application/tool/chat/current_time_test.go` - deterministic timezone, schema, invalid-input, and cancellation coverage.
- `application/tool/chat/register.go` - remove `get_weather` registration.
- `application/tool/chat/register_test.go` - expect only local calculate/time tools and remove the local weather loop fixture.
- `application/web/register.go` - stop registering Open-Meteo and require valid Exa configuration.
- `application/web/register_test.go` - update local tool expectations and validate required Exa policy.
- `application/web/mcp_test.go` - verify a registered Exa search tool reaches `tools/call`.
- `application/package_boundary_test.go` - enforce no direct HTTP import in chat tools and no Open-Meteo Web dependency.
- `workspaces/chat/AGENTS.md` - state that public current information must use Exa tools.
- `workspaces/chat/skills/weather-assistance/SKILL.md` - replace `get_weather` workflow with Exa search/fetch workflow.
- `application/web/workspace_test.go` - assert the Exa-only weather Skill contract while preserving the current Agent Profile refactor.

**Delete:**

- `application/tool/chat/weather.go`
- `application/tool/chat/weather_test.go`
- `application/tool/chat/location.go`
- `domain/service/weather.go`
- `infrastructure/driver/openmeteo/client.go`
- `infrastructure/driver/openmeteo/client_test.go`
- `infrastructure/driver/openmeteo/register.go`
- `infrastructure/driver/openmeteo/register_test.go`

**Unchanged:**

- `pi/mcp/*` - the existing generic MCP protocol, transport, extension, and tool adapter already provide the required runtime path.
- `config.example.json` - it already contains the required Exa server, Header environment mapping, allowlist, and empty prefix.

---

### Task 1: Make `get_current_time` Network-Free

**Files:**
- Modify: `application/tool/chat/current_time.go`
- Modify: `application/tool/chat/current_time_test.go`

**Interfaces:**
- Consumes: `Clock func() time.Time`, `ai.Tool`, `invalidArguments`, and `jsonOutput` from `application/tool/chat`.
- Produces: `newCurrentTimeTool(clock Clock) *currentTimeTool`; tool input `currentTimeArgs{Timezone string}`; result `currentTimeResult{Timezone, LocalTime, Date, Weekday string}`.

- [ ] **Step 1: Replace location-based tests with failing IANA-timezone tests**

Use this test shape in `current_time_test.go` and remove tests that inject `service.LocationResolver`:

```go
func TestCurrentTimeToolUsesIANAZone(t *testing.T) {
	tool := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Asia/Tokyo"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[currentTimeResult](t, output)
	if got.Timezone != "Asia/Tokyo" || got.LocalTime != "2026-08-16T10:30:00+09:00" ||
		got.Date != "2026-08-16" || got.Weekday != "Sunday" {
		t.Fatalf("result = %#v", got)
	}
}

func TestCurrentTimeToolDefinitionAcceptsOnlyTimezone(t *testing.T) {
	definition := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z")).Definition()
	properties := definition.InputSchema.(map[string]any)["properties"].(map[string]any)
	if definition.Name != "get_current_time" || !definition.ParallelSafe || len(properties) != 1 {
		t.Fatalf("definition = %#v", definition)
	}
	if _, ok := properties["timezone"]; !ok {
		t.Fatalf("properties = %#v", properties)
	}
}

func TestCurrentTimeToolRejectsInvalidTimezoneAndExtraArguments(t *testing.T) {
	tool := newCurrentTimeTool(fixedClock("2026-08-16T01:30:00Z"))
	for _, arguments := range []string{`{"timezone":"Mars/Olympus"}`, `{"timezone":"Asia/Tokyo","location":"Tokyo"}`} {
		_, err := tool.Execute(context.Background(), json.RawMessage(arguments), nil)
		if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
			t.Fatalf("arguments = %s, error = %v", arguments, err)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and confirm the old API fails**

```bash
go test ./application/tool/chat -run '^TestCurrentTimeTool' -count=1
```

Expected: compile failures because `newCurrentTimeTool` still requires a resolver and `currentTimeResult` still contains `Location`.

- [ ] **Step 3: Implement the pure local current-time tool**

Refactor `current_time.go` to this contract:

```go
type currentTimeTool struct{ clock Clock }

type currentTimeArgs struct {
	Timezone string `json:"timezone"`
}

type currentTimeResult struct {
	Timezone  string `json:"timezone"`
	LocalTime string `json:"local_time"`
	Date      string `json:"date"`
	Weekday   string `json:"weekday"`
}

func newCurrentTimeTool(clock Clock) *currentTimeTool {
	return &currentTimeTool{clock: clock}
}
```

Define the Schema with one required `timezone` string, `minLength: 1`, `maxLength: 120`, and `additionalProperties: false`. At the start of `Execute`, return `ctx.Err()` when canceled. Decode `currentTimeArgs`, call `time.LoadLocation(arguments.Timezone)`, convert load failures with `invalidArguments("invalid IANA timezone", err)`, and return the injected clock in that zone.

- [ ] **Step 4: Run current-time and package tests**

```bash
go test ./application/tool/chat -run '^TestCurrentTimeTool' -count=1
go test ./application/tool/chat -count=1
```

Expected: all current-time and chat-tool tests pass; weather tests remain present in this task.

- [ ] **Step 5: Commit only the clean current-time files**

```bash
git commit --only application/tool/chat/current_time.go application/tool/chat/current_time_test.go \
  -m "refactor(chat): make current time network-free"
```

Expected: unrelated staged and unstaged files remain unchanged.

---

### Task 2: Remove `get_weather` and Open-Meteo

**Files:**
- Modify: `application/tool/chat/register.go`
- Modify: `application/tool/chat/register_test.go`
- Modify: `application/web/register.go`
- Modify: `application/web/register_test.go`
- Delete: `application/tool/chat/weather.go`
- Delete: `application/tool/chat/weather_test.go`
- Delete: `application/tool/chat/location.go`
- Delete: `domain/service/weather.go`
- Delete: `infrastructure/driver/openmeteo/client.go`
- Delete: `infrastructure/driver/openmeteo/client_test.go`
- Delete: `infrastructure/driver/openmeteo/register.go`
- Delete: `infrastructure/driver/openmeteo/register_test.go`

**Interfaces:**
- Consumes: `newCurrentTimeTool(clock Clock)` from Task 1.
- Produces: local chat tool set `calculate`, `get_current_time`; Web adds `read`, business tools, and MCP tools at startup.

- [ ] **Step 1: Change tool-list tests to require the local tool boundary**

In `application/tool/chat/register_test.go`, replace the three-tool test and delete the local weather loop test and its provider/stream helpers:

```go
func TestRegisterProvidesLocalChatTools(t *testing.T) {
	var tools []ai.Tool
	app := fxtest.New(t, Register, fx.Invoke(func(params registeredTools) { tools = params.Tools }))
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Definition().Name
		if !tool.Definition().ParallelSafe {
			t.Fatalf("tool %q is not parallel safe", names[i])
		}
	}
	slices.Sort(names)
	if want := []string{"calculate", "get_current_time"}; !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}
```

Update `application/web/register_test.go` expected names to:

```go
wantWithBusiness := []string{"calculate", "course_query", "get_current_time", "read"}
wantGeneral := []string{"calculate", "get_current_time", "read"}
```

- [ ] **Step 2: Run focused tests and confirm `get_weather` is still exposed**

```bash
go test ./application/tool/chat ./application/web -run 'TestRegisterProvidesLocalChatTools|TestAgentRegister' -count=1
```

Expected: failures showing the unexpected `get_weather` tool.

- [ ] **Step 3: Remove weather registration and Open-Meteo Web assembly**

Change `application/tool/chat/register.go` to:

```go
var Register = fx.Options(fx.Provide(
	newSystemClock,
	fx.Annotate(newCurrentTimeTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
	fx.Annotate(newCalculateTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
))
```

Remove the `openmeteo` import and `openmeteo.Register` option from `application/web/register.go`.

- [ ] **Step 4: Delete orphaned weather and location code**

Delete exactly the files listed in this task. Do not retain aliases or deprecated wrappers: after Task 1 no production package should use `LocationResolver`, `WeatherProvider`, `Location`, `Forecast`, or `DailyForecast`.

- [ ] **Step 5: Verify there are no production references**

```bash
rg -n 'get_weather|openmeteo|Open-Meteo|LocationResolver|WeatherProvider' \
  application domain infrastructure --glob '*.go'
```

Expected: no production matches. Workspace references are handled in Task 4.

- [ ] **Step 6: Run affected package tests**

```bash
go test ./application/tool/chat ./application/web ./domain/... ./infrastructure/... -count=1
```

Expected: all packages pass and Open-Meteo is absent from package discovery.

- [ ] **Step 7: Commit only Task 2 paths**

```bash
git commit --only \
  application/tool/chat/register.go application/tool/chat/register_test.go \
  application/tool/chat/weather.go application/tool/chat/weather_test.go application/tool/chat/location.go \
  application/web/register.go application/web/register_test.go domain/service/weather.go \
  infrastructure/driver/openmeteo \
  -m "refactor(chat): route weather away from Open-Meteo"
```

---

### Task 3: Require the Exa MCP Contract in the Web Application

**Files:**
- Modify: `application/web/register.go`
- Modify: `application/web/register_test.go`

**Interfaces:**
- Consumes: normalized `config.MCPServerConfig` and generic `newMCPExtensions`.
- Produces: `validateRequiredExa(config.MCPConfig) error`, called by `validateConfig`.

- [ ] **Step 1: Add failing tests for missing and malformed Exa configuration**

Add this helper to `application/web/register_test.go`:

```go
func validWebConfig() *config.Config {
	return &config.Config{
		Conversation: config.ConversationConfig{Enabled: true},
		HTTP:         config.HTTPConfig{Host: "127.0.0.1"},
		MCP: config.MCPConfig{Servers: []config.MCPServerConfig{{
			Name: "exa", Enabled: true, Required: true, URL: "https://mcp.exa.ai/mcp", Timeout: 60,
			HeaderEnv: map[string]string{"X-Api-Key": "EXA_API_KEY"},
			AllowTools: []string{"web_search_exa", "web_fetch_exa"},
		}}},
	}
}
```

Test these mutations and error fragments:

```go
tests := []struct {
	name, want string
	mutate     func(*config.Config)
}{
	{name: "missing Exa", want: "required Exa MCP", mutate: func(c *config.Config) { c.MCP.Servers = nil }},
	{name: "disabled Exa", want: "enabled", mutate: func(c *config.Config) { c.MCP.Servers[0].Enabled = false }},
	{name: "wrong URL", want: "https://mcp.exa.ai/mcp", mutate: func(c *config.Config) { c.MCP.Servers[0].URL = "https://example.test/mcp" }},
	{name: "missing search", want: "web_search_exa", mutate: func(c *config.Config) { c.MCP.Servers[0].AllowTools = []string{"web_fetch_exa"} }},
	{name: "missing fetch", want: "web_fetch_exa", mutate: func(c *config.Config) { c.MCP.Servers[0].AllowTools = []string{"web_search_exa"} }},
	{name: "extra tool", want: "exactly", mutate: func(c *config.Config) { c.MCP.Servers[0].AllowTools = []string{"web_search_exa", "web_fetch_exa", "other"} }},
	{name: "prefixed tools", want: "tool_prefix", mutate: func(c *config.Config) { c.MCP.Servers[0].ToolPrefix = "exa" }},
	{name: "wrong key env", want: "EXA_API_KEY", mutate: func(c *config.Config) { c.MCP.Servers[0].HeaderEnv = map[string]string{"X-Api-Key": "OTHER_KEY"} }},
}
```

Update the existing positive loopback assertion to call `validateConfig(validWebConfig())`. Keep the existing nil, disabled-persistence, and public-bind cases so their earlier validation errors remain covered.

- [ ] **Step 2: Run validation tests and confirm missing Exa is accepted**

```bash
go test ./application/web -run '^TestValidateConfig' -count=1
```

Expected: new Exa cases fail because current validation checks only persistence and loopback binding.

- [ ] **Step 3: Implement exact Exa policy validation**

Add to `application/web/register.go`:

```go
const exaMCPEndpoint = "https://mcp.exa.ai/mcp"

func validateRequiredExa(mcpConfig config.MCPConfig) error {
	for _, server := range mcpConfig.Servers {
		if server.Name != "exa" {
			continue
		}
		if !server.Enabled || !server.Required {
			return errors.New("required Exa MCP server must be enabled")
		}
		if server.URL != exaMCPEndpoint {
			return fmt.Errorf("required Exa MCP URL must be %s", exaMCPEndpoint)
		}
		if server.ToolPrefix != "" {
			return errors.New("required Exa MCP tool_prefix must be empty")
		}
		if len(server.AllowTools) != 2 || !slices.Contains(server.AllowTools, "web_search_exa") ||
			!slices.Contains(server.AllowTools, "web_fetch_exa") {
			return errors.New("required Exa MCP must allow exactly web_search_exa and web_fetch_exa")
		}
		if len(server.HeaderEnv) != 1 || server.HeaderEnv["X-Api-Key"] != "EXA_API_KEY" {
			return errors.New("required Exa MCP X-Api-Key must use EXA_API_KEY")
		}
		return nil
	}
	return errors.New("web server requires required Exa MCP configuration")
}
```

Call `validateRequiredExa(cfg.MCP)` only after conversation persistence and loopback-host validation have succeeded. Replace the current early `return nil` for `localhost` with `return validateRequiredExa(cfg.MCP)`, and return the same helper after a parsed loopback IP succeeds. This preserves the existing error priority for nil config, disabled persistence, and public binding. Read the normalized `HeaderEnv` entry using the canonical key `X-Api-Key`; `config.MCPServerConfig.normalizeHeaders` already canonicalizes configured Header names. Keep `newMCPExtensions` generic so loopback tests and future non-information extensions remain reusable.

- [ ] **Step 4: Run Web configuration and MCP tests**

```bash
go test ./application/web -run 'TestValidateConfig|TestNewMCPExtensions|TestWebMCP' -count=1
```

Expected: all tests pass; only production `validateConfig` enforces Hosted Exa.

- [ ] **Step 5: Commit the Web Exa policy**

```bash
git commit --only application/web/register.go application/web/register_test.go \
  -m "feat(web): require Exa for external information"
```

---

### Task 4: Route Workspace Current Information Through Exa

**Files:**
- Modify: `workspaces/chat/AGENTS.md`
- Modify: `workspaces/chat/skills/weather-assistance/SKILL.md`
- Modify: `application/web/workspace_test.go`

**Interfaces:**
- Consumes: `web_search_exa`, `web_fetch_exa`, and local `get_current_time`.
- Produces: Workspace instructions that forbid public-information fallback.

**Dirty-file rule:** All three files already contain user changes. Read each immediately before patching, preserve its current Profile/Skill-contract structure, and apply only the Exa lines below. Do not commit these files in this task because doing so would also commit the user's pre-existing work.

- [ ] **Step 1: Add a failing Workspace contract test**

Extend `application/web/workspace_test.go`:

```go
func mustReadWorkspaceFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestDefaultWorkspaceRoutesPublicInformationThroughExa(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	agents := mustReadWorkspaceFile(t, filepath.Join(workspace, "AGENTS.md"))
	weather := mustReadWorkspaceFile(t, filepath.Join(workspace, "skills", "weather-assistance", "SKILL.md"))
	for _, fragment := range []string{"web_search_exa", "web_fetch_exa", "不回退到其他公网数据源"} {
		if !strings.Contains(agents+weather, fragment) {
			t.Errorf("Exa-only Workspace contract missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"`get_weather`", "Open-Meteo"} {
		if strings.Contains(weather, forbidden) {
			t.Errorf("weather Skill retains forbidden dependency %q", forbidden)
		}
	}
}
```

- [ ] **Step 2: Run the contract test and confirm the old workflow fails**

```bash
go test ./application/web -run '^TestDefaultWorkspaceRoutesPublicInformationThroughExa$' -count=1
```

Expected: failure because the current Skill still requires `get_weather`.

- [ ] **Step 3: Amend global Workspace policy without replacing user content**

Replace the current-information bullet in the existing “事实与工具纪律” section with:

```markdown
- 天气、实时价格、现行政策、新闻和网页资料等公网当前信息，必须通过 `web_search_exa` 查询；需要核对原文时使用 `web_fetch_exa`。Exa 失败或证据不足时明确无法确认，不回退到其他公网数据源或模型记忆。
- 当前时间必须来自 `get_current_time`；地点对应的 IANA 时区不确定时，先通过 Exa 查询，再调用本地时间工具。
```

- [ ] **Step 4: Rewrite weather Skill tool rules while preserving its headings**

Keep the folded YAML description and nine required Chinese headings. Use these core rules:

```markdown
## 硬门禁

- 实时天气或预报必须调用 `web_search_exa`，不用模型记忆猜测。
- 搜索摘要不足以支持结论时，必须对选中的可信来源调用 `web_fetch_exa`。
- Exa 失败、来源过旧或可信来源冲突时，明确说明无法确认；不回退到其他公网数据源。

## 执行流程

1. 解析地点和相对日期；相对日期必须结合当前会话日期转换为明确日期。
2. 使用“地点 + 明确日期 + 用户关心的天气指标”调用 `web_search_exa`。
3. 优先选择气象机构、政府部门或可信天气服务；摘要不足时调用 `web_fetch_exa`。
4. 多个可信来源冲突时分别说明，不拼接成一份虚构预报。
5. 先给天气结论，再给与证据一致的穿衣、出行或活动建议，并注明来源和查询时间。
```

Update the output contract, references, examples, and common errors so no line mentions `get_weather`, Open-Meteo, `ambiguous`, or `not_found`.

- [ ] **Step 5: Run Workspace tests**

```bash
go test ./application/web -run 'TestDefaultWorkspace|TestAgentProfileSkill' -count=1
```

Expected: Exa contract, Skill structure, routing corpus, and current Profile tests pass.

- [ ] **Step 6: Preserve dirty-file ownership**

```bash
git status --short -- workspaces/chat/AGENTS.md \
  workspaces/chat/skills/weather-assistance/SKILL.md application/web/workspace_test.go
```

Expected: all three remain modified. Do not stage or commit them.

---

### Task 5: Enforce the Boundary and Verify Exa Execution

**Files:**
- Modify: `application/package_boundary_test.go`
- Modify: `application/web/mcp_test.go`

**Interfaces:**
- Consumes: existing Web MCP test app and ToolRuntime.
- Produces: direct-import boundary tests and an MCP `tools/call` integration test.

- [ ] **Step 1: Add direct-import boundary tests**

Add to `application/package_boundary_test.go`:

```go
func goListImports(t *testing.T, pkg string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg)
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list imports %s: %v: %s", pkg, err, strings.TrimSpace(string(output)))
	}
	return strings.Fields(string(output))
}

func TestAgentPublicInformationUsesMCPBoundary(t *testing.T) {
	if slices.Contains(goListImports(t, "./application/tool/chat"), "net/http") {
		t.Fatal("application/tool/chat directly imports net/http")
	}
	if slices.Contains(goListImports(t, "./application/web"), "github.com/PycMono/go-reagent/infrastructure/driver/openmeteo") {
		t.Fatal("application/web still imports Open-Meteo")
	}
	if _, err := os.Stat(filepath.Join("..", "infrastructure", "driver", "openmeteo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open-Meteo production package still exists: %v", err)
	}
}
```

- [ ] **Step 2: Run the boundary test**

```bash
go test ./application -run '^TestAgentPublicInformationUsesMCPBoundary$' -count=1
```

Expected after Task 2: PASS. To prove the guard, temporarily restore only `infrastructure/driver/openmeteo/register.go`, verify the test fails, then delete that temporary restore again before continuing.

- [ ] **Step 3: Extend the mock MCP server for `tools/call`**

In `application/web/mcp_test.go`, add:

```go
type webMCPRecorder struct {
	toolCalls atomic.Int32
	lastTool  atomic.Value
	failCalls atomic.Bool
}
```

Decode `params.name`. For `tools/call`, record the name and return:

```go
response["result"] = map[string]any{
	"content": []any{map[string]any{
		"type": "text",
		"text": "Shanghai weather source: example.test/weather",
	}},
}
```

Change `newWebMCPTestServer` to return `(*httptest.Server, *webMCPRecorder)` and update existing callers.

- [ ] **Step 4: Add Web runtime Exa execution coverage**

```go
func TestWebMCPExecutesWeatherSearchThroughExa(t *testing.T) {
	server, recorder := newWebMCPTestServer(t, true)
	var consumerStarted atomic.Bool
	app, runtime := newWebMCPTestApp(t, server.URL, &consumerStarted)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	result, err := runtime.Execute(context.Background(), ai.ToolCall{
		ID: "weather-search-1", Name: "web_search_exa",
		Arguments: json.RawMessage(`{"query":"Shanghai weather 2026-08-19"}`),
	}, nil)
	if err != nil || result.IsError {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if recorder.toolCalls.Load() != 1 || recorder.lastTool.Load() != "web_search_exa" {
		t.Fatalf("tool calls = %d, last = %#v", recorder.toolCalls.Load(), recorder.lastTool.Load())
	}
}
```

- [ ] **Step 5: Verify runtime failure has no weather fallback**

Make the mock handler return JSON-RPC error `-32000` with message `search unavailable` when `recorder.failCalls.Load()` is true. Add:

```go
func TestWebMCPFailureHasNoPublicInformationFallback(t *testing.T) {
	server, recorder := newWebMCPTestServer(t, true)
	recorder.failCalls.Store(true)
	var consumerStarted atomic.Bool
	app, runtime := newWebMCPTestApp(t, server.URL, &consumerStarted)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	result, err := runtime.Execute(context.Background(), ai.ToolCall{
		ID: "failed-search-1", Name: "web_search_exa",
		Arguments: json.RawMessage(`{"query":"Shanghai weather"}`),
	}, nil)
	if err != nil || !result.IsError {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if slices.Contains(webMCPDefinitionNames(runtime.Definitions()), "get_weather") {
		t.Fatal("runtime exposed forbidden get_weather fallback")
	}
	if recorder.toolCalls.Load() != 1 {
		t.Fatalf("tool calls = %d", recorder.toolCalls.Load())
	}
}
```

- [ ] **Step 6: Run integration and package tests**

```bash
go test ./application ./application/web ./pi/mcp ./pi/test -count=1
```

Expected: all tests pass without public network access.

- [ ] **Step 7: Commit clean test files**

```bash
git commit --only application/package_boundary_test.go application/web/mcp_test.go \
  -m "test: enforce Exa-only information boundary"
```

---

### Task 6: Full Verification and Local Runtime Configuration

**Files:**
- Verify: all Task 1-5 paths
- Local-only: ignored `config.json`, when present

**Interfaces:**
- Consumes: completed Exa-only implementation.
- Produces: a green test suite and explicit local startup prerequisites.

- [ ] **Step 1: Confirm tracked source contains no Open-Meteo or `get_weather`**

```bash
rg -n 'get_weather|openmeteo|Open-Meteo|api\.open-meteo\.com|geocoding-api\.open-meteo\.com' \
  application domain infrastructure workspaces --glob '*.go' --glob '*.md'
```

Expected: no matches. Historical design documents under `docs/` are intentionally excluded.

- [ ] **Step 2: Confirm local config has the Exa block without printing secrets**

```bash
jq '{exa:[.mcp.servers[]? | select(.name == "exa") | {enabled,required,url,header_env,allow_tools,tool_prefix}]}' config.json
```

Expected: one enabled, required Exa server matching the spec. If the ignored local file lacks it, add only the public fields from `config.example.json`; never insert the API key value.

- [ ] **Step 3: Format and run focused tests**

```bash
gofmt -w application/tool/chat/current_time.go application/tool/chat/current_time_test.go \
  application/tool/chat/register.go application/tool/chat/register_test.go \
  application/web/register.go application/web/register_test.go application/web/mcp_test.go \
  application/package_boundary_test.go
go test ./application/tool/chat ./application/web ./application ./pi/mcp ./pi/test -count=1
```

Expected: formatting produces no semantic changes and focused tests pass.

- [ ] **Step 4: Run full non-network verification**

```bash
GOMODCACHE=/tmp/go-reagent-mcp-mod-cache GOCACHE=/tmp/go-reagent-mcp-go-cache \
  go test ./... -count=1 -timeout=180s
GOMODCACHE=/tmp/go-reagent-mcp-mod-cache GOCACHE=/tmp/go-reagent-mcp-go-cache \
  go test -race ./... -count=1 -timeout=300s
GOMODCACHE=/tmp/go-reagent-mcp-mod-cache GOCACHE=/tmp/go-reagent-mcp-go-cache \
  go vet ./...
git diff --check
```

Expected: every command exits zero. The opt-in Exa smoke test skips unless both its explicit switch and `EXA_API_KEY` are set.

- [ ] **Step 5: Review final diff without disturbing user changes**

```bash
git status --short
git diff -- application/tool/chat application/web/register.go application/web/register_test.go \
  application/web/mcp_test.go application/package_boundary_test.go domain/service \
  infrastructure/driver/openmeteo workspaces/chat/AGENTS.md \
  workspaces/chat/skills/weather-assistance/SKILL.md application/web/workspace_test.go
```

Expected: implementation matches the spec; unrelated pre-existing changes and the staged `skills/repository-development/SKILL.md` deletion remain intact.

- [ ] **Step 6: Do not create a catch-all commit**

Tasks 1, 2, 3, and 5 commit clean implementation paths. Leave Task 4's overlapping Workspace changes uncommitted so the user's existing edits are not silently absorbed into this feature. Report those exact paths during handoff.
