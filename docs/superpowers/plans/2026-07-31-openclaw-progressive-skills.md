# OpenClaw-Style Progressive Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace full Skill Body injection with a validated, budgeted Skill catalog whose selected `SKILL.md` files are loaded progressively through paginated `read_file` calls.

**Architecture:** `internal/context` separates strict Frontmatter parsing, multi-source discovery, immutable snapshots, and XML Prompt rendering. `PromptComposer` consumes a snapshot but never reads Skill bodies; `AgentEngine` creates one snapshot per Run and carries Thinking output into Action context. The existing `read_file` remains the only loading tool and gains 1-based line pagination with 2000-line and 50-KiB output limits.

**Tech Stack:** Go 1.26, standard library (`os.Root`, `bufio`, `crypto/sha256`, `encoding/xml`-compatible escaping, `runtime`, `os/exec`, `unicode/utf8`), `gopkg.in/yaml.v3`, Go `testing`.

## Global Constraints

- Keep development on `master`; do not create a worktree or feature branch.
- Never add, edit, or commit the user's untracked `.claw/` tree.
- Discover `skills`, `.agents/skills`, then `.claw/skills` in descending precedence.
- `SkillSummary`, `SkillSnapshot`, and System Prompt must never retain or contain Skill Body text.
- Accept only valid UTF-8 regular `SKILL.md` files no larger than 256 KiB, with strict first-line YAML Frontmatter and non-empty Body.
- Require names matching `^[a-z0-9]+(?:-[a-z0-9]+)*$`, 1–64 runes, and descriptions of 1–1024 runes.
- Use the existing direct dependency `gopkg.in/yaml.v3`; do not introduce another YAML library.
- Prompt catalog limits are 150 Skills and 18,000 Unicode code points, with identity fields taking priority over descriptions.
- Keep `read_file` workspace-relative and `os.Root`-confined; defaults are offset 1, limit 2000, and final output at most 50 KiB including the continuation marker.
- Preserve existing Provider, Registry, Reporter, scheduler, and dispatch behavior except for the documented Thinking-to-Action context change.
- Use RED-GREEN TDD for every task and make one focused commit after each independently passing unit.

---

### Task 1: Strict Skill Parser and Immutable Snapshot Types

**Files:**
- Modify: `internal/context/skill.go`
- Modify: `internal/context/skill_test.go`
- Create: `internal/context/skill_snapshot.go`
- Create: `internal/context/skill_snapshot_test.go`

**Interfaces:**
- Produces: `type SkillSummary struct { Name, Description, Location, Version string; Source SkillSource }`
- Produces: `type SkillDiagnostic struct { Path string; Severity DiagnosticSeverity; Code, Message string }`
- Produces: `func (s *SkillSnapshot) Skills() []SkillSummary`
- Produces: `func (s *SkillSnapshot) Diagnostics() []SkillDiagnostic`
- Package-private: `func parseSkillMD(content []byte) (parsedSkill, error)`
- Package-private: `func newSkillSnapshot(skills []SkillSummary, diagnostics []SkillDiagnostic) *SkillSnapshot`

- [ ] **Step 1: Replace permissive parser tests with strict validation tests**

Write table tests covering LF, CRLF, multiline YAML descriptions, missing/unclosed Frontmatter, invalid YAML, missing fields, invalid names, rune boundaries, empty Body, NUL, invalid UTF-8, and top-level `disable-model-invocation`:

```go
func TestParseSkillMD(t *testing.T) {
    valid := []byte("---\r\nname: code-review\r\ndescription: |\r\n  Review code.\r\n  Report risks.\r\ndisable-model-invocation: true\r\n---\r\n# Guide\r\nCheck tests.\r\n")
    got, err := parseSkillMD(valid)
    if err != nil {
        t.Fatalf("parseSkillMD() error = %v", err)
    }
    if got.Name != "code-review" || got.Description != "Review code.\nReport risks." ||
        got.Body != "# Guide\nCheck tests." || !got.DisableModelInvocation {
        t.Fatalf("parseSkillMD() = %#v", got)
    }
}

func TestParseSkillMDRejectsInvalidContent(t *testing.T) {
    tests := []struct{ name, content, wantCode string }{
        {"missing frontmatter", "# Guide\nBody", "skill_frontmatter_missing"},
        {"unclosed frontmatter", "---\nname: broken", "skill_frontmatter_invalid"},
        {"missing name", "---\ndescription: useful\n---\nBody", "skill_name_missing"},
        {"invalid name", "---\nname: Bad_Name\ndescription: useful\n---\nBody", "skill_name_invalid"},
        {"missing description", "---\nname: valid\n---\nBody", "skill_description_missing"},
        {"empty body", "---\nname: valid\ndescription: useful\n---\n  ", "skill_body_empty"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := parseSkillMD([]byte(tt.content))
            var parseErr *skillParseError
            if !errors.As(err, &parseErr) || parseErr.Code != tt.wantCode {
                t.Fatalf("error = %v, want code %q", err, tt.wantCode)
            }
        })
    }
}
```

