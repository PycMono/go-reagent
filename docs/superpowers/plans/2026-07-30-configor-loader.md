# Configor Loader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-written strict JSON configuration loader with Configor v1.2.2 and expose Configor's native JSON, YAML, TOML, environment overlay, example fallback, and Shell environment behavior.

**Architecture:** `internal/config.Load` delegates file discovery and decoding to `configor.Load`, then runs the existing normalization and business validation unchanged. Struct tags describe equivalent names to all three decoders, while tests exercise Configor only through the project's public `Load` boundary.

**Tech Stack:** Go 1.26, `github.com/jinzhu/configor v1.2.2`, Go `testing`

## Global Constraints

- Use Configor v1.2.2 through its native package-level `Load` function without compatibility options.
- Preserve the public `Config`, `Options`, `Load`, and `Current` APIs.
- Preserve all existing normalization, URL/protocol/model/platform, duplicate-ID, and API-key validation.
- Accept Configor defaults: unknown fields and trailing JSON are not rejected.
- Do not modify Engine, Provider, Schema, Registry, Tools, or CLI behavior.
- The workspace has no Git metadata, so commit steps are intentionally omitted.

---

### Task 1: Capture Configor Loading Contracts

**Files:**
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `func Load(path string) (*Config, error)`
- Produces: regression coverage for JSON/YAML/TOML, environment overlays, example fallback, and Shell environment overrides

- [ ] **Step 1: Replace obsolete strict-decoder cases and add format tests**

Remove the `unknown field` and `trailing JSON` entries from `TestLoadRejectsInvalidConfiguration`. Add table-driven tests that write equivalent valid `.yaml` and `.toml` documents, call `Load`, and assert the normalized current platform fields.

- [ ] **Step 2: Add Configor native behavior tests**

Add isolated tests using `t.TempDir` and `t.Setenv` for:

```text
CONFIGOR_CURRENTPLATFORM selects another configured platform
CONFIGOR_ENV=test causes config.test.json to override config.json
missing config.json falls back to config.example.json
```

Also add a test proving unknown JSON fields and a second trailing JSON value no longer block a valid configuration.

- [ ] **Step 3: Run the focused tests and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test ./internal/config -count=1
```

Expected: YAML/TOML or Configor-native behavior tests fail because the current implementation only strictly decodes one JSON file.

---

### Task 2: Replace the Loader and Add the Dependency

**Files:**
- Modify: `internal/config/config.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: Configor `func Load(config interface{}, files ...string) error`
- Produces: unchanged project API `func Load(path string) (*Config, error)` backed by Configor

- [ ] **Step 1: Add Configor v1.2.2**

Run:

```bash
go get github.com/jinzhu/configor@v1.2.2
```

- [ ] **Step 2: Implement the minimum loader replacement**

Add matching `json`, `yaml`, and `toml` tags to `Config` and `Options`. Replace direct file opening and `json.Decoder` usage with:

```go
var cfg Config
if err := configor.Load(&cfg, path); err != nil {
	return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
}
```

Delete `requireJSONEnd` and imports used only by the old decoder. Leave `normalizeAndValidate` and all downstream methods unchanged.

- [ ] **Step 3: Format and verify GREEN**

Run:

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
GOCACHE=/tmp/go-reagent-build-cache go test ./internal/config -count=1
```

Expected: all `internal/config` tests pass.

---

### Task 3: Document and Verify the Migration

**Files:**
- Modify: `README.md`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: completed Configor-backed `config.Load`
- Produces: accurate setup documentation and a tidy, verified Go module

- [ ] **Step 1: Update configuration documentation**

Change the architecture/capability text from strict JSON loading to Configor-backed JSON/YAML/TOML loading. Document:

```text
CONFIG_PATH selects the base file
CONFIGOR_ENV selects an environment overlay such as config.production.yaml
CONFIGOR_<FIELD> overrides a field, for example CONFIGOR_CURRENTPLATFORM
config.example.<ext> is used when the requested base and environment files are absent
```

Retain the API-key warning and JSON quick-start example.

- [ ] **Step 2: Tidy dependencies**

Run:

```bash
go mod tidy
```

- [ ] **Step 3: Run complete verification**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go vet ./...
gofmt -l cmd internal
GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./...
```

Expected: `go vet` and the race-enabled test suite exit zero; `gofmt -l` prints no files.
