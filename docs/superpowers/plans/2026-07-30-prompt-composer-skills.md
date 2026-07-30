# Prompt Composer and Skill Loader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dynamically compose the go-reagent System Prompt from a safe core prompt, workspace `AGENTS.md`, and YAML-frontmatter Agent Skills, while exposing all lifecycle events through the terminal Reporter.

**Architecture:** A new `internal/context` package owns Skill parsing/loading and Prompt composition. `AgentEngine` constructs one Composer from its workspace and calls `Build` at the start of every Run; the existing two-phase loop remains unchanged. The terminal Reporter serializes complete event messages to an injected writer so parallel callbacks remain readable and testable.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, standard library filesystem/string/I/O packages, Go `testing` package.

## Global Constraints

- Use `gopkg.in/yaml.v3` directly; do not use Configor to parse Markdown Frontmatter.
- Only a first-line, line-delimited `---` block is Frontmatter; support LF and CRLF.
- Invalid Skills are skipped without failing an Agent Run; valid sibling Skills still load.
- Build Prompt content on every `AgentEngine.Run`; do not cache `AGENTS.md` or Skill contents.
- Do not mention unavailable `bash`, `write_file`, `ls`, or `test -f` tools in the core Prompt.
- Preserve the existing Thinking/Action loop, tool scheduling, Provider, Schema, and WeCom behavior.
- Implement every production behavior through a witnessed RED-GREEN TDD cycle.

---

### Task 1: YAML Skill Parser and Deterministic Loader

**Files:**
- Create: `internal/context/skill.go`
- Create: `internal/context/skill_test.go`
- Modify: `go.mod:5-33`
- Modify: `go.sum`

**Interfaces:**
- Produces: `type Skill struct { Name, Description, Body string }`
- Produces: `func NewSkillLoader(workDir string) *SkillLoader`
- Produces: `func (s *SkillLoader) LoadAll() string`
- Package-private: `func parseSkillMD(content string) (Skill, error)`

- [ ] **Step 1: Write failing parser tests**

Create `internal/context/skill_test.go` in package `context` with table tests that assert:

```go
func TestParseSkillMD(t *testing.T) {
    tests := []struct {
        name    string
        content string
        want    Skill
        wantErr bool
    }{
        {
            name: "YAML metadata and body separator",
            content: "---\nname: review\ndescription: |\n  Review code carefully.\n  Report concrete risks.\n---\n# Guide\nKeep this --- marker in the body.\n---\nDone.",
            want: Skill{Name: "review", Description: "Review code carefully.\nReport concrete risks.", Body: "# Guide\nKeep this --- marker in the body.\n---\nDone."},
        },
        {
            name: "CRLF and quoted values",
            content: "---\r\nname: \"release\"\r\ndescription: 'Ship safely'\r\n---\r\nRun checks.\r\n",
            want: Skill{Name: "release", Description: "Ship safely", Body: "Run checks."},
        },
        {
            name: "plain Markdown",
            content: "# Plain skill\nNo frontmatter.",
            want: Skill{Name: "Unknown Skill", Description: "No description provided.", Body: "# Plain skill\nNo frontmatter."},
        },
        {
            name: "empty metadata uses defaults",
            content: "---\nname: '  '\ndescription: ''\n---\nBody",
            want: Skill{Name: "Unknown Skill", Description: "No description provided.", Body: "Body"},
        },
        {name: "unclosed frontmatter", content: "---\nname: broken", wantErr: true},
        {name: "invalid YAML", content: "---\nname: [broken\n---\nBody", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := parseSkillMD(tt.content)
            if (err != nil) != tt.wantErr {
                t.Fatalf("parseSkillMD() error = %v, wantErr %v", err, tt.wantErr)
            }
            if !tt.wantErr && got != tt.want {
                t.Fatalf("parseSkillMD() = %#v, want %#v", got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run parser tests and verify RED**

Run: `go test ./internal/context -run TestParseSkillMD -count=1`

Expected: FAIL because package `internal/context` and `parseSkillMD` do not exist.

- [ ] **Step 3: Add the direct YAML dependency and minimal parser**

Move `gopkg.in/yaml.v3 v3.0.1` into the first `require` block in `go.mod`. Create `skill.go` with default constants, YAML tags, exact delimiter-line detection, CRLF normalization, `yaml.Unmarshal`, `strings.TrimSpace` for metadata/body, and explicit errors for unclosed or malformed Frontmatter:

```go
package context