- [ ] **Step 2: Run parser tests and witness RED**

Run: `go test ./internal/context -run 'TestParseSkillMD' -count=1`

Expected: FAIL because the old parser accepts defaults and its signature/types do not satisfy the strict tests.

- [ ] **Step 3: Implement strict parsing and metadata types**

Replace the old `Skill`/default-value parser with typed Frontmatter and structured parse errors:

```go
const maxSkillFileBytes = 256 * 1024

type parsedSkill struct {
    Name                   string
    Description            string
    Body                   string
    OS                     []string
    RequiredBins           []string
    RequiredEnv            []string
    DisableModelInvocation bool
}

type skillFrontmatter struct {
    Name                   string            `yaml:"name"`
    Description            string            `yaml:"description"`
    DisableModelInvocation bool              `yaml:"disable-model-invocation"`
    Metadata               skillMetadataRoot `yaml:"metadata"`
}

type skillMetadataRoot struct {
    OpenClaw openClawMetadata `yaml:"openclaw"`
}

type openClawMetadata struct {
    OS       []string              `yaml:"os"`
    Requires openClawRequirements  `yaml:"requires"`
}

type openClawRequirements struct {
    Bins []string `yaml:"bins"`
    Env  []string `yaml:"env"`
}

type skillParseError struct {
    Code    string
    Message string
}
```

Normalize CRLF before delimiter detection, use `yaml.Unmarshal`, validate lengths with `utf8.RuneCountInString`, validate NUL/UTF-8 before parsing, and return no fallback name or description.

- [ ] **Step 4: Run parser tests and witness GREEN**

Run: `gofmt -w internal/context/skill.go internal/context/skill_test.go && go test ./internal/context -run 'TestParseSkillMD' -count=1`

Expected: PASS.

- [ ] **Step 5: Write immutable snapshot copy tests**

```go
func TestSkillSnapshotReturnsCopies(t *testing.T) {
    original := []SkillSummary{{Name: "review", Description: "Review", Location: "skills/review/SKILL.md"}}
    snapshot := newSkillSnapshot(original, []SkillDiagnostic{{Code: "sample"}})
    original[0].Name = "mutated-original"

    first := snapshot.Skills()
    first[0].Name = "mutated-copy"
    if got := snapshot.Skills()[0].Name; got != "review" {
        t.Fatalf("snapshot name = %q", got)
    }
    diagnostics := snapshot.Diagnostics()
    diagnostics[0].Code = "mutated-copy"
    if got := snapshot.Diagnostics()[0].Code; got != "sample" {
        t.Fatalf("diagnostic code = %q", got)
    }
}
```

- [ ] **Step 6: Run snapshot test and witness RED**

Run: `go test ./internal/context -run TestSkillSnapshotReturnsCopies -count=1`

Expected: FAIL because `SkillSnapshot` and its accessors do not exist.

- [ ] **Step 7: Implement snapshot and diagnostic value types**

```go
type SkillSnapshot struct {
    skills      []SkillSummary
    diagnostics []SkillDiagnostic
}

func newSkillSnapshot(skills []SkillSummary, diagnostics []SkillDiagnostic) *SkillSnapshot {
    return &SkillSnapshot{
        skills: append([]SkillSummary(nil), skills...),
        diagnostics: append([]SkillDiagnostic(nil), diagnostics...),
    }
}

func (s *SkillSnapshot) Skills() []SkillSummary {
    if s == nil { return nil }
    return append([]SkillSummary(nil), s.skills...)
}

func (s *SkillSnapshot) Diagnostics() []SkillDiagnostic {
    if s == nil { return nil }
    return append([]SkillDiagnostic(nil), s.diagnostics...)
}
```

Define `SkillSource` constants for `skills`, `.agents/skills`, and `.claw/skills`, and `DiagnosticSeverity` constants `info`, `warning`, and `error`.

