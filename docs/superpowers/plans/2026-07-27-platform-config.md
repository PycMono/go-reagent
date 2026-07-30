# Platform Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace scattered Provider environment variables with a strict `config.json` containing self-contained platform profiles selected by `currentPlatform`.

**Architecture:** A new `internal/config` package owns strict JSON loading, normalization, validation, and current-profile selection. A protocol-only `provider.New(Options)` factory constructs OpenAI- or Anthropic-compatible adapters without vendor-specific environment lookups. `cmd/reagent` loads one configuration file, builds the selected Provider, and injects it into the existing engine.

**Tech Stack:** Go 1.26, standard library `encoding/json` and `net/url`, OpenAI Go v3, Anthropic Go SDK, Go `testing` and `httptest`.

## Global Constraints

- The JSON fields are exactly `currentPlatform`, `platforms`, `id`, `protocol`, `baseURL`, `apiKey`, and `model`.
- Supported protocols are exactly `openai` and `anthropic`.
- `config.json` is ignored; `config.example.json` contains no real credentials.
- Only `CONFIG_PATH` and `AGENT_PROMPT` remain as startup environment variables.
- API keys must never appear in logs or errors.
- Existing Provider translation and Engine Thinking behavior must remain unchanged.
- The current workspace has no `.git` metadata, so commit steps are unavailable; every task still ends with a fresh test checkpoint.

---

### Task 1: Strict platform configuration loader

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces: `func Load(path string) (*Config, error)`
- Produces: `func (c *Config) Current() (PlatformConfig, error)`
- Produces: `type PlatformConfig struct { ID, Protocol, BaseURL, APIKey, Model string }`

- [ ] **Step 1: Write failing happy-path tests**

Create `internal/config/config_test.go` with a helper that writes JSON under `t.TempDir()`. Verify that `Load` trims fields, lowercases `protocol`, appends `/` to `baseURL`, and `Current` returns the selected profile:

```go
func TestLoadSelectsAndNormalizesCurrentPlatform(t *testing.T) {
    path := writeConfig(t, `{
      "currentPlatform":" deepseek ",
      "platforms":[{
        "id":" deepseek ","protocol":" OpenAI ",
        "baseURL":"https://api.deepseek.com/v1",
        "apiKey":" key ","model":" deepseek-chat "
      }]
    }`)

    cfg, err := Load(path)
    if err != nil { t.Fatalf("Load() error = %v", err) }
    current, err := cfg.Current()
    if err != nil { t.Fatalf("Current() error = %v", err) }
    if current.ID != "deepseek" || current.Protocol != "openai" ||
        current.BaseURL != "https://api.deepseek.com/v1/" ||
        current.APIKey != "key" || current.Model != "deepseek-chat" {
        t.Fatalf("current = %#v", current)
    }
}
```

- [ ] **Step 2: Run the happy-path test and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test ./internal/config`

Expected: FAIL because package `internal/config` and `Load` do not exist.

- [ ] **Step 3: Add failing validation tests**

Add table-driven cases with exact invalid documents and expected error fragments:

```go
func TestLoadRejectsInvalidConfiguration(t *testing.T) {
    tests := []struct { name, document, want string }{
        {"unknown field", `{"currentPlatform":"x","platforms":[],"typo":true}`, "unknown field"},
        {"trailing JSON", `{"currentPlatform":"x","platforms":[]} {}`, "多余内容"},
        {"duplicate id", `{"currentPlatform":"x","platforms":[
          {"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","apiKey":"k"},
          {"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}]}`, "重复"},
        {"missing current", `{"currentPlatform":"missing","platforms":[
          {"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}]}`, "可用平台"},
        {"missing current key", `{"currentPlatform":"x","platforms":[
          {"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}]}`, "apiKey"},
        {"unsupported protocol", `{"currentPlatform":"x","platforms":[
          {"id":"x","protocol":"other","baseURL":"https://x.test/","apiKey":"k","model":"m"}]}`, "protocol"},
        {"invalid URL", `{"currentPlatform":"x","platforms":[
          {"id":"x","protocol":"openai","baseURL":"file:///tmp/x","apiKey":"k","model":"m"}]}`, "baseURL"},
        {"empty model", `{"currentPlatform":"x","platforms":[
          {"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":" "}]}`, "model"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := Load(writeConfig(t, tt.document))
            if err == nil || !strings.Contains(err.Error(), tt.want) {
                t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
            }
        })
    }
}
```

- [ ] **Step 4: Implement `internal/config/config.go` minimally**

Implement:

```go
type Config struct {
    CurrentPlatform string           `json:"currentPlatform"`
    Platforms       []PlatformConfig `json:"platforms"`
}