import (
    "errors"
    "fmt"
    "strings"

    "gopkg.in/yaml.v3"
)

const (
    defaultSkillName = "Unknown Skill"
    defaultSkillDescription = "No description provided."
)

type Skill struct {
    Name string
    Description string
    Body string
}

type skillMetadata struct {
    Name string `yaml:"name"`
    Description string `yaml:"description"`
}

func parseSkillMD(content string) (Skill, error) {
    skill := Skill{Name: defaultSkillName, Description: defaultSkillDescription, Body: content}
    normalized := strings.ReplaceAll(content, "\r\n", "\n")
    lines := strings.Split(normalized, "\n")
    if len(lines) == 0 || lines[0] != "---" {
        return skill, nil
    }

    closing := -1
    for index := 1; index < len(lines); index++ {
        if lines[index] == "---" {
            closing = index
            break
        }
    }
    if closing == -1 {
        return Skill{}, errors.New("skill frontmatter is not closed")
    }

    var metadata skillMetadata
    if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &metadata); err != nil {
        return Skill{}, fmt.Errorf("parse skill frontmatter: %w", err)
    }
    if name := strings.TrimSpace(metadata.Name); name != "" {
        skill.Name = name
    }
    if description := strings.TrimSpace(metadata.Description); description != "" {
        skill.Description = description
    }
    skill.Body = strings.TrimSpace(strings.Join(lines[closing+1:], "\n"))
    return skill, nil
}
```

- [ ] **Step 4: Run parser tests and verify GREEN**

Run: `go test ./internal/context -run TestParseSkillMD -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing loader tests**

Add helpers using `os.MkdirAll` and `os.WriteFile` and tests that create `alpha/SKILL.md`, malformed `middle/SKILL.md`, `zeta/SKILL.md`, and an ignored lowercase filename. Assert `LoadAll` includes only valid Skills, alpha appears before zeta, the header appears exactly once, and missing `.claw/skills` returns `""`:

```go
func writeSkill(t *testing.T, workDir string, relativePath string, content string) {
    t.Helper()
    path := filepath.Join(workDir, ".claw", "skills", filepath.FromSlash(relativePath))
    if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
        t.Fatalf("MkdirAll() error = %v", err)
    }
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
        t.Fatalf("WriteFile() error = %v", err)
    }
}

func TestSkillLoaderLoadsValidSkillsInPathOrder(t *testing.T) {
    workDir := t.TempDir()
    writeSkill(t, workDir, "zeta/SKILL.md", "---\nname: Zeta\ndescription: Last\n---\nZ body")
    writeSkill(t, workDir, "alpha/SKILL.md", "---\nname: Alpha\ndescription: First\n---\nA body")
    writeSkill(t, workDir, "middle/SKILL.md", "---\nname: [broken\n---\nBad")
    writeSkill(t, workDir, "ignored/skill.md", "---\nname: ignored\n---\nignored")

    got := NewSkillLoader(workDir).LoadAll()
    if strings.Count(got, "### 可用专业技能 (Agent Skills)") != 1 {
        t.Fatalf("skill header count in %q", got)
    }
    alpha := strings.Index(got, "#### 技能名称: Alpha")
    zeta := strings.Index(got, "#### 技能名称: Zeta")
    if alpha < 0 || zeta < 0 || alpha >= zeta {
        t.Fatalf("skills not rendered in path order: %q", got)
    }
    if strings.Contains(got, "broken") || strings.Contains(got, "ignored") {
        t.Fatalf("invalid or non-SKILL file was rendered: %q", got)
    }
}

func TestSkillLoaderReturnsEmptyWithoutValidSkills(t *testing.T) {
    if got := NewSkillLoader(t.TempDir()).LoadAll(); got != "" {
        t.Fatalf("LoadAll() = %q, want empty", got)
    }
}
```