- [ ] **Step 8: Run Task 1 tests and commit**

Run: `gofmt -w internal/context/skill*.go && go test ./internal/context -run 'TestParseSkillMD|TestSkillSnapshot' -count=1`

Expected: PASS.

```bash
git add internal/context/skill.go internal/context/skill_test.go internal/context/skill_snapshot.go internal/context/skill_snapshot_test.go
git commit -m "refactor: model validated skill metadata"
```

---

### Task 2: Multi-Source Skill Discovery, Filtering, and Precedence

**Files:**
- Create: `internal/context/skill_discovery.go`
- Create: `internal/context/skill_discovery_test.go`
- Modify: `internal/context/skill.go`
- Remove obsolete loader assertions from: `internal/context/skill_test.go`

**Interfaces:**
- Consumes: `parseSkillMD([]byte) (parsedSkill, error)` and `newSkillSnapshot`
- Produces: `func NewSkillLoader(workDir string) *SkillLoader`
- Produces: `func (s *SkillLoader) Discover(env SkillEnvironment) (*SkillSnapshot, error)`
- Produces: `type SkillEnvironment struct { GOOS string; EnvLookup, BinLookup func(string) bool }`
- Produces: `func DefaultSkillEnvironment() SkillEnvironment`

- [ ] **Step 1: Write discovery source, conflict, and version tests**

Create helpers that accept the full workspace-relative destination rather than always writing beneath `.claw/skills`:

```go
func writeWorkspaceSkill(t *testing.T, workDir, relativePath, name, description, body string) {
    t.Helper()
    path := filepath.Join(workDir, filepath.FromSlash(relativePath))
    if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { t.Fatal(err) }
    content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, description, body)
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil { t.Fatal(err) }
}

func TestSkillLoaderDiscoversSourcesWithPrecedence(t *testing.T) {
    workDir := t.TempDir()
    writeWorkspaceSkill(t, workDir, ".claw/skills/review/SKILL.md", "review", "legacy", "legacy-body")
    writeWorkspaceSkill(t, workDir, ".agents/skills/review/SKILL.md", "review", "agents", "agents-body")
    writeWorkspaceSkill(t, workDir, "skills/review/SKILL.md", "review", "workspace", "workspace-body")

    snapshot, err := NewSkillLoader(workDir).Discover(testSkillEnvironment())
    if err != nil { t.Fatal(err) }
    skills := snapshot.Skills()
    if len(skills) != 1 || skills[0].Description != "workspace" || skills[0].Location != "skills/review/SKILL.md" {
        t.Fatalf("skills = %#v", skills)
    }
    if strings.Contains(fmt.Sprint(skills), "body") { t.Fatalf("snapshot retained body: %#v", skills) }
    requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_shadowed")
}
```

Add cases for same-source duplicate names (all conflicting entries excluded), stable name/location ordering, exact `SKILL.md` filename matching, missing directories returning a non-nil empty snapshot, SHA-256 version stability/change, malformed siblings producing diagnostics without failing discovery, 256-KiB rejection, and internal/external symlink rejection.

Assert the relevant exact codes: `skill_duplicate_name`, `skill_shadowed`, `skill_file_too_large`, `skill_not_utf8`, and `skill_binary_content`.

- [ ] **Step 2: Run source discovery tests and witness RED**

Run: `go test ./internal/context -run 'TestSkillLoaderDiscovers|TestSkillLoaderRejects|TestSkillLoaderVersion' -count=1`

Expected: FAIL because `Discover` and multi-source merging are absent.

- [ ] **Step 3: Implement bounded source scanning and deterministic merge**

Use these source descriptors and keep the parsed Body local to each candidate only:

```go
var skillSources = []skillSourceSpec{
    {Source: SkillSourceWorkspace, Directory: "skills", Priority: 3},
    {Source: SkillSourceAgents, Directory: ".agents/skills", Priority: 2},
    {Source: SkillSourceClaw, Directory: ".claw/skills", Priority: 1},
}

type discoveredSkill struct {
    Summary SkillSummary
    Parsed  parsedSkill
}
```

Open the workspace with `os.OpenRoot`, walk `root.FS()` using slash-relative paths, reject entries whose `Lstat` mode is not regular, read at most `maxSkillFileBytes+1`, compute `sha256:<first 16 hex>` from the original bytes, and convert parser failures into `SkillDiagnostic`. Group candidates by source and normalized name before precedence merging so a same-source collision cannot be resolved by path order.

