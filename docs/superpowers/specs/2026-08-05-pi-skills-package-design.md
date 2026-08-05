# Pi Skills Package Design

## Goal

Extract the reusable Agent Skill implementation from the `pi` root package
into `pi/skills`. The new package owns Skill parsing, discovery, environment
eligibility, conflict resolution, immutable snapshots, diagnostics, and the
bounded prompt catalog.

This is a package-boundary refactor. It does not change Skill discovery rules,
prompt text, resource requirements, or runtime behavior.

## Decision

Use a dedicated `pi/skills` package.

The official Pi TypeScript implementation uses focused files such as
`skills.ts` rather than a `skills` directory. The Go implementation already
has separate parser, discovery, snapshot, prompt, XML, and test files, so an
independent Go package provides a meaningful ownership boundary and avoids an
overloaded `pi` root package.

The rejected alternatives are:

1. Keep every Skill file in package `pi`. This is closest to Pi's TypeScript
   file layout, but leaves a multi-responsibility root package and weakens the
   future extraction boundary.
2. Create `pi/context` for Skills and compaction. This groups features by the
   broad concept of context, but conflicts with Go's standard `context`
   package in imports and combines two domains with different lifecycles.

Compaction is not part of `pi/skills`. If real context compaction is added, it
will use a separate `pi/compaction` package.

## Directory Model

```text
pi/
├── skills/
│   ├── loader.go
│   ├── parser.go
│   ├── discovery.go
│   ├── snapshot.go
│   ├── prompt.go
│   ├── xml_text.go
│   ├── errors.go
│   └── *_test.go
├── resource_loader.go
├── run_context.go
├── system_prompt.go
├── bootstrap.go
└── ...

skills/
└── repository-development/
    └── SKILL.md
```

The two `skills` directories have different ownership:

- `pi/skills` contains reusable Go implementation.
- repository-root `skills` contains runtime Workspace resources bundled with
  the go-reagent service.

## Package Responsibilities

### `pi/skills`

The package owns:

- strict parsing and validation of `SKILL.md` Frontmatter and body;
- safe, workspace-confined discovery from the supported source directories;
- operating-system, executable, environment-variable, and model-invocation
  eligibility checks;
- source priority, duplicate-name rejection, and shadow diagnostics;
- immutable snapshots of eligible Skill summaries and diagnostics;
- bounded, XML-safe Skill catalog rendering for a System Prompt;
- Skill-specific error classification.

The package does not own:

- loading or composing `AGENTS.md`;
- creating `agent.RunContext` values;
- registering Tools or checking whether `read` is available;
- logging diagnostics;
- converting failures into public Pi error codes;
- conversation history or context compaction.

### Pi root package

Package `pi` continues to own Workspace-level orchestration:

- `resource_loader.go` binds the selected working root to the Pi Fx graph;
- `system_prompt.go` combines core instructions, `AGENTS.md`, and the rendered
  Skill catalog;
- `run_context.go` discovers Skills for each Run, logs diagnostics, validates
  required Tools, and constructs `agent.RunContext`;
- `bootstrap.go` wires `skills.Loader` into the default runtime;
- the public error layer converts Skill package errors into stable Pi error
  codes.

## Public API

Names inside package `skills` avoid redundant `Skill` prefixes:

```go
package skills

var ErrInvalid = errors.New("skills: invalid workspace")

type Loader struct { /* private state */ }
func NewLoader(workDir string) *Loader
func (l *Loader) Discover(Environment) (*Snapshot, error)

type Environment struct {
	GOOS      string
	EnvLookup func(name string) bool
	BinLookup func(name string) bool
}
func DefaultEnvironment() Environment

type Source string
type Severity string
type Summary struct { /* current SkillSummary fields */ }
type Diagnostic struct { /* current SkillDiagnostic fields */ }
type Snapshot struct { /* immutable slices */ }

func (s *Snapshot) Skills() []Summary
func (s *Snapshot) Diagnostics() []Diagnostic

type PromptReport struct { /* current SkillPromptReport fields */ }
func RenderPrompt(*Snapshot) (string, PromptReport)
```