- [ ] **Step 6: Run loader tests and verify RED**

Run: `go test ./internal/context -run TestSkillLoader -count=1`

Expected: FAIL because `SkillLoader` is undefined.

- [ ] **Step 7: Implement loader and renderer**

Add `io/fs`, `os`, and `path/filepath` to the imports, then implement the loader below. It walks `<workDir>/.claw/skills`, accepts exact `SKILL.md` names, skips unreadable/malformed files, and only renders the heading when at least one valid Skill was parsed:

```go
type SkillLoader struct {
    workDir string
}

func NewSkillLoader(workDir string) *SkillLoader {
    return &SkillLoader{workDir: workDir}
}

func (s *SkillLoader) LoadAll() string {
    skillBaseDir := filepath.Join(s.workDir, ".claw", "skills")
    if _, err := os.Stat(skillBaseDir); err != nil {
        return ""
    }

    skills := make([]Skill, 0)
    if err := filepath.WalkDir(skillBaseDir, func(path string, entry fs.DirEntry, walkErr error) error {
        if walkErr != nil {
            return walkErr
        }
        if entry.IsDir() || entry.Name() != "SKILL.md" {
            return nil
        }
        content, err := os.ReadFile(path)
        if err != nil {
            return nil
        }
        skill, err := parseSkillMD(string(content))
        if err != nil {
            return nil
        }
        skills = append(skills, skill)
        return nil
    }); err != nil || len(skills) == 0 {
        return ""
    }

    var builder strings.Builder
    builder.WriteString("\n### 可用专业技能 (Agent Skills)\n")
    builder.WriteString("以下是你拥有的标准化外挂技能，请在符合 description 描述的场景下严格遵循其正文指令：\n\n")
    for _, skill := range skills {
        fmt.Fprintf(&builder, "#### 技能名称: %s\n", skill.Name)
        fmt.Fprintf(&builder, "**触发条件**: %s\n\n", skill.Description)
        builder.WriteString("**执行指南**:\n")
        builder.WriteString(skill.Body)
        builder.WriteString("\n\n---\n")
    }
    return builder.String()
}
```

If `os.Stat` or `filepath.WalkDir` fails, return `""`. Do not use a length heuristic.

- [ ] **Step 8: Run Task 1 tests and tidy dependencies**

Run: `gofmt -w internal/context/skill.go internal/context/skill_test.go && go mod tidy && go test ./internal/context -count=1`

Expected: PASS; `gopkg.in/yaml.v3` is a direct dependency.

- [ ] **Step 9: Commit Task 1**

```bash
git add go.mod go.sum internal/context/skill.go internal/context/skill_test.go
git commit -m "feat: load YAML agent skills"
```

---

### Task 2: Workspace Prompt Composer

**Files:**
- Create: `internal/context/composer.go`
- Create: `internal/context/composer_test.go`

**Interfaces:**
- Consumes: `NewSkillLoader(workDir string) *SkillLoader`, `(*SkillLoader).LoadAll() string`
- Produces: `func NewPromptComposer(workDir string) *PromptComposer`
- Produces: `func (c *PromptComposer) Build() schema.Message`

- [ ] **Step 1: Write failing Composer tests**

Create tests that build from a temporary workspace. Assert Role is system; core text contains `go-reagent`, `Thinking`, “修改文件前”, and “始终使用中文回复”; core text does not instruct use of `write_file` or `bash`; `AGENTS.md` and a valid Skill are included in that order; a missing workspace file produces only the core; and editing `AGENTS.md` between two `Build` calls changes the second result.