- [ ] **Step 4: Run source discovery tests and witness GREEN**

Run: `gofmt -w internal/context/skill_discovery.go internal/context/skill_discovery_test.go && go test ./internal/context -run 'TestSkillLoaderDiscovers|TestSkillLoaderRejects|TestSkillLoaderVersion' -count=1`

Expected: PASS.

- [ ] **Step 5: Write OS, binary, environment, and invocation policy tests**

```go
func TestSkillLoaderFiltersIneligibleSkills(t *testing.T) {
    workDir := t.TempDir()
    writeRawWorkspaceSkill(t, workDir, "skills/image/SKILL.md", `---
name: image
description: Image operations
metadata:
  openclaw:
    os: [darwin]
    requires:
      bins: [ffmpeg]
      env: [IMAGE_TOKEN]
---
Body`)
    env := SkillEnvironment{
        GOOS: "linux",
        BinLookup: func(string) bool { return false },
        EnvLookup: func(string) bool { return false },
    }
    snapshot, err := NewSkillLoader(workDir).Discover(env)
    if err != nil { t.Fatal(err) }
    if len(snapshot.Skills()) != 0 { t.Fatalf("skills = %#v", snapshot.Skills()) }
    requireDiagnosticCodes(t, snapshot.Diagnostics(), "skill_os_ineligible")
}
```

Add separate cases for missing binaries, missing environment variables, top-level `disable-model-invocation: true`, `runtime.GOOS == windows` mapping to `win32`, and eligible metadata entering the snapshot. Assert the exact codes `skill_missing_binary`, `skill_missing_environment`, and `skill_model_invocation_disabled`; diagnostics must never contain environment values or Body text.

- [ ] **Step 6: Run eligibility tests and witness RED**

Run: `go test ./internal/context -run TestSkillLoaderFilters -count=1`

Expected: FAIL until policy checks are applied.

- [ ] **Step 7: Implement deterministic eligibility checks**

```go
type SkillEnvironment struct {
    GOOS      string
    EnvLookup func(string) bool
    BinLookup func(string) bool
}

func DefaultSkillEnvironment() SkillEnvironment {
    return SkillEnvironment{
        GOOS: runtime.GOOS,
        EnvLookup: func(name string) bool { _, ok := os.LookupEnv(name); return ok },
        BinLookup: func(name string) bool { _, err := exec.LookPath(name); return err == nil },
    }
}
```

Normalize `windows` to `win32`, treat empty requirements as satisfied, stop at the first failed category per Skill, and emit the exact diagnostic code without environment values.

- [ ] **Step 8: Run all context discovery tests and commit**

Run: `gofmt -w internal/context/skill*.go && go test ./internal/context -count=1`

Expected: PASS.

```bash
git add internal/context/skill.go internal/context/skill_test.go internal/context/skill_discovery.go internal/context/skill_discovery_test.go
git commit -m "feat: discover eligible workspace skills"
```

---

### Task 3: Budgeted XML Skill Catalog and Prompt Composer

**Files:**
- Create: `internal/context/skill_prompt.go`
- Create: `internal/context/skill_prompt_test.go`
- Modify: `internal/context/composer.go`
- Modify: `internal/context/composer_test.go`

**Interfaces:**
- Consumes: `(*SkillSnapshot).Skills() []SkillSummary`
- Produces: `type SkillPromptReport struct { IncludedSkills, OmittedSkills, ShortenedDescriptions int; Truncated bool }`
- Package-private: `func renderSkillPrompt(snapshot *SkillSnapshot) (string, SkillPromptReport)`
- Changes: `func (c *PromptComposer) Build(snapshot *SkillSnapshot) (schema.Message, SkillPromptReport)`

- [ ] **Step 1: Write XML catalog, Body exclusion, ordering, and escaping tests**

```go
func TestRenderSkillPromptContainsOnlyCatalogMetadata(t *testing.T) {
    snapshot := newSkillSnapshot([]SkillSummary{{
        Name: "review", Description: `Review <code> & "tests"`,
        Location: "skills/review/SKILL.md", Version: "sha256:0123456789abcdef",
    }}, nil)
    got, report := renderSkillPrompt(snapshot)
    for _, want := range []string{
        "<available_skills>", "<name>review</name>",
        "Review &lt;code&gt; &amp; &quot;tests&quot;", "skills/review/SKILL.md",
        "sha256:0123456789abcdef", "必须先使用 read_file",
    } {
        if !strings.Contains(got, want) { t.Fatalf("prompt missing %q: %q", want, got) }
    }
    if strings.Contains(got, "execution-body-secret") { t.Fatalf("prompt leaked body: %q", got) }
    if report.IncludedSkills != 1 || report.Truncated { t.Fatalf("report = %#v", report) }
}
```

