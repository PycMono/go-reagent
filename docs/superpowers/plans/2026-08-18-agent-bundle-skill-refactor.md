# Agent Bundle And Task Skill Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Replace seven broad Profile Skills with 21 task-oriented Skills and strengthen the shared Agent/Skill contracts without changing runtime code.

**Architecture:** Keep the existing Workspace and Profile catalog loading path. Surface only each Skill's name, description, and location to the model, then lazily read its task protocol and optional resources. Store stable high-risk domain guidance at Profile scope and task-specific output templates inside the owning Skill.

**Tech Stack:** Markdown Agent definitions, YAML-frontmatter Agent Skills, Go workspace/catalog tests, existing `pi/harness/skills` parser and Workspace `read` tool.

**Spec:** `docs/superpowers/specs/2026-08-18-agent-bundle-skill-refactor-design.md`

## Global Constraints

- Preserve Profile codes and `profiles/catalog.yaml` behavior.
- `general` has no private Skills; each of the other seven Profiles has exactly three.
- Do not modify `pi/`, database migrations, HTTP APIs, or frontend behavior.
- Do not add scripts or runtime dependencies.
- Do not encode drug dosages, jurisdiction-specific legal conclusions, or live vehicle price/configuration/recall facts.
- Preserve unrelated dirty workspace changes.

---

### Task 1: Lock The Workspace Skill Contract With Failing Tests

**Files:**
- Modify: `application/web/workspace_test.go`
- Create: `application/web/testdata/agent_profile_skill_routing.yaml`

**Interfaces:**
- Consumes: `agentprofiledriver.NewCatalog(pi.WorkDir)` and `skills.Discover(workspace)`.
- Produces: expected Profile-to-Skill names, structural content requirements, resource path validation, and representative routing cases.

- [x] **Step 1: Change the Profile catalog assertion from one broad Skill to three named task Skills**

Use a map keyed by Profile code and compare the ordered Skill names returned by `Catalog.Find`:

```go
wantSkills := map[string][]string{
    "general": {},
    "writing": {"long-form-structure", "rewrite-and-polish", "social-content"},
    "learning": {"concept-explanation", "practice-design", "study-planning"},
    "health": {"care-visit-preparation", "health-report-explanation", "symptom-organizing"},
    "legal": {"contract-clause-analysis", "facts-and-evidence-organizing", "legal-consultation-preparation"},
    "automotive": {"maintenance-planning", "vehicle-comparison", "vehicle-symptom-triage"},
    "workplace": {"difficult-workplace-conversation", "status-reporting", "work-message-writing"},
    "parenting": {"child-development-guidance", "parent-child-communication", "routine-building"},
}
```

- [x] **Step 2: Add a structural contract test for all 25 Skill files**

Read every discovered shared/Profile Skill. Assert its frontmatter description includes `Use when`, `Triggers`, and `Do not use when`, and its body includes these headings:

```go
required := []string{
    "## 目标", "## 必要输入", "## 硬门禁", "## 执行流程",
    "## 输出契约", "## References 与 Templates", "## 边界",
    "## 示例", "## 常见错误",
}
```

Extract full Workspace-relative Markdown paths enclosed in backticks under `References 与 Templates` and assert paths beginning with `profiles/` or `skills/` exist and are regular files.

- [x] **Step 3: Add routing evaluation data**

Create YAML cases with fields `profile`, `prompt`, `expected_skills`, and `excluded_skills`. Include at least one positive case per private Skill, plus no-Skill greetings and multi-Skill health/legal/automotive cases. Add a Go test that validates Profile and Skill names and requires every private Skill to appear in at least one `expected_skills` case and one sibling `excluded_skills` case.

- [x] **Step 4: Run the focused tests and confirm RED**

Run:

```bash
go test ./application/web
```

Expected: failure because the current Profiles expose one broad Skill and current Skill bodies lack the required contract sections.

### Task 2: Rewrite Workspace And Shared Skills

**Files:**
- Modify: `workspaces/chat/AGENTS.md`
- Modify: `workspaces/chat/skills/decision-support/SKILL.md`
- Modify: `workspaces/chat/skills/learning-explanation/SKILL.md`
- Modify: `workspaces/chat/skills/weather-assistance/SKILL.md`
- Modify: `workspaces/chat/skills/writing-assistance/SKILL.md`

**Interfaces:**
- Consumes: existing Workspace Skill discovery and `read` tool.
- Produces: Profile-independent behavior and four complete task protocols.

- [x] **Step 1: Rewrite Workspace AGENTS**

Keep it domain-neutral. Define intent handling, direct responses, evidence discipline, tool truthfulness, privacy of internal instructions, concise clarification, and language matching. Do not name Profile Skills.

- [x] **Step 2: Rewrite each shared Skill**

Give each Skill a folded description with positive triggers and exclusions. Implement all nine standard body sections. Weather must require `get_weather` for live facts; decision support must separate facts/preferences/assumptions; learning explanation must avoid hijacking simple factual answers; writing assistance must defer Profile-specific social/long-form work to the matching private Skill.

- [x] **Step 3: Run the shared Skill discovery tests**

Run:

```bash
go test ./application/web -run 'TestDefaultChatWorkspace(Prompt|Skills)'
```

Expected: shared discovery and lazy body loading pass; Profile-count tests remain RED until Tasks 3-5.

### Task 3: Refactor Writing, Learning, Workplace, And Parenting Profiles