```go
func TestPromptComposerBuildsCoreAgentsAndSkillsInOrder(t *testing.T) {
    workDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("Use project conventions."), 0o600); err != nil {
        t.Fatal(err)
    }
    writeSkill(t, workDir, "review/SKILL.md", "---\nname: Review\ndescription: Review changes\n---\nCheck tests.")

    message := NewPromptComposer(workDir).Build()
    if message.Role != schema.RoleSystem {
        t.Fatalf("Role = %q", message.Role)
    }
    core := strings.Index(message.Content, "# 核心身份")
    agents := strings.Index(message.Content, "# 项目专属指南")
    skills := strings.Index(message.Content, "### 可用专业技能")
    if core < 0 || agents <= core || skills <= agents {
        t.Fatalf("prompt sections out of order: %q", message.Content)
    }
    for _, want := range []string{"go-reagent", "Thinking", "修改文件前", "始终使用中文回复", "Use project conventions.", "Review changes", "Check tests."} {
        if !strings.Contains(message.Content, want) {
            t.Fatalf("prompt missing %q", want)
        }
    }
}

func TestPromptComposerDoesNotRequireUnavailableTools(t *testing.T) {
    content := NewPromptComposer(t.TempDir()).Build().Content
    for _, unavailable := range []string{"write_file", "test -f", "ls 或", "调用 bash"} {
        if strings.Contains(content, unavailable) {
            t.Fatalf("core prompt requires unavailable tool %q: %q", unavailable, content)
        }
    }
}

func TestPromptComposerReadsAgentsFileOnEveryBuild(t *testing.T) {
    workDir := t.TempDir()
    path := filepath.Join(workDir, "AGENTS.md")
    composer := NewPromptComposer(workDir)
    if err := os.WriteFile(path, []byte("guide-v1"), 0o600); err != nil {
        t.Fatal(err)
    }
    first := composer.Build().Content
    if err := os.WriteFile(path, []byte("guide-v2"), 0o600); err != nil {
        t.Fatal(err)
    }
    second := composer.Build().Content
    if !strings.Contains(first, "guide-v1") || strings.Contains(first, "guide-v2") {
        t.Fatalf("first Build() = %q", first)
    }
    if !strings.Contains(second, "guide-v2") || strings.Contains(second, "guide-v1") {
        t.Fatalf("second Build() = %q", second)
    }
}
```

- [ ] **Step 2: Run Composer tests and verify RED**

Run: `go test ./internal/context -run TestPromptComposer -count=1`

Expected: FAIL because `PromptComposer` is undefined.

- [ ] **Step 3: Implement PromptComposer**

Create `composer.go` with a raw-string `corePrompt` and the exact assembly order. The core must preserve the current phase contract:

```go
const corePrompt = `# 核心身份
你名叫 go-reagent，是一名经验丰富、注重事实与简洁表达的研发助手。你可以通过当前请求实际提供的工具定义读取、修改和检查工作区内容。

# 核心纪律 (CRITICAL)
1. 只能调用当前请求中实际提供定义的工具，不得虚构或模拟工具调用。
2. 当没有提供工具定义时，你正处于 Thinking 阶段：只能制定计划，不得声称工具已执行，也不得编造文件内容。
3. 修改文件前必须先读取并理解现有内容。
4. 工具执行失败时，应根据真实错误信息修正操作后重试。
5. 获得真实工具结果后，必须以这些 Observation 为依据完成面向用户的回答。
6. 始终使用中文回复，以便清晰传达进展和结论。
`
```

Define the Composer and exact assembly logic as follows; no file bytes are cached:

```go
type PromptComposer struct {
    workDir string
    skillLoader *SkillLoader
}

func NewPromptComposer(workDir string) *PromptComposer {
    return &PromptComposer{
        workDir: workDir,
        skillLoader: NewSkillLoader(workDir),
    }
}

func (c *PromptComposer) Build() schema.Message {
    var builder strings.Builder
    builder.WriteString(corePrompt)

    agentsPath := filepath.Join(c.workDir, "AGENTS.md")
    if content, err := os.ReadFile(agentsPath); err == nil {
        builder.WriteString("\n# 项目专属指南 (来自 AGENTS.md)\n")
        builder.WriteString("以下是当前工作区特有的架构规范与注意事项，你的行为必须符合以下要求：\n")
        builder.WriteString("```markdown\n")
        builder.Write(content)
        builder.WriteString("\n```\n")
    }
    if skills := c.skillLoader.LoadAll(); skills != "" {
        builder.WriteString(skills)
    }
    return schema.Message{Role: schema.RoleSystem, Content: builder.String()}
}
```

- [ ] **Step 4: Run Composer tests and verify GREEN**

Run: `gofmt -w internal/context/composer.go internal/context/composer_test.go && go test ./internal/context -count=1`

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/context/composer.go internal/context/composer_test.go
git commit -m "feat: compose workspace system prompt"
```

---

### Task 3: Inject Composer into AgentEngine

**Files:**
- Modify: `internal/engine/loop.go:3-81`
- Modify: `internal/engine/loop_test.go:108-145`

**Interfaces:**
- Consumes: `context.NewPromptComposer(workDir string) *PromptComposer`
- Consumes: `(*PromptComposer).Build() schema.Message`
- Preserves: `func NewAgentEngine(provider.LLMProvider, tools.Registry, string, bool) *AgentEngine`

- [ ] **Step 1: Write a failing Engine integration test**

Change the initial-context test to use `workDir := t.TempDir()`, write `AGENTS.md` and a Skill there, run one Action response, then assert request message zero contains both workspace sources and message one is the untouched user input:

```go
func TestAgentEngineBuildsWorkspaceContextForEachRun(t *testing.T) {
    workDir := t.TempDir()
    if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("engine-agent-guide-v1"), 0o600); err != nil {
        t.Fatal(err)
    }
    skillDir := filepath.Join(workDir, ".claw", "skills", "engine")
    if err := os.MkdirAll(skillDir, 0o700); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: engine-skill\ndescription: engine-trigger\n---\nengine-body"), 0o600); err != nil {
        t.Fatal(err)
    }

    provider := &fakeProvider{responses: []*schema.Message{
        {Role: schema.RoleAssistant, Content: "done one"},
        {Role: schema.RoleAssistant, Content: "done two"},
    }}
    agentEngine := engine.NewAgentEngine(provider, &fakeRegistry{}, workDir, false)
    if err := agentEngine.Run(context.Background(), "hello one", nil); err != nil {
        t.Fatalf("Run() error = %v", err)
    }
    request := provider.requests[0]
    if len(request) != 2 || request[0].Role != schema.RoleSystem || request[1].Content != "hello one" {
        t.Fatalf("initial context = %#v", request)
    }
    for _, want := range []string{"engine-agent-guide-v1", "engine-skill", "engine-body"} {
        if !strings.Contains(request[0].Content, want) {
            t.Fatalf("system prompt missing %q: %q", want, request[0].Content)
        }
    }

    if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("engine-agent-guide-v2"), 0o600); err != nil {
        t.Fatal(err)
    }
    if err := agentEngine.Run(context.Background(), "hello two", nil); err != nil {
        t.Fatalf("second Run() error = %v", err)
    }
    secondRequest := provider.requests[1]
    if secondRequest[1].Content != "hello two" || !strings.Contains(secondRequest[0].Content, "engine-agent-guide-v2") || strings.Contains(secondRequest[0].Content, "engine-agent-guide-v1") {
        t.Fatalf("second initial context = %#v", secondRequest)
    }
}
```