Add tests for apostrophe escaping, stable Skill order, nil/empty snapshots producing no heading/XML, and the exact progressive-read instructions including continuation and relative-reference behavior.

- [ ] **Step 2: Run basic Prompt tests and witness RED**

Run: `go test ./internal/context -run 'TestRenderSkillPrompt|TestPromptComposer' -count=1`

Expected: FAIL because the catalog renderer and new Composer signature do not exist.

- [ ] **Step 3: Implement XML-safe rendering and Composer injection**

Use constants and a text-node escaper that replaces all five XML-sensitive characters:

```go
const (
    maxSkillsInPrompt = 150
    maxSkillsPromptChars = 18_000
    maxSkillDescriptionChars = 1_024
)

type SkillPromptReport struct {
    IncludedSkills        int
    OmittedSkills         int
    ShortenedDescriptions int
    Truncated             bool
}

var xmlTextReplacer = strings.NewReplacer(
    "&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
)
```

Move Skill loading out of `PromptComposer`: it retains only `workDir`, reads root `AGENTS.md` as before, and appends `renderSkillPrompt(snapshot)`. Return the render report alongside the system message.

- [ ] **Step 4: Run basic Prompt tests and witness GREEN**

Run: `gofmt -w internal/context/skill_prompt.go internal/context/skill_prompt_test.go internal/context/composer.go internal/context/composer_test.go && go test ./internal/context -run 'TestRenderSkillPrompt|TestPromptComposer' -count=1`

Expected: PASS.

- [ ] **Step 5: Write character and count budget tests**

```go
func TestRenderSkillPromptHonorsBudgets(t *testing.T) {
    skills := make([]SkillSummary, 0, maxSkillsInPrompt+10)
    for index := 0; index < maxSkillsInPrompt+10; index++ {
        skills = append(skills, SkillSummary{
            Name: fmt.Sprintf("skill-%03d", index),
            Description: strings.Repeat("说明", 512),
            Location: fmt.Sprintf("skills/skill-%03d/SKILL.md", index),
            Version: "sha256:0123456789abcdef",
        })
    }
    prompt, report := renderSkillPrompt(newSkillSnapshot(skills, nil))
    if utf8.RuneCountInString(prompt) > maxSkillsPromptChars {
        t.Fatalf("prompt runes = %d", utf8.RuneCountInString(prompt))
    }
    if report.IncludedSkills > maxSkillsInPrompt || report.OmittedSkills == 0 || !report.Truncated {
        t.Fatalf("report = %#v", report)
    }
    if !utf8.ValidString(prompt) || !strings.Contains(prompt, "省略") {
        t.Fatalf("invalid budgeted prompt: %q", prompt)
    }
}
```

Add a case proving `name/location/version` for later Skills survive before earlier long descriptions, and a case where identity entries alone force Skill omission.

- [ ] **Step 6: Run budget tests and witness RED**

Run: `go test ./internal/context -run TestRenderSkillPromptHonorsBudgets -count=1`

Expected: FAIL until count/rune budgeting is implemented.

- [ ] **Step 7: Implement identity-first rune budgeting**

Render the fixed instructions/opening/closing and omission note inside the same 18,000-rune budget. Select at most 150 identity-only entries in stable order; then distribute remaining runes to descriptions. For the first description that does not fit, binary-search the longest raw-rune prefix whose XML-escaped entry fits, increment `ShortenedDescriptions`, and keep all cuts on rune boundaries. Set `Truncated` when descriptions are shortened or Skills omitted.

- [ ] **Step 8: Run all context tests and commit**

Run: `gofmt -w internal/context && go test ./internal/context -count=1`

Expected: PASS and no Skill Body appears in any Composer output assertion.

```bash
git add internal/context/composer.go internal/context/composer_test.go internal/context/skill_prompt.go internal/context/skill_prompt_test.go
git commit -m "feat: render budgeted skill catalog"
```

---

### Task 4: OpenClaw-Style Paginated `read_file`

**Files:**
- Modify: `internal/tools/read_file.go`
- Modify: `internal/tools/read_file_test.go`

