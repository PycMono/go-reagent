# General Agent Workspace Contract Design

## Goal

Define every go-reagent runtime workspace as a general Agent resource space rather than a coding repository, while keeping `AGENTS.md`, Skills, and the confined `read` tool mandatory for every Agent type.

## Required Workspace Contract

Every runtime workspace must provide:

```text
workspace/
├── AGENTS.md
└── skills|.agents/skills|.claw/skills/
    └── <skill>/SKILL.md
```

- `AGENTS.md` must exist, be a non-empty regular UTF-8 file, and must not be a symbolic link.
- At least one valid and environment-eligible Skill must be discovered.
- The Tool Registry must contain the confined `read` tool.
- `write`, `edit`, `apply_patch`, `exec`, and `process` remain optional.

Missing requirements fail context preparation before the first Provider call.

## Prompt Composition

The final model context remains:

```text
generic runtime discipline
+ AGENTS.md
+ available Skill catalog
+ external Context Blocks
+ History
+ current Input
```

The built-in prompt contains only general runtime discipline: follow `AGENTS.md`, inspect and load matching Skills with `read`, call only provided tools, never fabricate tool results, and ground final responses in real context and observations.

Agent identity, language, business responsibilities, and domain-specific restrictions belong to `AGENTS.md`. The built-in prompt must not identify the Agent as a developer, require file modification, or force Chinese output.

`AGENTS.md` is injected as authoritative instructions, not quoted inside a Markdown code fence.

## Skill Behavior

The existing SkillLoader remains mandatory and continues to scan only the configured Workspace roots:

- `skills/`
- `.agents/skills/`
- `.claw/skills/`

Invalid and ineligible Skills keep producing diagnostics. If no eligible Skill remains after filtering, context preparation fails.

The current progressive-loading model remains unchanged: the system prompt exposes Skill summaries and locations; the model uses the confined `read` tool to load the selected `SKILL.md` completely.

## Error Contract

Context preparation returns explicit errors for:

- missing, empty, invalid, or unsafe `AGENTS.md`;
- zero eligible Skills;
- missing `read` definition.

No Provider call or Tool execution occurs after one of these errors.

## Compatibility

The current command application keeps working by adding a repository-root `AGENTS.md` and one default workspace Skill. Existing tests that construct temporary workspaces must create the same mandatory fixture.

This change does not alter RunRequest/RunResult, session ownership, persistence, business tools, structured output, or package visibility.

## Verification

Tests cover:

- generic prompt content and absence of coding identity;
- mandatory AGENTS.md validation;
- mandatory eligible Skill validation;
- mandatory read validation;
- a service-Agent workspace containing service instructions and a business Skill;
- preservation of Context/History/Input order;
- existing context, engine, app, tool, and integration behavior.