Parsing helpers, discovered candidates, source specifications, eligibility
checks, merge helpers, and XML helpers remain package-private.

The Pi root package does not retain aliases such as `pi.SkillLoader` or
`pi.SkillSnapshot`. These are subsystem APIs and are exposed at the explicit
`pi/skills` import path. The ready-to-use `pi.New`/`Run`/`Close` facade remains
unchanged.

## Dependency Rules

```text
pi ───────────────→ pi/skills
pi/skills ────────→ Go standard library
pi/skills ────────→ gopkg.in/yaml.v3
```

Required invariants:

1. `pi/skills` never imports its parent package `pi`.
2. `pi/skills` never imports `pi/agent`, `pi/ai`, `pi/tools`, or a service
   package.
3. Package `pi` may import `pi/skills` and translate its errors at the runtime
   boundary.
4. Repository-root Workspace resources are accessed only at runtime and are
   never embedded or imported by `pi/skills`.

## Error Behavior

`skills.ErrInvalid` classifies failures that prevent discovery from operating
on the selected root, such as resolving or opening the workspace. Invalid
individual `SKILL.md` files continue to produce `Diagnostic` values so one bad
Skill does not stop discovery of the others.

When discovery fails during a Pi Run, `run_context.go` wraps both
`pi.ErrInvalid` and the original `skills.ErrInvalid` cause. The facade
therefore preserves the existing public Pi invalid-workspace error code while
direct `pi/skills` callers can still use `errors.Is(err, skills.ErrInvalid)`.

Prompt truncation remains a successful result described by `PromptReport`,
not an error.

## Runtime Flow

For each Pi Run:

1. `pi.RunContextFactory` calls `skills.Loader.Discover` with the current
   environment.
2. `pi` logs returned diagnostics and rejects an empty eligible Skill set
   according to its existing Workspace contract.
3. `pi.PromptComposer` loads `AGENTS.md` and calls `skills.RenderPrompt`.
4. `pi` combines the core prompt, Agent definition, and Skill catalog.
5. `pi` constructs the `agent.RunContext` with history, request context, input,
   Tools, and metadata.

No Skill contents are cached across Runs. Workspace changes continue to take
effect on the next Run.

## File Migration

| Current path | Target path |
| --- | --- |
| `pi/skills.go` | `pi/skills/loader.go` and `pi/skills/parser.go` |
| `pi/skill_discovery.go` | `pi/skills/discovery.go` |
| `pi/skill_snapshot.go` | `pi/skills/snapshot.go` |
| `pi/skill_prompt.go` | `pi/skills/prompt.go` |
| `pi/xml_text.go` | `pi/skills/xml_text.go` |
| Skill-specific part of `pi/resource_errors.go` | `pi/skills/errors.go` |
| corresponding `pi/*_test.go` files | corresponding `pi/skills/*_test.go` files |

Tests for Workspace orchestration remain in package `pi` next to
`resource_loader.go`, `run_context.go`, and `system_prompt.go`.

## Testing

The refactor must preserve existing parser, discovery, eligibility, source
priority, snapshot immutability, XML safety, prompt budget, and runtime-context
tests.

Additional boundary coverage must verify:

- `go test ./pi/skills` passes independently;
- `go list -deps ./pi/skills` contains no parent Pi or service package;
- package `pi` consumes the public `pi/skills` API;
- direct Skill errors remain discoverable with `errors.Is`;
- Pi still maps discovery failures to its existing public invalid error code.

Repository verification remains:

```bash
go test ./...
go build ./cmd/reagent ./cmd/ping
git diff --check
```

## Non-Goals

- adding context compaction or summarization;
- adding a `pi/context` package;
- changing supported Skill source directories or their priority;
- changing `SKILL.md` validation and eligibility rules;
- loading Skill bodies into the System Prompt;
- adding caches, watchers, or persistence;
- extracting `pi` into a separate module or repository.