**Interfaces:**
- Changes arguments to: `type readFileArgs struct { Path string; Offset, Limit *int }`
- Keeps: `func (t *ReadFileTool) Execute(context.Context, json.RawMessage) (string, error)`
- Adds package-private line readers that do not use `bufio.Scanner`.

- [ ] **Step 1: Replace 8000-byte truncation tests with schema and pagination tests**

Assert the JSON Schema exposes integer `offset` and `limit` with minimum 1 and `limit` maximum 2000. Add helpers accepting optional paging fields:

```go
func readFilePageArguments(t *testing.T, path string, offset, limit *int) json.RawMessage {
    t.Helper()
    input := map[string]any{"path": path}
    if offset != nil { input["offset"] = *offset }
    if limit != nil { input["limit"] = *limit }
    arguments, err := json.Marshal(input)
    if err != nil { t.Fatal(err) }
    return arguments
}

func TestReadFileToolPaginatesByLine(t *testing.T) {
    workDir := t.TempDir()
    writeTestFile(t, filepath.Join(workDir, "lines.txt"), []byte("one\ntwo\nthree\nfour\n"))
    tool := newReadFileToolForTest(t, workDir)
    offset, limit := 2, 2
    got, err := tool.Execute(context.Background(), readFilePageArguments(t, "lines.txt", &offset, &limit))
    if err != nil { t.Fatal(err) }
    want := "two\nthree\n\n[Showing lines 2-3. Use offset=4 to continue.]"
    if got != want { t.Fatalf("output = %q, want %q", got, want) }
}
```

Add invalid offset/limit cases, default 2000-line behavior, offset beyond EOF, empty file, final page without marker, trailing newline behavior, and consecutive pages reconstructing the original file with no lost/duplicated lines after stripping markers.

- [ ] **Step 2: Run line pagination tests and witness RED**

Run: `go test ./internal/tools -run 'TestReadFileToolDefinition|TestReadFileToolPaginates|TestReadFileToolRejectsInvalidArguments' -count=1`

Expected: FAIL because `offset`/`limit` are rejected and the old truncation format remains.

- [ ] **Step 3: Implement argument/schema changes and line pagination**

Use these exact limits and marker format:

```go
const (
    defaultReadFileMaxLines = 2000
    defaultReadFileMaxBytes = 50 * 1024
)

func readFileContinuationMarker(start, end, next int) string {
    return fmt.Sprintf("[Showing lines %d-%d. Use offset=%d to continue.]", start, end, next)
}
```

Decode pointers so omission differs from zero. Open with `os.Root`, verify a regular file, skip `offset-1` lines without retaining them, and collect complete lines with `bufio.Reader.ReadSlice` chunk handling. Do not use `bufio.Scanner`. Detect whether more data exists before adding a marker.

Append the marker after exactly one blank separator line without changing the page content: use `"\n" + marker` when the content already ends in `\n`, otherwise use `"\n\n" + marker`. The separator is presentation metadata and must be removed together with the marker when a test reconstructs the original file.

- [ ] **Step 4: Run line pagination tests and witness GREEN**

Run: `gofmt -w internal/tools/read_file.go internal/tools/read_file_test.go && go test ./internal/tools -run 'TestReadFileToolDefinition|TestReadFileToolPaginates|TestReadFileToolRejectsInvalidArguments' -count=1`

Expected: PASS.

- [ ] **Step 5: Write 50-KiB, UTF-8, NUL, cancellation, and long-line tests**

```go
func TestReadFileToolLimitsFinalOutputTo50KiB(t *testing.T) {
    workDir := t.TempDir()
    content := strings.Repeat("中文内容\n", 7000)
    writeTestFile(t, filepath.Join(workDir, "large.txt"), []byte(content))
    tool := newReadFileToolForTest(t, workDir)
    got, err := tool.Execute(context.Background(), readFileArguments(t, "large.txt"))
    if err != nil { t.Fatal(err) }
    if len([]byte(got)) > defaultReadFileMaxBytes || !utf8.ValidString(got) {
        t.Fatalf("output bytes = %d, valid = %v", len([]byte(got)), utf8.ValidString(got))
    }
    if !strings.Contains(got, "Use offset=") { t.Fatalf("missing marker: %q", got[len(got)-100:]) }
}
```

Add tests ensuring the marker itself is included in 50 KiB, a first line that cannot coexist with the required marker returns a clear error, invalid UTF-8/NUL in the requested page fails, invalid bytes on a later page do not fail the current page, skipped large lines do not require large retained buffers, and a pre-canceled Context stops before reading.

