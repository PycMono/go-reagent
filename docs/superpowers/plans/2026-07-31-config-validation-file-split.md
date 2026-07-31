# Config Validation File Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the existing private configuration normalization and validation pipeline into `internal/config/validate.go` without changing behavior.

**Architecture:** Keep configuration data types, loading, and active-platform selection in `config.go`. Keep the complete normalization and validation pipeline together in `validate.go`, preserving every existing signature, call order, normalization result, and error message.

**Tech Stack:** Go 1.26, `github.com/jinzhu/configor`, Go testing

## Global Constraints

- Do not change exported APIs.
- Do not change validation order, accepted values, normalization results, or error messages.
- Do not expose API keys or webhook credentials in errors.
- Preserve existing user changes in the dirty worktree.
- Do not commit implementation files because `README.md`, `config.go`, and `validate.go` already contain overlapping user changes.

---

### Task 1: Relocate configuration normalization and validation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `README.md`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `Config`, `BotConfig`, `PlatformConfig`, `ProtocolOpenAI`, `ProtocolAnthropic`, and `Config.Current` from `config.go`.
- Produces: unchanged private methods `(*Config).normalizeAndValidate() error`, `(*BotConfig).normalizeAndValidate() error`, `(*PlatformConfig).normalize()`, `(*PlatformConfig).validate(int) error`, and `(*PlatformConfig).normalizeBaseURL() error` in `validate.go`.

- [ ] **Step 1: Verify the existing behavior contract**

Run:

```bash
go test -count=1 ./internal/config
```

Expected: PASS before relocation.

- [ ] **Step 2: Move the private validation pipeline**

Remove the five private methods from `config.go` and place them in `validate.go` unchanged. `validate.go` must import exactly the standard-library dependencies used by those methods:

```go
import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)
```

After the move, remove `net/url` from `config.go`; retain `errors`, `fmt`, and `strings` because `Current` and `currentPlatformNotFoundError` still use them.

- [ ] **Step 3: Update the README package tree**

Replace the single `config.go` description with these two entries:

```text
│   │   ├── config.go        # 配置结构、加载与当前平台选择
│   │   ├── validate.go      # 配置规范化与数据校验
```

- [ ] **Step 4: Format and verify the focused package**

Run:

```bash
gofmt -w internal/config/config.go internal/config/validate.go
go test -count=1 ./internal/config
```

Expected: PASS with unchanged test behavior.

- [ ] **Step 5: Verify the full repository and diff quality**

Run:

```bash
git diff --check
go test -count=1 ./...
```

Expected: `git diff --check` succeeds and every Go package passes. If sandboxed networking blocks `httptest` listeners, rerun the full test command outside the sandbox.

- [ ] **Step 6: Leave implementation changes uncommitted for review**

Report the modified files and verification output. Do not stage or commit the implementation because the same paths already contain user changes.