type PlatformConfig struct {
    ID       string `json:"id"`
    Protocol string `json:"protocol"`
    BaseURL  string `json:"baseURL"`
    APIKey   string `json:"apiKey"`
    Model    string `json:"model"`
}

func Load(path string) (*Config, error)
func (c *Config) Current() (PlatformConfig, error)
```

`Load` must open the exact path, call `json.Decoder.DisallowUnknownFields`, require the second decode to return `io.EOF`, trim all fields, lowercase protocols, validate unique IDs and required non-secret fields, normalize URLs to one trailing slash, verify `currentPlatform` exists, and require only the selected profile's API key. Errors must wrap the config path and never include key values.

- [ ] **Step 5: Run Task 1 tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/config`

Expected: PASS with package `github.com/PycMono/go-reagent/internal/config`.

---

### Task 2: Protocol-driven Provider factory

**Files:**
- Create: `internal/provider/factory.go`
- Create: `internal/provider/factory_test.go`
- Modify: `internal/provider/openai.go`
- Modify: `internal/provider/claude.go`
- Modify: `internal/provider/openai_test.go`
- Modify: `internal/provider/claude_test.go`
- Delete: `internal/provider/deepseek.go`
- Delete: `internal/provider/environment.go`

**Interfaces:**
- Consumes: normalized `config.PlatformConfig` fields through scalar options.
- Produces: `func New(options Options) (LLMProvider, error)`.
- Preserves: `func (p *OpenAIProvider) Generate(...)` and `func (p *ClaudeProvider) Generate(...)`.

- [ ] **Step 1: Write failing factory tests**

Create `internal/provider/factory_test.go`:

```go
func TestNewSelectsProtocolAdapter(t *testing.T) {
    base := Options{Name: "test", BaseURL: "https://example.com/v1/", APIKey: "secret", Model: "model"}
    tests := []struct {
        protocol string
        assert   func(*testing.T, LLMProvider)
    }{
        {"openai", func(t *testing.T, p LLMProvider) {
            if _, ok := p.(*OpenAIProvider); !ok { t.Fatalf("provider = %T", p) }
        }},
        {"anthropic", func(t *testing.T, p LLMProvider) {
            if _, ok := p.(*ClaudeProvider); !ok { t.Fatalf("provider = %T", p) }
        }},
    }
    for _, tt := range tests {
        t.Run(tt.protocol, func(t *testing.T) {
            options := base
            options.Protocol = tt.protocol
            p, err := New(options)
            if err != nil { t.Fatalf("New() error = %v", err) }
            tt.assert(t, p)
        })
    }
}

func TestNewRejectsInvalidOptionsWithoutLeakingAPIKey(t *testing.T) {
    secret := "never-print-this"
    _, err := New(Options{Protocol: "other", APIKey: secret})
    if err == nil { t.Fatal("New() error = nil") }
    if strings.Contains(err.Error(), secret) { t.Fatalf("error leaks API key: %v", err) }
}
```

- [ ] **Step 2: Run factory tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestNew' ./internal/provider`

Expected: FAIL because `Options` and `New` do not exist.

- [ ] **Step 3: Implement the factory**

Create:

```go
type Options struct {
    Name     string
    Protocol string
    BaseURL  string
    APIKey   string
    Model    string
}

func New(options Options) (LLMProvider, error) {
    if strings.TrimSpace(options.APIKey) == "" { return nil, errors.New("apiKey 不能为空") }
    if strings.TrimSpace(options.Model) == "" { return nil, errors.New("model 不能为空") }
    if strings.TrimSpace(options.BaseURL) == "" { return nil, errors.New("baseURL 不能为空") }
    switch strings.ToLower(strings.TrimSpace(options.Protocol)) {
    case "openai":
        return newOpenAICompatibleProvider(options.APIKey, options.BaseURL, options.Model, options.Name), nil
    case "anthropic":
        return newClaudeProvider(options.APIKey, options.BaseURL, options.Model, options.Name), nil
    default:
        return nil, fmt.Errorf("不支持的 Provider protocol %q", options.Protocol)
    }
}
```

Remove environment-backed vendor constructors from `openai.go` and `claude.go`; delete `deepseek.go` and `environment.go`. Update old constructor tests to use `New(Options)` while retaining all HTTP translation tests unchanged.