- [ ] **Step 6: Run safety/byte tests and witness RED**

Run: `go test ./internal/tools -run 'TestReadFileToolLimits|TestReadFileToolRejectsBinary|TestReadFileToolHonorsCancellation' -count=1`

Expected: FAIL until byte accounting and chunk-safe line reads are complete.

- [ ] **Step 7: Implement final-output byte accounting and page validation**

Reserve the actual separator length plus `len(marker)` whenever continuation exists. If adding the first requested line plus the necessary separator and marker exceeds 50 KiB, return an error containing `单行超过 read_file 单页限制`. Validate NUL and UTF-8 only for lines returned on the requested page, check `ctx.Err()` while skipping and after every chunk, and never return a partial line or partial rune.

- [ ] **Step 8: Run all tool tests and commit**

Run: `gofmt -w internal/tools/read_file.go internal/tools/read_file_test.go && go test ./internal/tools -count=1`

Expected: PASS with the old 8000-byte marker removed.

```bash
git add internal/tools/read_file.go internal/tools/read_file_test.go
git commit -m "feat: paginate read file output"
```

---

### Task 5: Engine Snapshot Lifecycle and Progressive Skill Flow

**Files:**
- Modify: `internal/engine/loop.go`
- Modify: `internal/engine/loop_test.go`

**Interfaces:**
- Consumes: `ctxpkg.NewSkillLoader`, `DefaultSkillEnvironment`, `Discover`, and `PromptComposer.Build(snapshot)`
- Changes `AgentEngine` to hold `skillLoader *ctxpkg.SkillLoader`
- Preserves public constructor: `NewAgentEngine(provider.LLMProvider, tools.Registry, string, bool) *AgentEngine`

- [ ] **Step 1: Rewrite workspace context test for catalog-only injection and per-Run refresh**

Update `TestAgentEngineBuildsWorkspaceContextForEachRun` so the fake Registry exposes `read_file`. Assert the first System Prompt contains name/description/location/version but not `engine-body-secret-v1`; rewrite the Skill before the second Run and assert description/version refresh while neither Body appears.

```go
registry := &fakeRegistry{definitions: []schema.ToolDefinition{{Name: "read_file", ParallelSafe: true}}}
agentEngine := engine.NewAgentEngine(provider, registry, workDir, false)
```

Add a test that a workspace with an eligible Skill but no `read_file` definition returns an error before calling the Provider. Add a test that malformed Skills only generate structured diagnostic logs while Run continues.

Replace every hard-coded `"/workspace"` used by a test that reaches `Run` with `t.TempDir()`. Discovery treats a missing workspace root as a system error by design; tests unrelated to workspace behavior must therefore supply a real empty workspace.

- [ ] **Step 2: Run snapshot lifecycle tests and witness RED**

Run: `go test ./internal/engine -run 'TestAgentEngineBuildsWorkspaceContext|TestAgentEngineRequiresReadFile|TestAgentEngineLogsSkillDiagnostics' -count=1`

Expected: FAIL because the Engine still calls the old zero-argument Composer and injects Body.

- [ ] **Step 3: Implement per-Run discovery and Prompt rendering**

At the beginning of `Run`, after base validation and before Provider calls:

```go
availableTools := e.registry.GetAvailableTools()
snapshot, err := e.skillLoader.Discover(ctxpkg.DefaultSkillEnvironment())
if err != nil { return fmt.Errorf("发现 Agent Skills 失败: %w", err) }
if len(snapshot.Skills()) > 0 && !hasToolDefinition(availableTools, "read_file") {
    return errors.New("发现可用 Agent Skills，但 Registry 未挂载 read_file")
}
systemMessage, promptReport := e.composer.Build(snapshot)
```

Initialize `skillLoader: ctxpkg.NewSkillLoader(workDir)` in `NewAgentEngine`. Log each diagnostic with code/path/severity and no Body. When the Prompt report is truncated, log code `skill_prompt_truncated` together with included/omitted/shortened counts. Move `GetAvailableTools` outside the turn loop so the Run snapshot and exposed tool set are consistent for that Run.

- [ ] **Step 4: Run snapshot lifecycle tests and witness GREEN**

Run: `gofmt -w internal/engine/loop.go internal/engine/loop_test.go && go test ./internal/engine -run 'TestAgentEngineBuildsWorkspaceContext|TestAgentEngineRequiresReadFile|TestAgentEngineLogsSkillDiagnostics' -count=1`

Expected: PASS.