- [ ] **Step 2: Run the integration test and verify RED**

Run: `go test ./internal/engine -run TestAgentEngineBuildsWorkspaceContextForEachRun -count=1`

Expected: FAIL because the hard-coded System Prompt omits workspace content.

- [ ] **Step 3: Inject and use PromptComposer**

Alias the new package to avoid colliding with the standard `context` import:

```go
ctxpkg "github.com/PycMono/go-reagent/internal/context"
```

Add `composer *ctxpkg.PromptComposer` to `AgentEngine`, initialize it in `NewAgentEngine`, and replace the hard-coded first message with:

```go
systemMessage := e.composer.Build()
contextHistory := []schema.Message{
    systemMessage,
    {Role: schema.RoleUser, Content: userPrompt},
}
```

- [ ] **Step 4: Run Engine tests and verify GREEN**

Run: `gofmt -w internal/engine/loop.go internal/engine/loop_test.go && go test ./internal/engine -count=1`

Expected: PASS, including all existing scheduling, cancellation, validation, logging, and Reporter tests.

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/engine/loop.go internal/engine/loop_test.go
git commit -m "feat: inject prompt composer into engine"
```

---

### Task 4: Complete the Concurrent-Safe Terminal Reporter

**Files:**
- Modify: `internal/engine/terminal_reporter.go`
- Create: `internal/engine/terminal_reporter_test.go`

**Interfaces:**
- Preserves: `func NewTerminalReporter() Reporter`
- Package-private test seam: `func newTerminalReporter(writer io.Writer) Reporter`

- [ ] **Step 1: Write failing lifecycle-output tests**

Create `terminal_reporter_test.go` in package `engine`. Use a `bytes.Buffer` through `newTerminalReporter`, invoke all four methods, and assert the buffer contains Thinking text, tool name, escaped newlines, success/failure text, error details, and Agent message. Add a 151-Chinese-rune argument and assert output is valid UTF-8, contains exactly 150 input runes before the suffix, and contains `... (已截断)`. Assert `OnMessage(ctx, "")` does not change the buffer.

```go
func TestTerminalReporterPrintsLifecycleEvents(t *testing.T) {
    var output bytes.Buffer
    reporter := newTerminalReporter(&output)
    ctx := context.Background()

    reporter.OnThinking(ctx)
    reporter.OnToolCall(ctx, "read_file", "line1\nline2\r")
    reporter.OnToolResult(ctx, "read_file", "ok", false)
    reporter.OnToolResult(ctx, "edit_file", "permission denied", true)
    reporter.OnMessage(ctx, "完成")

    got := output.String()
    for _, want := range []string{"思考中", "read_file", `line1\nline2\r`, "执行成功", "执行失败", "permission denied", "Agent 回复", "完成"} {
        if !strings.Contains(got, want) {
            t.Fatalf("terminal output missing %q: %q", want, got)
        }
    }
}

func TestTerminalDisplayArgumentsTruncatesAtRuneBoundary(t *testing.T) {
    got := terminalDisplayArguments(strings.Repeat("界", 151))
    want := strings.Repeat("界", 150) + "... (已截断)"
    if got != want {
        t.Fatalf("terminalDisplayArguments() = %q, want %q", got, want)
    }
    if !utf8.ValidString(got) {
        t.Fatalf("terminalDisplayArguments() returned invalid UTF-8: %q", got)
    }
}