- [ ] **Step 4: Run all Provider tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/provider`

Expected: PASS. The `httptest` command may require loopback-port approval in the sandbox.

---

### Task 3: Migrate CLI bootstrap to `config.json`

**Files:**
- Modify: `cmd/reagent/main.go`
- Modify: `cmd/reagent/main_test.go`

**Interfaces:**
- Consumes: `config.Load`, `Config.Current`, and `provider.New(Options)`.
- Produces: `func providerFromConfig(path string) (provider.LLMProvider, config.PlatformConfig, error)`.

- [ ] **Step 1: Replace environment-constructor tests with failing config-bootstrap tests**

Add a temporary JSON configuration and assert selection:

```go
func TestProviderFromConfigBuildsSelectedPlatform(t *testing.T) {
    path := writeAppConfig(t, `{
      "currentPlatform":"zhipu",
      "platforms":[{
        "id":"zhipu","protocol":"anthropic",
        "baseURL":"https://example.com/anthropic/",
        "apiKey":"fake-key","model":"glm-test"
      }]
    }`)
    llmProvider, platform, err := providerFromConfig(path)
    if err != nil { t.Fatalf("providerFromConfig() error = %v", err) }
    if llmProvider == nil || platform.ID != "zhipu" || platform.Model != "glm-test" {
        t.Fatalf("provider = %T, platform = %#v", llmProvider, platform)
    }
}

func TestProviderFromConfigReturnsLoadError(t *testing.T) {
    _, _, err := providerFromConfig(filepath.Join(t.TempDir(), "missing.json"))
    if err == nil || !strings.Contains(err.Error(), "missing.json") {
        t.Fatalf("providerFromConfig() error = %v", err)
    }
}
```

- [ ] **Step 2: Run CLI tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestProviderFromConfig' ./cmd/reagent`

Expected: FAIL because `providerFromConfig` does not exist.

- [ ] **Step 3: Implement config-driven bootstrap**

Replace `providerFromEnvironment` with:

```go
func providerFromConfig(path string) (provider.LLMProvider, config.PlatformConfig, error) {
    cfg, err := config.Load(path)
    if err != nil { return nil, config.PlatformConfig{}, err }
    platform, err := cfg.Current()
    if err != nil { return nil, config.PlatformConfig{}, err }
    llmProvider, err := provider.New(provider.Options{
        Name: platform.ID, Protocol: platform.Protocol, BaseURL: platform.BaseURL,
        APIKey: platform.APIKey, Model: platform.Model,
    })
    if err != nil {
        return nil, config.PlatformConfig{}, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
    }
    return llmProvider, platform, nil
}
```

In `main`, resolve `CONFIG_PATH` with default `config.json`, log only `ID`, `Protocol`, and `Model`, preserve `AGENT_PROMPT`, and keep Thinking enabled.

- [ ] **Step 4: Run CLI tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./cmd/reagent`

Expected: PASS.

---

### Task 4: Secure templates and documentation

**Files:**
- Create: `.gitignore`
- Create: `config.example.json`
- Create: `config.json`
- Modify: `README.md`

**Interfaces:**
- Consumes: the exact JSON schema implemented by `internal/config`.
- Produces: a local startup configuration and a safe repository template.

- [ ] **Step 1: Create the ignored local configuration and safe template**

Both JSON files contain the approved DeepSeek, Zhipu OpenAI, and Zhipu Anthropic profiles. `config.json` uses empty API key values for the user to fill locally. `config.example.json` also uses empty API keys and contains no secret-like values. Add exactly this ignore rule:

```gitignore
config.json
```

- [ ] **Step 2: Update README startup documentation**

Document:

```bash
cp config.example.json config.json
chmod 600 config.json
go run ./cmd/reagent
```

Explain `currentPlatform`, all platform fields, `CONFIG_PATH`, the two supported protocols, and that platform switching requires changing only `currentPlatform`. Remove the old API-key and LLM-selection environment-variable instructions and update the project tree.

- [ ] **Step 3: Verify template syntax and intentional credential failure**

Run: `jq empty config.example.json config.json`

Expected: exit code 0, proving both templates are valid JSON.

Run: `CONFIG_PATH=config.example.json GOCACHE=/tmp/go-reagent-build-cache go run ./cmd/reagent`

Expected: exit code non-zero with `当前平台 "deepseek" 未配置 apiKey`; no network request is made.

---

### Task 5: Full regression verification

**Files:**
- Verify all files under `cmd` and `internal`.

**Interfaces:**
- Verifies the complete configuration-to-engine integration.

- [ ] **Step 1: Format Go code**

Run: `gofmt -w cmd internal`

Expected: exit code 0.

- [ ] **Step 2: Run static analysis**

Run: `GOCACHE=/tmp/go-reagent-build-cache go vet ./...`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 3: Run race-enabled tests**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./...`

Expected: all packages PASS; `internal/tools` may report `[no test files]`.

- [ ] **Step 4: Verify formatting and secret hygiene**

Run: `gofmt -l cmd internal`

Expected: no output.

Run: `rg -n 'sk-[A-Za-z0-9]{20,}|apiKey"\s*:\s*"[^"[:space:]]{20,}' --glob '!go.sum' --glob '!docs/**' .`

Expected: no real-looking credential values in repository files.