- [ ] **Step 5: Replace Thinking isolation expectation with Thinking-to-Action handoff tests**

Rename the old `TestAgentEngineThinkingPhaseHidesToolsWithoutPollutingActionContext` and assert the Action request has four messages: system, original user, validated assistant Thinking, and a user Action transition instruction. Keep the assertion that Thinking receives no tools and Action receives all tools.

```go
actionContext := provider.requests[1]
if len(actionContext) != 4 || actionContext[2].Role != schema.RoleAssistant ||
    actionContext[2].Content != "先读取匹配的技能，再执行。" ||
    !strings.Contains(actionContext[3].Content, "进入 Action") {
    t.Fatalf("action context = %#v", actionContext)
}
```

Add an integration-style fake Provider sequence: Thinking selects `git-workflow`; first Action calls `read_file`; Observation contains a continuation marker; second Thinking sees that Observation; second Action requests the next offset; final response follows after the final page. Use a real `tools.NewRegistry()` with a real `tools.NewReadFileTool(workDir)` so observations prove the actual pagination contract, and close the read tool with `t.Cleanup`. Assert Body appears only in Observation messages, never the System Prompt.

- [ ] **Step 6: Run handoff tests and witness RED**

Run: `go test ./internal/engine -run 'TestAgentEngineCarriesThinkingIntoAction|TestAgentEngineProgressivelyReadsSkill' -count=1`

Expected: FAIL because `thinkResp` is printed but not appended.

- [ ] **Step 7: Append validated Thinking and transition instruction**

Immediately after `validateThinkingResponse` succeeds:

```go
contextHistory = append(contextHistory, *thinkResp, schema.Message{
    Role: schema.RoleUser,
    Content: "请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。",
})
```

Do not append invalid/nil Thinking responses. Keep real tool results as the only path by which Body reaches later requests.

- [ ] **Step 8: Run Engine regression tests and adjust exact message counts**

Run: `gofmt -w internal/engine/loop.go internal/engine/loop_test.go && go test ./internal/engine -count=1`

Expected: PASS. Update only assertions whose context length legitimately changes when Thinking is enabled; scheduler and non-Thinking tests must retain their existing behavior.

- [ ] **Step 9: Commit Engine integration**

```bash
git add internal/engine/loop.go internal/engine/loop_test.go
git commit -m "feat: load skills progressively in agent runs"
```

---

### Task 6: Repository-Wide Verification and Documentation Consistency

**Files:**
- Modify only if verification exposes a directly related defect: files already listed in Tasks 1–5
- Verify: `docs/superpowers/specs/2026-07-31-openclaw-progressive-skills-design.md`
- Verify: `docs/superpowers/plans/2026-07-31-openclaw-progressive-skills.md`

**Interfaces:**
- No new interfaces; this task proves the integrated contract.

- [ ] **Step 1: Prove old full-Body and 8000-byte paths are gone**

Run:

```bash
rg -n 'LoadAll\(\)|defaultSkillName|Unknown Skill|8000|readFileTruncationMarker|执行指南' internal/context internal/tools internal/engine
```

Expected: no production matches for the retired implementation. Test fixture text is acceptable only when asserting absence.

- [ ] **Step 2: Format and inspect formatting output**

Run: `gofmt -w cmd internal && test -z "$(gofmt -l cmd internal)"`

Expected: exit 0 and no listed Go files.

- [ ] **Step 3: Run focused packages without cache**

Run: `go test -count=1 ./internal/context ./internal/tools ./internal/engine`

Expected: PASS.

- [ ] **Step 4: Run static analysis**

Run: `go vet ./...`

Expected: PASS with no findings.

- [ ] **Step 5: Run the complete race suite**

Run: `go test -race -count=1 ./...`

Expected: PASS for every package.

- [ ] **Step 6: Inspect the final diff and workspace ownership boundary**

Run: `git status --short && git diff --check && git diff --stat HEAD~4..HEAD`

Expected: `.claw/` remains untracked and absent from every commit; no whitespace errors; changes are limited to the planned context, tool, engine, test, spec, and plan files.

- [ ] **Step 7: Request code review and resolve only verified findings**

Review against these invariants: no Body in catalog/snapshot, deterministic precedence, Prompt budget never exceeded, paginated reads never lose/duplicate lines, final read output never exceeds 50 KiB, and Thinking content reaches Action. For each accepted finding, add a failing regression test, witness RED, apply the smallest fix, and rerun the focused package plus `go test -race -count=1 ./...`.