func TestTerminalReporterIgnoresEmptyMessageAndSerializesConcurrentEvents(t *testing.T) {
    var output bytes.Buffer
    reporter := newTerminalReporter(&output)
    reporter.OnMessage(context.Background(), "")
    if output.Len() != 0 {
        t.Fatalf("empty message output = %q", output.String())
    }

    var waitGroup sync.WaitGroup
    for index := range 16 {
        waitGroup.Add(1)
        go func(index int) {
            defer waitGroup.Done()
            reporter.OnToolCall(context.Background(), fmt.Sprintf("tool-%d", index), "{}")
        }(index)
    }
    waitGroup.Wait()
    if got := strings.Count(output.String(), "[🛠️ 调用工具]"); got != 16 {
        t.Fatalf("tool-call output count = %d, want 16: %q", got, output.String())
    }
}
```

- [ ] **Step 2: Run Reporter tests and verify RED**

Run: `go test ./internal/engine -run TestTerminalReporter -count=1`

Expected: FAIL because `newTerminalReporter` and lifecycle output are absent.

- [ ] **Step 3: Implement serialized output and rune-safe truncation**

Use this structure:

```go
const terminalArgumentLimit = 150

type terminalReporter struct {
    mu sync.Mutex
    writer io.Writer
}

func NewTerminalReporter() Reporter {
    return newTerminalReporter(os.Stdout)
}

func newTerminalReporter(writer io.Writer) Reporter {
    if writer == nil {
        writer = io.Discard
    }
    return &terminalReporter{writer: writer}
}

func (r *terminalReporter) write(message string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    _, _ = io.WriteString(r.writer, message)
}

func terminalDisplayArguments(arguments string) string {
    display := strings.NewReplacer("\n", `\n`, "\r", `\r`).Replace(arguments)
    runes := []rune(display)
    if len(runes) <= terminalArgumentLimit {
        return display
    }
    return string(runes[:terminalArgumentLimit]) + "... (已截断)"
}

func (r *terminalReporter) OnThinking(context.Context) {
    r.write("\n[🤔 思考中] 模型正在推理...\n")
}

func (r *terminalReporter) OnToolCall(_ context.Context, toolName string, arguments string) {
    r.write(fmt.Sprintf("[🛠️ 调用工具] %s\n   参数: %s\n", toolName, terminalDisplayArguments(arguments)))
}

func (r *terminalReporter) OnToolResult(_ context.Context, toolName string, result string, isError bool) {
    if !isError {
        r.write(fmt.Sprintf("[✅ 执行成功] %s\n", toolName))
        return
    }
    message := fmt.Sprintf("[❌ 执行失败] %s\n", toolName)
    if result != "" {
        message += fmt.Sprintf("   错误: %s\n", result)
    }
    r.write(message)
}

func (r *terminalReporter) OnMessage(_ context.Context, content string) {
    if content == "" {
        return
    }
    r.write(fmt.Sprintf("\n🤖 Agent 回复:\n%s\n\n", content))
}
```

Format each callback as one complete string before calling `write`, so the mutex covers the full event. Successful results print the tool name only; failed results additionally print non-empty error text; empty Agent messages write nothing.

- [ ] **Step 4: Run Reporter and race tests and verify GREEN**

Run: `gofmt -w internal/engine/terminal_reporter.go internal/engine/terminal_reporter_test.go && go test -race ./internal/engine -run 'TestTerminalReporter|TestAgentEngineReports' -count=1`

Expected: PASS with no race reports.

- [ ] **Step 5: Commit Task 4**

```bash
git add internal/engine/terminal_reporter.go internal/engine/terminal_reporter_test.go
git commit -m "feat: report agent lifecycle in terminal"
```

---

### Task 5: Full Regression Verification

**Files:**
- Verify all changed files from Tasks 1-4

**Interfaces:**
- Verifies the full repository acceptance criteria; produces no new API.

- [ ] **Step 1: Check formatting**

Run: `gofmt -l cmd internal`

Expected: no output.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 3: Run the complete race-enabled suite**

Run: `go test -race -count=1 ./...`

Expected: all packages PASS with no race reports.

- [ ] **Step 4: Inspect the final patch and dependency classification**

Run: `git diff --check HEAD~4..HEAD && go mod why -m gopkg.in/yaml.v3 && git status --short`

Expected: no whitespace errors; `yaml.v3` is required by `internal/context`; working tree is clean after the four implementation commits.
