# Pi Skills Package Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move reusable Skill parsing, discovery, snapshots, diagnostics, and prompt rendering from package `pi` into an independent `pi/skills` package without changing runtime behavior.

**Architecture:** Package `pi/skills` exposes non-stuttering Go APIs and depends only on the standard library plus `yaml.v3`. Package `pi` consumes those APIs for Workspace prompt and Run context orchestration, then translates Skill discovery failures into the existing Pi workspace error classification.

**Tech Stack:** Go 1.26, `os.Root`, `gopkg.in/yaml.v3`, Fx, Go error wrapping, `go test`, and `go list` package checks.

## Global Constraints

- Work directly on `master` as requested.
- Preserve the uncommitted `application/transport` service migration and the untracked `.superpowers/` directory.
- Do not create `internal`, `sdk`, `workspace`, or `pi/context` directories.
- Do not add compaction behavior.
- Preserve Skill sources, priority, validation, eligibility, diagnostics, prompt text, and prompt budgets.
- Preserve `pi.New`/`Run`/`Close` and current Pi error codes.
- `pi/skills` must not import package `pi`, another Pi subpackage, or a service package.
- Stage and commit only paths belonging to this migration.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `pi/skills/errors.go` | Skill workspace error sentinel |
| `pi/skills/loader.go` | Loader state and constructor |
| `pi/skills/parser.go` | strict `SKILL.md` parsing and validation |
| `pi/skills/discovery.go` | safe scanning, eligibility, priority, diagnostics |
| `pi/skills/snapshot.go` | immutable summaries and diagnostics |
| `pi/skills/prompt.go` | bounded XML Skill catalog rendering |
| `pi/skills/xml_text.go` | XML 1.0 helpers |
| `pi/resource_loader.go` | Fx adapters for Workspace resources |
| `pi/system_prompt.go` | AGENTS plus Skill prompt composition |
| `pi/run_context.go` | per-Run discovery and context construction |
| `tests/integration/package_boundaries_test.go` | standalone dependency enforcement |

### Task 1: Extract the Standalone Skills Package

**Files:**
- Create: `pi/skills/errors.go`
- Create: `pi/skills/loader.go`
- Move/rewrite: `pi/skills.go` -> `pi/skills/parser.go`
- Move/rewrite: `pi/skill_discovery.go` -> `pi/skills/discovery.go`
- Move/rewrite: `pi/skill_snapshot.go` -> `pi/skills/snapshot.go`
- Move/rewrite: `pi/skill_prompt.go` -> `pi/skills/prompt.go`
- Move/rewrite: `pi/xml_text.go` -> `pi/skills/xml_text.go`
- Move/rewrite: `pi/skills_test.go` -> `pi/skills/parser_test.go`
- Move/rewrite: `pi/skill_discovery_test.go` -> `pi/skills/discovery_test.go`
- Move/rewrite: `pi/skill_snapshot_test.go` -> `pi/skills/snapshot_test.go`
- Move/rewrite: `pi/skill_prompt_test.go` -> `pi/skills/prompt_test.go`

**Interfaces:**
- Produces: `skills.NewLoader(string) *skills.Loader`
- Produces: `(*skills.Loader).Discover(skills.Environment) (*skills.Snapshot, error)`
- Produces: `skills.DefaultEnvironment() skills.Environment`
- Produces: `skills.RenderPrompt(*skills.Snapshot) (string, skills.PromptReport)`
- Produces: `skills.ErrInvalid`, `skills.Source`, `skills.Severity`, `skills.Summary`, and `skills.Diagnostic`

- [ ] **Step 1: Record the focused baseline**

Run:

```bash
go test ./pi -run 'Skill|Prompt|RunContext|Resource' -count=1
```

Expected: PASS before relocation.

- [ ] **Step 2: Move tests into package `skills` and rename API references**

Apply these exact replacements in moved tests and implementation:

```text
NewSkillLoader            -> NewLoader
SkillLoader               -> Loader
SkillEnvironment          -> Environment
DefaultSkillEnvironment   -> DefaultEnvironment
SkillSnapshot             -> Snapshot
SkillSummary              -> Summary
SkillDiagnostic           -> Diagnostic
SkillSource               -> Source
SkillSourceWorkspace      -> SourceWorkspace
SkillSourceAgents         -> SourceAgents
SkillSourceClaw           -> SourceClaw
DiagnosticSeverity        -> Severity
DiagnosticSeverityInfo    -> SeverityInfo
DiagnosticSeverityWarning -> SeverityWarning
DiagnosticSeverityError   -> SeverityError
SkillPromptReport         -> PromptReport
renderSkillPrompt         -> RenderPrompt
```

- [ ] **Step 3: Confirm the relocated package initially fails**

Run: `go test ./pi/skills -count=1`

Expected: FAIL until the renamed implementation and error sentinel exist.

- [ ] **Step 4: Implement the standalone API**

```go
package skills

var ErrInvalid = errors.New("skills: invalid workspace")

type Loader struct {
	workDir string
}

func NewLoader(workDir string) *Loader {
	return &Loader{workDir: workDir}
}

type Environment struct {
	GOOS      string
	EnvLookup func(name string) bool
	BinLookup func(name string) bool
}

func DefaultEnvironment() Environment
func (l *Loader) Discover(Environment) (*Snapshot, error)
func RenderPrompt(*Snapshot) (string, PromptReport)
```

Keep construction private and accessors defensive:

```go
type Snapshot struct {
	skills      []Summary
	diagnostics []Diagnostic
}

func newSnapshot(items []Summary, diagnostics []Diagnostic) *Snapshot
func (s *Snapshot) Skills() []Summary
func (s *Snapshot) Diagnostics() []Diagnostic
```

Discovery wraps `skills.ErrInvalid`; invalid individual files remain diagnostics.

- [ ] **Step 5: Verify the package**

Run: `go test ./pi/skills -count=1`

Expected: PASS for parsing, discovery, eligibility, snapshots, and prompt rendering.

### Task 2: Rewire Pi Workspace Orchestration

**Files:**
- Modify: `pi/resource_loader.go`
- Modify: `pi/system_prompt.go`
- Modify: `pi/run_context.go`
- Modify: `pi/bootstrap_resource_test.go`
- Modify: `pi/resource_loader_test.go`
- Modify: `pi/system_prompt_test.go`
- Modify: `pi/run_context_test.go`
- Modify: `pi/agent/loop_test.go`
- Modify: `tests/integration/engine_skill_tool_test.go`

**Interfaces:**
- Consumes: `skills.Loader`, `skills.Snapshot`, `skills.Diagnostic`, `skills.PromptReport`
- Preserves: `pi.NewPromptComposer(string) *pi.PromptComposer`
- Preserves: `pi.NewRunContextFactory(*PromptComposer, *skills.Loader) *RunContextFactory`
- Preserves: `agent.ContextFactory` and public Pi facade behavior

- [ ] **Step 1: Expose stale root references**

Run:

```bash
go test ./pi ./pi/agent ./tests/integration -run 'Skill|Prompt|RunContext|Resource|EngineSkillTool' -count=1
```

Expected: FAIL with undefined root Skill APIs after Task 1.

- [ ] **Step 2: Rewire Fx and prompt composition**

```go
func newSkillLoader(workDir WorkDir) *skills.Loader {
	return skills.NewLoader(string(workDir))
}

func (c *PromptComposer) Build(snapshot *skills.Snapshot) (ai.Message, skills.PromptReport, error) {
	skillPrompt, report := skills.RenderPrompt(snapshot)
	// Retain existing AGENTS.md loading, prompt order, and message construction.
}
```

- [ ] **Step 3: Rewire Run context and retain both error identities**

```go
snapshot, err := f.skillLoader.Discover(skills.DefaultEnvironment())
if err != nil {
	return agent.RunContext{}, fmt.Errorf("%w: 发现 Agent Skills 失败: %w", ErrInvalid, err)
}
```

Use `[]skills.Diagnostic`, `skills.SeverityError`, and `skills.SeverityWarning` in diagnostic logging.

- [ ] **Step 4: Update Pi and cross-package tests**

```go
import "github.com/PycMono/go-reagent/pi/skills"

factory := pi.NewRunContextFactory(
	pi.NewPromptComposer(workDir),
	skills.NewLoader(workDir),
)
```

Tests needing a Snapshot outside package `skills` create a temporary valid `SKILL.md` and call `Loader.Discover`; no mutable Snapshot constructor is exported.

- [ ] **Step 5: Verify Pi integration**

Run:

```bash
go test ./pi ./pi/agent ./tests/integration -run 'Skill|Prompt|RunContext|Resource|EngineSkillTool' -count=1
```

Expected: PASS with package `pi` consuming `pi/skills`.

### Task 3: Enforce Boundaries and Complete Verification

**Files:**
- Modify: `tests/integration/package_boundaries_test.go`
- Verify: all repository packages and commands

**Interfaces:**
- Consumes: package graph produced by Tasks 1 and 2
- Produces: regression protection for the standalone `pi/skills` boundary

- [ ] **Step 1: Add a focused dependency-boundary case**

```go
{
	pkg: modulePath + "/pi/skills",
	forbidden: func(dependency string) bool {
		return dependency == modulePath ||
			strings.HasPrefix(dependency, modulePath+"/pi/") ||
			strings.HasPrefix(dependency, modulePath+"/application") ||
			strings.HasPrefix(dependency, modulePath+"/config") ||
			strings.HasPrefix(dependency, modulePath+"/conversation") ||
			strings.HasPrefix(dependency, modulePath+"/persistence") ||
			strings.HasPrefix(dependency, modulePath+"/transport")
	},
},
```

The dependency helper excludes the package itself, so `/pi/` catches only sibling Pi packages.

- [ ] **Step 2: Run boundary tests**

Run:

```bash
go test ./tests/integration -run 'Package|PiDoesNotImportServicePackages|LegacyInternal' -count=1
```

Expected: PASS.

- [ ] **Step 3: Format and run focused tests**

Run:

```bash
gofmt -w pi/skills pi/resource_loader.go pi/system_prompt.go pi/run_context.go
go test ./pi/... ./tests/integration -count=1
```

Expected: formatting completes and all Pi plus integration tests pass.

- [ ] **Step 4: Run repository verification**

Run each command:

```bash
go test ./...
go build ./cmd/reagent ./cmd/ping
git diff --check
rg --files | rg '(^|/)internal/'
rg -n 'github.com/PycMono/go-reagent/(ai|agent|internal)(/|")' --glob '*.go'
```

Expected: tests and builds pass; diff check is empty; both `rg` commands return no matches.

- [ ] **Step 5: Commit only the Skills migration**

Stage `pi/skills`, the deleted root Skill files, the modified Pi orchestration files, and the two modified integration tests. Commit as:

```bash
git commit -m "refactor: extract pi skills package"
```

`git status --short` must still show the pre-existing service migration and `.superpowers/`, but no uncommitted Skills migration files.