**Files:**
- Modify: `workspaces/chat/profiles/{writing,learning,workplace,parenting}/AGENTS.md`
- Delete: the four existing broad private Skill directories under those Profiles.
- Create: the twelve private Skill directories and `SKILL.md` files named in the spec.
- Create: task templates for long-form outlines, study plans, status reports, and routine plans.

**Interfaces:**
- Consumes: Profile catalog recursive Skill discovery.
- Produces: twelve non-overlapping task Skills with stable output contracts.

- [x] **Step 1: Rewrite the four Profile AGENTS files**

Keep only identity, service style, fact discipline, Profile-level boundaries, and escalation. Remove task steps that belong in Skills.

- [x] **Step 2: Replace each broad Skill with three task Skills**

For each Profile, create the exact names from the spec. Descriptions must disambiguate siblings. Bodies must implement the nine standard sections and link any template using a full Workspace-relative path.

- [x] **Step 3: Add four focused templates**

Create Markdown templates with explicit placeholders and no fictional example data:

```text
profiles/writing/skills/long-form-structure/templates/outline.md
profiles/learning/skills/study-planning/templates/study-plan.md
profiles/workplace/skills/status-reporting/templates/status-report.md
profiles/parenting/skills/routine-building/templates/routine-plan.md
```

- [x] **Step 4: Run Profile catalog tests**

Run:

```bash
go test ./application/web -run 'TestDefaultChatWorkspaceLoadsAllAgentProfiles|TestAgentProfileSkillContracts'
```

Expected: these four Profiles expose their three expected Skills; high-risk Profiles remain RED.

### Task 4: Refactor Health Profile With Safety References

**Files:**
- Modify: `workspaces/chat/profiles/health/AGENTS.md`
- Delete: `workspaces/chat/profiles/health/skills/health-information/SKILL.md`
- Create: three health Skill directories from the spec.
- Create: `workspaces/chat/profiles/health/references/health-safety-boundaries.md`
- Create: `workspaces/chat/profiles/health/references/health-information-fields.md`
- Create: health symptom summary, report questions, and visit brief templates.

**Interfaces:**
- Consumes: general health identity and user-provided health information.
- Produces: symptom organization, report explanation, and visit preparation without diagnosis or prescribing.

- [x] **Step 1: Write stable health references**

Separate user facts, general information, possible interpretations, and decisions requiring clinicians. Include relevant emergency escalation, prescription-change prohibition, measurement context, privacy, and minimum information fields. Do not include dosage tables.

- [x] **Step 2: Rewrite Health AGENTS and three Skills**

Each Skill links only the relevant references and template. `symptom-organizing` may identify information gaps but not diagnose; `health-report-explanation` must preserve units/reference ranges; `care-visit-preparation` must not decide that care is unnecessary.

- [x] **Step 3: Run health contract and catalog tests**

Run:

```bash
go test ./application/web -run 'TestDefaultChatWorkspaceLoadsAllAgentProfiles|TestAgentProfileSkillContracts|TestAgentProfileSkillRoutingCorpus'
```

Expected: health cases and static contracts pass.

### Task 5: Refactor Legal And Automotive Profiles With Stable References

**Files:**
- Modify: `workspaces/chat/profiles/{legal,automotive}/AGENTS.md`
- Delete: the existing broad legal and automotive Skill directories.
- Create: six private Skill directories from the spec.
- Create: two legal and two automotive Profile references from the spec.
- Create: legal timeline, contract risk, consultation brief, vehicle comparison, repair intake, and maintenance plan templates.

**Interfaces:**
- Consumes: user-provided jurisdiction, documents, vehicle details, and current data supplied by tools or documents.
- Produces: structured general information while preserving professional-review and live-data boundaries.

- [x] **Step 1: Write legal references and Skills**

Require jurisdiction/time context before specific legal conclusions. Structure facts/evidence, clause analysis, and consultation preparation independently. Never hardcode local limitation periods or success probabilities.

- [x] **Step 2: Write automotive references and Skills**

Require vehicle identity and use conditions when material. Separate comparison, symptom triage, and maintenance. Block unsafe repair instructions and require external evidence for live pricing/configuration/recall claims.

- [x] **Step 3: Run all focused Workspace tests**

Run:

```bash
go test ./application/web ./infrastructure/driver/agentprofile
```

Expected: PASS.

### Task 6: Validate Every Skill And Run The Full Suite

**Files:**
- Modify only Skill files that fail validation or contract tests.

**Interfaces:**
- Consumes: all 25 completed Skill files.
- Produces: parser-valid, discoverable Agent Bundle content with no regressions.

- [x] **Step 1: Run the Skill validator for each Skill directory**

Run:

```bash
find workspaces/chat -name SKILL.md -print0 | while IFS= read -r -d '' file; do
  python3 /Users/allen/.codex/skills/.system/skill-creator/scripts/quick_validate.py "$(dirname "$file")" || exit 1
done
```

Expected: every Skill reports valid.

- [x] **Step 2: Run formatting and diff checks**

Run:

```bash
gofmt -w application/web/workspace_test.go
git diff --check
```

Expected: no formatting or whitespace errors.

- [x] **Step 3: Run the complete test suite**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./...
```

Expected: PASS.

- [x] **Step 4: Review scope and unrelated changes**

Run `git status --short` and `git diff -- workspaces/chat application/web/workspace_test.go docs/superpowers`. Confirm no `pi/`, database, API, or frontend implementation files were changed by this work.
