# Model Cost Observability Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the complete `feature/model-cost-observability` capability onto the current `master` architecture and guarantee that every successful model response accepted by `agent.Loop` has valid Usage, a calculated USD cost, and exactly one ordered invocation record.

**Architecture:** Public usage and pricing contracts live in `ai`, while invocation phase and run output live in `agent`. Vendor clients extract raw tokens, `internal/observability.CostTracker` enriches and validates them, `internal/bootstrap` makes that tracker mandatory for the default SDK/CLI graph, and `agent.Loop` independently rejects unmetered or incorrectly costed responses. Bundled CLI persistence writes messages and invocation rows atomically under `internal/cli/conversation/mysql`.

**Tech Stack:** Go 1.26, OpenAI Go SDK v3, Anthropic Go SDK, Uber Fx, go-logger-sdk, GORM/MySQL, sqlmock, standard `testing` package.

## Global Constraints

- All prices and calculated costs use USD per one million tokens.
- Every successful provider response accepted by Thinking or Action, including repeated Action calls in tool loops, has exactly one invocation with valid Usage and calculated cost.
- Missing, partial, negative, NaN, infinite, or incorrectly calculated Usage terminates the Run; do not return an uncosted response as successful.
- A provider transport/API error has no fabricated token or cost record because no trustworthy Usage exists.
- Platform pricing is required, finite, non-negative, less than `100,000,000`, and may be zero for a free model. Calculated per-call cost uses the same exclusive upper bound required by `DECIMAL(20,12)`.
- Preserve the public `Loop.Run(context.Context, RunContext, Reporter) ([]ai.Message, error)` signature.
- Preserve the public dependency direction `ai <- agent <- reagent`; do not restore legacy `internal/schema`, `internal/provider`, `internal/engine`, or `internal/conversation` packages.
- Persist customer input and visible Action/tool messages only; never persist Thinking text, complete provider requests, tool definitions, API keys, or authorization headers.
- The invocation ledger is authoritative for totals; never add ledger totals to duplicated `Message.Usage` values.
- Use red-green-refactor for each behavior and run fresh verification before each commit.

---

## File Structure

- `ai/message.go`: normalized Usage attached to model messages.
- `ai/model.go`: required per-platform pricing contract.
- `agent/run.go`: public invocation phase, invocation record, and `RunResult.Invocations`.
- `agent/loop.go`: mandatory metering invariant and ordered phase capture.
- `agent/loop_result.go`: private detailed loop result used by `Agent.Run` while preserving `Loop.Run` compatibility.
- `agent/agent.go`: deep-copy Usage and expose detailed loop invocations.
- `ai/providers/openai/client.go`: OpenAI prompt/completion token mapping.
- `ai/providers/anthropic/client.go`: Anthropic input/output token mapping.
- `internal/observability/tracker.go`: strict cost/latency decorator and structured logs.
- `internal/bootstrap/module.go`: mandatory default-client decoration.
- `config_validate.go`, `config.go`, `bootstrap.go`: pricing validation, aliases, and deep configuration copy.
- `internal/cli/conversation/{store,runner}.go`: carry invocation records into persistence.
- `internal/cli/conversation/mysql/{model,invocation,store}.go`: validate and atomically persist ledger rows.
- `migrations/0002_model_invocation_observability.{up,down}.sql`: durable ledger schema.
- `config.example.json`, `README.md`, `docs/conversation-persistence.md`, `docs/sdk-architecture.md`: configuration, runtime, privacy, and aggregation documentation.

---

### Task 1: Add public Usage, invocation, and pricing contracts

**Files:**
- Modify: `ai/message.go`
- Modify: `ai/message_test.go`
- Modify: `ai/model.go`
- Modify: `agent/run.go`
- Modify: `agent/message_test.go`
- Modify: `config.go`
- Modify: `config_validate.go`
- Modify: `config_test.go`
- Modify: `bootstrap.go`
- Modify valid `ai.PlatformConfig` fixtures found by `rg -n 'PlatformConfig\\{' --glob '*.go'`

**Interfaces:**
- Produces: `ai.Usage`, `ai.PricingConfig`, and `ai.PlatformConfig.Pricing`.
- Produces: `agent.ModelInvocationPhase`, `agent.ModelInvocation`, and `agent.RunResult.Invocations`.
- Preserves: decoding messages without `usage`, decoding results without `invocations`, and root aliases for public SDK users.

- [ ] **Step 1: Write failing JSON contract tests**

Add to `ai/message_test.go`:

```go
func TestMessageUsageJSONRoundTripAndOmission(t *testing.T) {
	want := ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
		InputTokens: 120, OutputTokens: 30,
		InputPriceUSDPerMillionTokens: 0.15,
		OutputPriceUSDPerMillionTokens: 0.60,
		CostUSD: 0.000036, LatencyMS: 245,
		PlatformID: "zhipu", Model: "glm-4.5-air",
	}}
	encoded, err := json.Marshal(want)
	if err != nil { t.Fatal(err) }
	var got ai.Message
	if err := json.Unmarshal(encoded, &got); err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(got, want) { t.Fatalf("round trip = %#v, want %#v", got, want) }

	withoutUsage, err := json.Marshal(ai.Message{Role: ai.RoleAssistant})
	if err != nil { t.Fatal(err) }
	if strings.Contains(string(withoutUsage), "usage") {
		t.Fatalf("nil Usage serialized: %s", withoutUsage)
	}
}
```

Add to `agent/message_test.go`:

```go
func TestRunResultInvocationJSONContract(t *testing.T) {
	result := agent.RunResult{RunID: "run-1", Invocations: []agent.ModelInvocation{{
		Sequence: 1, Phase: agent.ModelInvocationPhaseAction,
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 4},
	}}}
	encoded, err := json.Marshal(result)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(encoded), `"phase":"action"`) ||
		!strings.Contains(string(encoded), `"input_tokens":10`) {
		t.Fatalf("RunResult JSON = %s", encoded)
	}
}
```

- [ ] **Step 2: Run contract tests and verify RED**

Run: `go test ./ai ./agent -run 'Test(MessageUsage|RunResultInvocation)' -count=1`

Expected: compilation fails because Usage and ModelInvocation do not exist.

- [ ] **Step 3: Write failing pricing and clone-isolation tests**

Add these focused cases in `config_test.go` (construct NaN/Inf cases programmatically because JSON cannot represent them):

```go
func TestPlatformPricingValidation(t *testing.T) {
	tests := []struct {
		name string
		pricing *ai.PricingConfig
	}{
		{name: "missing"},
		{name: "negative input", pricing: &ai.PricingConfig{InputUSDPerMillionTokens: -1}},
		{name: "negative output", pricing: &ai.PricingConfig{OutputUSDPerMillionTokens: -1}},
		{name: "NaN input", pricing: &ai.PricingConfig{InputUSDPerMillionTokens: math.NaN()}},
		{name: "infinite output", pricing: &ai.PricingConfig{OutputUSDPerMillionTokens: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := ai.PlatformConfig{ID: "x", Protocol: ai.ProtocolOpenAI,
				BaseURL: "https://x.test/", APIKey: "k", Model: "m", Pricing: tt.pricing}
			if err := validatePlatform(&platform, 0); err == nil || !strings.Contains(err.Error(), "pricing") {
				t.Fatalf("validatePlatform() error = %v", err)
			}
		})
	}
}

func TestPlatformPricingAllowsFreeModel(t *testing.T) {
	platform := ai.PlatformConfig{ID: "x", Protocol: ai.ProtocolOpenAI,
		BaseURL: "https://x.test/", APIKey: "k", Model: "m", Pricing: &ai.PricingConfig{}}
	if err := validatePlatform(&platform, 0); err != nil { t.Fatalf("validatePlatform() error = %v", err) }
}

func TestCloneConfigCopiesPricing(t *testing.T) {
	input := &Config{Platforms: []PlatformConfig{{Pricing: &ai.PricingConfig{InputUSDPerMillionTokens: 0.15}}}}
	cloned := cloneConfig(input)
	input.Platforms[0].Pricing.InputUSDPerMillionTokens = 9
	if cloned.Platforms[0].Pricing.InputUSDPerMillionTokens != 0.15 {
		t.Fatalf("cloned pricing mutated: %#v", cloned.Platforms[0].Pricing)
	}
}
```

Use this valid JSON block in all success fixtures:

```json
"pricing": {
  "input_usd_per_million_tokens": 0.15,
  "output_usd_per_million_tokens": 0.60
}
```

- [ ] **Step 4: Run pricing tests and verify RED**

Run: `go test . -run 'Test.*(Pricing|CloneConfig)' -count=1`

Expected: compilation fails because `ai.PricingConfig` and `PlatformConfig.Pricing` do not exist.

- [ ] **Step 5: Implement public contracts and validation**

Add to `ai/message.go`:

```go
type Usage struct {
	InputTokens                    int64   `json:"input_tokens"`
	OutputTokens                   int64   `json:"output_tokens"`
	InputPriceUSDPerMillionTokens  float64 `json:"input_price_usd_per_million_tokens"`
	OutputPriceUSDPerMillionTokens float64 `json:"output_price_usd_per_million_tokens"`
	CostUSD                        float64 `json:"cost_usd"`
	LatencyMS                      int64   `json:"latency_ms"`
	PlatformID                     string  `json:"platform_id"`
	Model                          string  `json:"model"`
}
```

Add `Usage *Usage ` + "`json:\"usage,omitempty\"`" + ` to `ai.Message`. Add to `ai/model.go`:

```go
type PricingConfig struct {
	InputUSDPerMillionTokens  float64 ` + "`json:\"input_usd_per_million_tokens\" yaml:\"input_usd_per_million_tokens\" toml:\"input_usd_per_million_tokens\"`" + `
	OutputUSDPerMillionTokens float64 ` + "`json:\"output_usd_per_million_tokens\" yaml:\"output_usd_per_million_tokens\" toml:\"output_usd_per_million_tokens\"`" + `
}
```

Add `Pricing *PricingConfig` with `pricing` JSON/YAML/TOML tags to `PlatformConfig`. In `agent/run.go`, add:

```go
type ModelInvocationPhase string

const (
	ModelInvocationPhaseThinking ModelInvocationPhase = "thinking"
	ModelInvocationPhaseAction   ModelInvocationPhase = "action"
)

type ModelInvocation struct {
	Sequence uint32               ` + "`json:\"sequence\"`" + `
	Phase    ModelInvocationPhase ` + "`json:\"phase\"`" + `
	Usage    ai.Usage             ` + "`json:\"usage\"`" + `
}
```

Extend `RunResult` with `Invocations []ModelInvocation ` + "`json:\"invocations,omitempty\"`" + `. Validate required finite non-negative prices with `math.IsNaN` and `math.IsInf` in `config_validate.go`. Expose `type PricingConfig = ai.PricingConfig` from `config.go`. Deep-copy every non-nil pricing pointer in `cloneConfig`.

- [ ] **Step 6: Update valid configuration fixtures and verify GREEN**

Use `rg -l 'PlatformConfig\\{' --glob '*.go'` and `rg -l '"platforms"' --glob '*.go' --glob '*.json'` to enumerate fixtures. Add explicit pricing to every fixture intended to pass; keep missing pricing only in the dedicated rejection test.

Run: `gofmt -w ai/message.go ai/message_test.go ai/model.go agent/run.go agent/message_test.go config.go config_validate.go config_test.go bootstrap.go`

Run: `go test ./ai ./agent . ./internal/bootstrap ./ai/providers ./tests/integration -count=1`

Expected: PASS.

- [ ] **Step 7: Commit the public contracts**

```bash
git add ai agent/run.go agent/message_test.go config.go config_validate.go config_test.go bootstrap.go internal/bootstrap ai/providers tests/integration config.example.json
git commit -m "feat: define public model usage and pricing contracts"
```

---

### Task 2: Extract vendor token Usage

**Files:**
- Modify: `ai/providers/openai/client.go`
- Modify: `ai/providers/openai/client_test.go`
- Modify: `ai/providers/anthropic/client.go`
- Modify: `ai/providers/anthropic/client_test.go`

**Interfaces:**
- Consumes: `ai.Usage` from Task 1.
- Produces: raw input/output tokens when the SDK response contains Usage, including explicit zero-token Usage.
- Preserves: outbound history conversion; stored `Message.Usage` must not be sent back to either vendor.

- [ ] **Step 1: Write failing provider mapping tests**

Change the OpenAI HTTP fixture to `prompt_tokens:123` and `completion_tokens:45`; change Anthropic to `input_tokens:123` and `output_tokens:45`. After `Generate`, assert:

```go
if result.Usage == nil || result.Usage.InputTokens != 123 || result.Usage.OutputTokens != 45 {
	t.Fatalf("Usage = %#v, want input=123 output=45", result.Usage)
}
```

Add an omitted-Usage fixture for each adapter and assert `result.Usage == nil`. Add an explicit zero-token Usage fixture and assert the pointer is non-nil.

- [ ] **Step 2: Run provider tests and verify RED**

Run: `go test ./ai/providers/openai ./ai/providers/anthropic -run 'Test.*Usage' -count=1`

Expected: Usage assertions fail because provider responses do not populate `ai.Message.Usage`.

- [ ] **Step 3: Implement presence-aware token mapping**

In OpenAI `Generate`, before returning:

```go
if response.JSON.Usage.Valid() {
	result.Usage = &ai.Usage{
		InputTokens: response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
}
```

In Anthropic `Generate`, before returning:

```go
if response.JSON.Usage.Valid() {
	result.Usage = &ai.Usage{
		InputTokens: response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}
}
```

Do not test token counts with `> 0`, and do not add Anthropic cache creation/read fields to `InputTokens`.

- [ ] **Step 4: Verify both adapters and commit**

Run: `gofmt -w ai/providers/openai/client.go ai/providers/openai/client_test.go ai/providers/anthropic/client.go ai/providers/anthropic/client_test.go`

Run: `go test ./ai/providers/openai ./ai/providers/anthropic -count=1`

Expected: PASS.

```bash
git add ai/providers/openai ai/providers/anthropic
git commit -m "feat: capture provider token usage"
```

---

### Task 3: Add strict cost tracking and default wiring

**Files:**
- Create: `internal/observability/tracker.go`
- Create: `internal/observability/tracker_test.go`
- Modify: `internal/bootstrap/module.go`
- Modify: `internal/bootstrap/module_test.go`
- Modify: `tests/integration/fx_dependency_graph_test.go`

**Interfaces:**
- Produces: `observability.NewCostTracker(ai.Client, string, string, Pricing) (*CostTracker, error)`.
- Produces: a concurrency-safe `ai.Client` decorator that returns only fully enriched Usage on success.
- Default graph: `providers.New(config) -> observability.NewCostTracker(...) -> agent.NewLoop(...)`.

- [ ] **Step 1: Write failing deterministic tracker tests**

Create a local `clientFunc` implementing `ai.Client`. Inject a two-value clock into an unexported constructor and test the exact formula:

```go
func TestCostTrackerCalculatesEverySuccessfulCall(t *testing.T) {
	original := &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
		InputTokens: 2_000_000, OutputTokens: 500_000,
	}}
	times := []time.Time{time.Unix(0, 0), time.Unix(0, 0).Add(2500 * time.Millisecond)}
	index := 0
	tracker, err := newCostTracker(clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return original, nil
	}), "zhipu", "glm-4.5-air", Pricing{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60}, func() time.Time {
		value := times[index]; index++; return value
	})
	if err != nil { t.Fatal(err) }
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err != nil { t.Fatal(err) }
	if result.Usage.CostUSD != 0.60 || result.Usage.LatencyMS != 2500 ||
		result.Usage.PlatformID != "zhipu" || result.Usage.Model != "glm-4.5-air" {
		t.Fatalf("Usage = %#v", result.Usage)
	}
	if original.Usage.CostUSD != 0 { t.Fatalf("delegate Usage mutated: %#v", original.Usage) }
}
```

Add this mandatory missing-Usage test:

```go
func TestCostTrackerRejectsMissingUsage(t *testing.T) {
	tracker, err := NewCostTracker(clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("uncosted")}}, nil
	}), "test", "model", Pricing{})
	if err != nil { t.Fatal(err) }
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err == nil || !errors.Is(err, ai.ErrGeneration) {
		t.Fatalf("Generate() error = %v, want generation error", err)
	}
	if result != nil {
		t.Fatalf("Generate() result = %#v, want nil on missing Usage", result)
	}
}
```

Add table-driven constructor cases using `nil`, blank platform/model, `-1`, `math.NaN()`, and `math.Inf(1)`. Add Generate cases for negative input/output tokens. Add a free-pricing case asserting exact zero cost, a delegated sentinel error case asserting `errors.Is`, and a 32-goroutine case asserting each returned `Usage` pointer is unique and every cost is identical.

- [ ] **Step 2: Run tracker tests and verify RED**

Run: `go test ./internal/observability -run TestCostTracker -count=1`

Expected: package or symbols do not exist.

- [ ] **Step 3: Implement the immutable strict decorator**

Define:

```go
type Pricing struct {
	InputUSDPerMillionTokens  float64
	OutputUSDPerMillionTokens float64
}

type CostTracker struct {
	next ai.Client
	platformID string
	model string
	pricing Pricing
	now func() time.Time
}
```

`Generate` must call the delegate exactly once, measure that call only, copy the response and Usage before enrichment, and compute:

```go
cost := (float64(usage.InputTokens)*pricing.InputUSDPerMillionTokens +
	float64(usage.OutputTokens)*pricing.OutputUSDPerMillionTokens) / 1_000_000
```

For missing/invalid Usage, emit `usage_missing` or `usage_invalid` and return an `ai.WrapGeneration("cost tracking", err)` error. For success, emit component, platform, model, tokens, prices, cost, and latency. Provider errors retain their identity and are not converted to fake Usage.

- [ ] **Step 4: Write failing bootstrap wiring test**

In `internal/bootstrap/module_test.go`, populate `ai.Client` and assert its concrete type is `*observability.CostTracker`:

```go
var client ai.Client
app := fx.New(
	fx.NopLogger,
	fx.Supply(
		ai.PlatformConfig{ID: "test", Protocol: ai.ProtocolOpenAI,
			BaseURL: "http://127.0.0.1/v1/", APIKey: "key", Model: "model",
			Pricing: &ai.PricingConfig{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60}},
		workspace.WorkDir(t.TempDir()),
	),
	Module,
	fx.Populate(&client),
)
if err := app.Err(); err != nil { t.Fatal(err) }
if _, ok := client.(*observability.CostTracker); !ok {
	t.Fatalf("client type = %T, want *observability.CostTracker", client)
}
```

- [ ] **Step 5: Wire the tracker into the default graph**

Replace `newClient` with:

```go
func newClient(config ai.PlatformConfig) (ai.Client, error) {
	if config.Pricing == nil {
		return nil, errors.New("model pricing is required")
	}
	next, err := providers.New(config)
	if err != nil { return nil, err }
	return observability.NewCostTracker(next, config.ID, config.Model, observability.Pricing{
		InputUSDPerMillionTokens: config.Pricing.InputUSDPerMillionTokens,
		OutputUSDPerMillionTokens: config.Pricing.OutputUSDPerMillionTokens,
	})
}
```

The root config validator normally guarantees non-nil Pricing before the default graph is built; the explicit check keeps direct Fx/module use from panicking. Add a bootstrap test that supplies nil Pricing and asserts `app.Err()` contains `pricing`.

- [ ] **Step 6: Verify tracker, wiring, and dependency direction**

Run: `gofmt -w internal/observability/tracker.go internal/observability/tracker_test.go internal/bootstrap/module.go internal/bootstrap/module_test.go tests/integration/fx_dependency_graph_test.go`

Run: `go test ./internal/observability ./internal/bootstrap ./tests/integration -count=1`

Run: `go test ./tests/integration -run 'Test(PublicPackageDependencyBoundaries|LegacyInternalPackageImportsAreAbsent)' -count=1`

Expected: PASS; `ai` and `agent` import no internal packages.

- [ ] **Step 7: Commit strict cost tracking**

```bash
git add internal/observability internal/bootstrap tests/integration/fx_dependency_graph_test.go
git commit -m "feat: require model cost tracking in default runtime"
```

---

### Task 4: Enforce metering and expose ordered invocations in Agent

**Files:**
- Create: `agent/loop_result.go`
- Modify: `agent/loop.go`
- Modify: `agent/loop_test.go`
- Modify: `agent/agent.go`
- Modify: `agent/agent_test.go`
- Modify: `agent/run_messages_test.go`

**Interfaces:**
- Preserves: public `Loop.Run(...) ([]ai.Message, error)`.
- Produces internally: `runDetailed(...) (loopResult, error)` containing messages and invocations.
- Produces publicly: `Agent.Run(...).Invocations` with provider-call ordinals and Thinking/Action phases.

- [ ] **Step 1: Write failing invocation-order test**

Use a fake client returning four fully enriched responses: Thinking 1, Action-with-tool 2, Thinking 3, final Action 4. The central assertions are:

```go
result, err := runtime.Run(context.Background(), agent.RunRequest{
	Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("run")}},
}, reporter)
if err != nil { t.Fatal(err) }
wantPhases := []agent.ModelInvocationPhase{
	agent.ModelInvocationPhaseThinking, agent.ModelInvocationPhaseAction,
	agent.ModelInvocationPhaseThinking, agent.ModelInvocationPhaseAction,
}
if len(result.Invocations) != 4 { t.Fatalf("Invocations = %#v", result.Invocations) }
for index, invocation := range result.Invocations {
	if invocation.Sequence != uint32(index+1) || invocation.Phase != wantPhases[index] {
		t.Fatalf("invocation %d = %#v", index, invocation)
	}
}
for _, message := range result.NewMessages {
	text, _ := ai.TextContent(message.Content)
	if strings.Contains(text, "private thinking") { t.Fatalf("Thinking leaked: %#v", result.NewMessages) }
}
```

Give each response distinct token counts so the test also proves Usage values are copied in call order. Assert Reporter message events contain only visible Action messages.

- [ ] **Step 2: Write failing strict-loop tests**

Add table cases for nil Usage, blank PlatformID, blank Model, negative tokens, negative latency, NaN/Inf prices or cost, and an incorrect `CostUSD`:

```go
tests := []struct {
	name string
	usage *ai.Usage
}{
	{name: "missing"},
	{name: "blank platform", usage: &ai.Usage{Model: "m"}},
	{name: "blank model", usage: &ai.Usage{PlatformID: "p"}},
	{name: "negative tokens", usage: &ai.Usage{PlatformID: "p", Model: "m", InputTokens: -1}},
	{name: "negative latency", usage: &ai.Usage{PlatformID: "p", Model: "m", LatencyMS: -1}},
	{name: "NaN price", usage: &ai.Usage{PlatformID: "p", Model: "m", InputPriceUSDPerMillionTokens: math.NaN()}},
	{name: "infinite cost", usage: &ai.Usage{PlatformID: "p", Model: "m", CostUSD: math.Inf(1)}},
	{name: "incorrect cost", usage: &ai.Usage{PlatformID: "p", Model: "m", InputTokens: 1_000_000,
		InputPriceUSDPerMillionTokens: 0.25, CostUSD: 0.20}},
}
```

Each case calls public `Loop.Run` directly and asserts:

```go
messages, err := loop.Run(context.Background(), runContext, nil)
if err == nil || !errors.Is(err, ai.ErrGeneration) {
	t.Fatalf("Run() error = %v, want generation error", err)
}
if len(messages) != 0 {
	t.Fatalf("unmetered response accepted: %#v", messages)
}
```

The incorrect-cost case uses input `1_000_000`, input price `0.25`, zero output tokens, and `CostUSD: 0.20`; expected calculated cost is `0.25`.

- [ ] **Step 3: Run Agent tests and verify RED**

Run: `go test ./agent -run 'Test.*(Invocation|Unmetered|InvalidUsage|IncorrectCost)' -count=1`

Expected: compilation or assertions fail because the loop does not return invocations or enforce metering.

- [ ] **Step 4: Implement private detailed loop execution**

Create:

```go
type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}
```

Keep public `Loop.Run` as a compatibility wrapper around `runDetailed`. In `runDetailed`, increment `callSequence` before each Thinking and Action call. After the client returns and before accepting the message, validate the fully enriched Usage and append:

```go
invocations = append(invocations, ModelInvocation{
	Sequence: callSequence,
	Phase: phase,
	Usage: *response.Usage,
})
```

Validate identity, non-negative integers, finite non-negative prices/cost, and this formula using a `1e-12` absolute tolerance:

```go
expected := (float64(u.InputTokens)*u.InputPriceUSDPerMillionTokens +
	float64(u.OutputTokens)*u.OutputPriceUSDPerMillionTokens) / 1_000_000
```

Return `ai.WrapGeneration("model usage", err)` on failure. Do not append the rejected response to context, `NewMessages`, invocation output, or Reporter events.

- [ ] **Step 5: Make Agent.Run use detailed output and clone Usage**

Call `a.loop.runDetailed`, copy both slices into `RunResult`, and extend `cloneMessage`:

```go
if message.Usage != nil {
	usage := *message.Usage
	message.Usage = &usage
}
```

Copy invocation slices by value. Preserve partial invocations and visible messages when a later provider or tool call fails.

- [ ] **Step 6: Update existing fake responses with valid metered Usage**

Add one test helper returning a fresh value:

```go
func meteredUsage() *ai.Usage {
	return &ai.Usage{PlatformID: "test", Model: "test-model"}
}
```

Attach it to every fake provider response intended to be accepted by a loop. Do not attach it to strict missing-Usage cases.

- [ ] **Step 7: Verify Agent behavior and compatibility**

Run: `gofmt -w agent/loop_result.go agent/loop.go agent/loop_test.go agent/agent.go agent/agent_test.go agent/run_messages_test.go`

Run: `go test ./agent -count=1`

Expected: PASS, including compile-time use of the unchanged public `Loop.Run` return signature.

- [ ] **Step 8: Commit Agent enforcement**

```bash
git add agent
git commit -m "feat: enforce costed model loop invocations"
```

---

### Task 5: Carry Usage and invocations through CLI conversations

**Files:**
- Modify: `internal/cli/conversation/store.go`
- Modify: `internal/cli/conversation/runner.go`
- Modify: `internal/cli/conversation/runner_test.go`
- Modify: `tests/integration/conversation_persistence_test.go`

**Interfaces:**
- Adds: `AppendRequest.Invocations []agent.ModelInvocation`.
- Preserves: caller isolation for message Usage pointers and invocation slices.
- Persists: completed invocations alongside any persisted partial visible messages.

- [ ] **Step 1: Write failing runner forwarding and clone tests**

Return an `agent.RunResult` containing one Action message with Usage and two invocations. Assert `AppendTurn` receives customer input, Action message, and both invocations:

Use:

```go
invocations := []agent.ModelInvocation{{
	Sequence: 1, Phase: agent.ModelInvocationPhaseThinking,
	Usage: ai.Usage{PlatformID: "test", Model: "model"},
}, {
	Sequence: 2, Phase: agent.ModelInvocationPhaseAction,
	Usage: ai.Usage{PlatformID: "test", Model: "model"},
}}

answer := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	Usage: &ai.Usage{PlatformID: "test", Model: "model"}}
runtime := &runnerRuntimeFake{result: agent.RunResult{NewMessages: []ai.Message{answer}, Invocations: invocations}}
store := &runnerStoreFake{snapshot: Snapshot{ConversationPK: 42, Version: 7}}
_, err := NewRunner(runtime, store, 100).Run(context.Background(), validConversationRunRequest(), nil)
if err != nil { t.Fatal(err) }
if !reflect.DeepEqual(store.appended.Invocations, invocations) {
	t.Fatalf("Invocations = %#v, want %#v", store.appended.Invocations, invocations)
}
runtime.result.NewMessages[0].Usage.PlatformID = "mutated"
runtime.result.Invocations[0].Usage.PlatformID = "mutated"
if store.appended.Messages[1].Usage.PlatformID != "test" ||
	store.appended.Invocations[0].Usage.PlatformID != "test" {
	t.Fatalf("stored request aliases runtime result: %#v", store.appended)
}
```

- [ ] **Step 2: Run conversation tests and verify RED**

Run: `go test ./internal/cli/conversation -run 'TestRunner.*(Invocation|Clone|Persist)' -count=1`

Expected: compilation/assertion failure because `AppendRequest` has no Invocations.

- [ ] **Step 3: Implement forwarding and deep cloning**

Add `Invocations []agent.ModelInvocation` to `AppendRequest`, forward `runtimeResult.Invocations`, and clone the slice before passing it to the Store. Extend conversation `cloneMessage` to copy a non-nil Usage pointer exactly as Agent cloning does.

Retain current partial-turn behavior: if the runtime returns an error and zero visible messages, skip `AppendTurn`; otherwise persist the customer input, completed visible messages, and completed invocations together.

- [ ] **Step 4: Update integration fake Store and verify**

Ensure `tests/integration/conversation_persistence_test.go` accepts and copies the new field without changing its optimistic version behavior.

Run: `gofmt -w internal/cli/conversation/store.go internal/cli/conversation/runner.go internal/cli/conversation/runner_test.go tests/integration/conversation_persistence_test.go`

Run: `go test ./internal/cli/conversation ./tests/integration -count=1`

Expected: PASS.

- [ ] **Step 5: Commit conversation transport**

```bash
git add internal/cli/conversation tests/integration/conversation_persistence_test.go
git commit -m "feat: carry model invocations through cli conversations"
```

---

### Task 6: Define the MySQL invocation ledger

**Files:**
- Create: `migrations/0002_model_invocation_observability.up.sql`
- Create: `migrations/0002_model_invocation_observability.down.sql`
- Create: `internal/cli/conversation/mysql/invocation.go`
- Create: `internal/cli/conversation/mysql/invocation_test.go`
- Modify: `internal/cli/conversation/mysql/model.go`
- Modify: `internal/cli/conversation/mysql/migration_test.go`
- Modify: `internal/cli/conversation/mysql/codec_test.go`

**Interfaces:**
- Produces: `agent_model_invocations` with unique `(conversation_pk, turn_version, sequence)` rows.
- Produces: `encodeInvocation(agent.ModelInvocation, conversationPK, turnVersion, runID)`.
- Stores decimal prices/cost as fixed-scale base-10 strings.

- [ ] **Step 1: Write failing migration contract test**

Read migration `0002` and require exact contract fragments:

```go
for _, want := range []string{
	"agent_model_invocations", "input_tokens", "output_tokens",
	"input_price_usd_per_million_tokens DECIMAL(20,12)",
	"output_price_usd_per_million_tokens DECIMAL(20,12)",
	"cost_usd DECIMAL(20,12)", "latency_ms", "phase", "platform_id", "model",
	"fk_agent_model_invocations_conversation", "idx_agent_model_invocations_conversation_time",
	"uq_agent_model_invocations_turn_sequence",
} {
	if !strings.Contains(sql, want) { t.Fatalf("migration missing %q", want) }
}
```

Assert the down migration is exactly `DROP TABLE IF EXISTS agent_model_invocations;` after trimming whitespace.

- [ ] **Step 2: Write failing encoder validation tests**

Test a valid invocation encodes fixed decimals:

```go
invocation := agent.ModelInvocation{Sequence: 2, Phase: agent.ModelInvocationPhaseAction, Usage: ai.Usage{
	InputTokens: 120, OutputTokens: 30,
	InputPriceUSDPerMillionTokens: 0.15, OutputPriceUSDPerMillionTokens: 0.60,
	CostUSD: 0.000036, LatencyMS: 245, PlatformID: "zhipu", Model: "glm-4.5-air",
}}
runID := "run-1"
row, err := encodeInvocation(invocation, 7, 3, &runID)
if err != nil { t.Fatal(err) }
if row.InputPriceUSDPerMillionTokens != "0.150000000000" ||
	row.OutputPriceUSDPerMillionTokens != "0.600000000000" || row.CostUSD != "0.000036000000" {
	t.Fatalf("decimal row = %#v", row)
}
runID = "mutated"
if row.RunID == nil || *row.RunID != "run-1" { t.Fatalf("RunID alias = %#v", row.RunID) }
```

Table-test zero sequence, invalid phase, blank platform/model, negative tokens/latency, and negative/NaN/infinite price or cost by mutating this valid value and requiring an error.

- [ ] **Step 3: Run ledger tests and verify RED**

Run: `go test ./internal/cli/conversation/mysql -run 'Test(ModelInvocationMigration|EncodeInvocation)' -count=1`

Expected: migration file and encoder symbols do not exist.

- [ ] **Step 4: Implement migration and row model**

Create `agent_model_invocations` with a foreign key to `agent_conversations(id)`, `turn_version`, nullable `run_id`, `sequence`, `phase`, identity, tokens, fixed-scale prices/cost, latency, and UTC timestamp. Add:

```go
type invocationRow struct {
	ID uint64 ` + "`gorm:\"column:id;primaryKey;autoIncrement\"`" + `
	ConversationPK uint64 ` + "`gorm:\"column:conversation_pk\"`" + `
	TurnVersion uint64 ` + "`gorm:\"column:turn_version\"`" + `
	RunID *string ` + "`gorm:\"column:run_id\"`" + `
	Sequence uint32 ` + "`gorm:\"column:sequence\"`" + `
	Phase string ` + "`gorm:\"column:phase\"`" + `
	PlatformID string ` + "`gorm:\"column:platform_id\"`" + `
	Model string ` + "`gorm:\"column:model\"`" + `
	InputTokens uint64 ` + "`gorm:\"column:input_tokens\"`" + `
	OutputTokens uint64 ` + "`gorm:\"column:output_tokens\"`" + `
	InputPriceUSDPerMillionTokens string ` + "`gorm:\"column:input_price_usd_per_million_tokens\"`" + `
	OutputPriceUSDPerMillionTokens string ` + "`gorm:\"column:output_price_usd_per_million_tokens\"`" + `
	CostUSD string ` + "`gorm:\"column:cost_usd\"`" + `
	LatencyMS uint64 ` + "`gorm:\"column:latency_ms\"`" + `
	CreatedAt time.Time ` + "`gorm:\"column:created_at\"`" + `
}
```

- [ ] **Step 5: Implement focused encoding**

Trim identity, validate every field before any database call, reject non-finite decimals, and format decimals with `strconv.FormatFloat(value, 'f', 12, 64)`. Preserve zero values as valid.

- [ ] **Step 6: Verify ledger schema and codec compatibility**

Run: `gofmt -w internal/cli/conversation/mysql/model.go internal/cli/conversation/mysql/invocation.go internal/cli/conversation/mysql/invocation_test.go internal/cli/conversation/mysql/migration_test.go internal/cli/conversation/mysql/codec_test.go`

Run: `go test ./internal/cli/conversation/mysql -run 'Test(ModelInvocationMigration|EncodeInvocation|MessageCodec)' -count=1`

Expected: PASS, and old message JSON without Usage still decodes.

- [ ] **Step 7: Commit ledger schema**

```bash
git add migrations/0002_model_invocation_observability.* internal/cli/conversation/mysql
git commit -m "feat: define model invocation ledger"
```

---

### Task 7: Persist messages and invocations atomically

**Files:**
- Modify: `internal/cli/conversation/mysql/store.go`
- Modify: `internal/cli/conversation/mysql/store_test.go`
- Modify: `internal/cli/conversation/mysql/store_integration_test.go`

**Interfaces:**
- Consumes: `AppendRequest.Invocations` and `encodeInvocation`.
- Guarantees: version update, message insert, and invocation insert share one transaction.

- [ ] **Step 1: Write failing successful transaction test**

Extend the sqlmock expectation sequence to require:

```text
BEGIN
UPDATE agent_conversations ... version
INSERT INTO agent_messages ...
INSERT INTO agent_model_invocations ...
COMMIT
```

Use two ordered invocations and assert `turn_version == ExpectedVersion + 1` and the nullable `run_id` matches message rows.

- [ ] **Step 2: Write failing rollback and validation tests**

For the rollback test, require this sqlmock order:

```go
mock.ExpectBegin()
expectVersionUpdate(mock, 11, 7).WillReturnResult(sqlmock.NewResult(0, 1))
mock.ExpectExec("INSERT INTO .*agent_messages").WillReturnResult(sqlmock.NewResult(1, 2))
insertErr := errors.New("insert invocations failed")
mock.ExpectExec("INSERT INTO .*agent_model_invocations").WillReturnError(insertErr)
mock.ExpectRollback()
err := NewStore(provider, provider).AppendTurn(context.Background(), requestWithInvocations())
if !errors.Is(err, insertErr) { t.Fatalf("AppendTurn() error = %v", err) }
```

Add table cases that mutate `requestWithInvocations()` to descending sequence, duplicate sequence, and malformed Usage, then require an error and `mock.ExpectationsWereMet()` with no `BEGIN`. Keep the existing message-only success test unchanged to prove backward compatibility.

- [ ] **Step 3: Run Store tests and verify RED**

Run: `go test ./internal/cli/conversation/mysql -run 'TestStoreAppendTurn.*Invocation' -count=1`

Expected: expectations fail because invocation rows are not inserted.

- [ ] **Step 4: Encode rows before opening the transaction**

Build `invocationRows` after message rows. Require each sequence to be greater than the previous sequence, then call `encodeInvocation` with the shared conversation PK, turn version, and run ID.

- [ ] **Step 5: Insert invocation rows inside the existing transaction**

Immediately after the message insert:

```go
if len(invocationRows) > 0 {
	if err := db.Create(&invocationRows).Error; err != nil {
		return err
	}
}
```

Do not create a second transaction or update cached totals on `agent_conversations`.

- [ ] **Step 6: Update the environment-gated live integration test**

When MySQL integration is enabled, read both `../../../../migrations/0001_conversation_persistence.up.sql` and `../../../../migrations/0002_model_invocation_observability.up.sql` in order, append one invocation, query it by conversation PK, and clean invocation rows before conversation rows because of the foreign key:

```go
var count int64
if err := db.Table("agent_model_invocations").
	Where("conversation_pk = ?", snapshot.ConversationPK).Count(&count).Error; err != nil { t.Fatal(err) }
if count != 1 { t.Fatalf("invocation count = %d, want 1", count) }
```

- [ ] **Step 7: Verify atomic persistence and commit**

Run: `gofmt -w internal/cli/conversation/mysql/store.go internal/cli/conversation/mysql/store_test.go internal/cli/conversation/mysql/store_integration_test.go`

Run: `go test ./internal/cli/conversation/mysql -count=1`

Expected: PASS; the live test may explicitly SKIP under its existing environment guard.

```bash
git add internal/cli/conversation/mysql
git commit -m "feat: persist model invocation metrics atomically"
```

---

### Task 8: Document, audit, and verify the complete migration

**Files:**
- Modify: `config.example.json`
- Modify: `README.md`
- Modify: `docs/conversation-persistence.md`
- Modify: `docs/sdk-architecture.md`
- Modify: `docs/superpowers/plans/2026-08-04-model-cost-observability.md` only for execution discoveries that change exact paths or commands

**Interfaces:**
- Documents: required pricing, mandatory metering failure behavior, public `RunResult.Invocations`, privacy boundary, migration order, and authoritative aggregation query.
- Verifies: the port contains the old branch capability without restoring old internal packages.

- [ ] **Step 1: Update configuration and public SDK documentation**

Add explicit pricing to every platform in `config.example.json`. In README and `docs/sdk-architecture.md`, document that every successful loop response has Usage and exactly one Invocation, and that missing/invalid Usage fails the Run. State that lower-level `agent` callers must supply a metered `ai.Client`.

- [ ] **Step 2: Update persistence documentation**

Document applying migration `0002` after `0001` and include:

```sql
SELECT
    SUM(input_tokens) AS input_tokens,
    SUM(output_tokens) AS output_tokens,
    SUM(cost_usd) AS cost_usd,
    SUM(latency_ms) AS latency_ms
FROM agent_model_invocations
WHERE conversation_pk = ?;
```

State that hidden Thinking text and complete provider requests are never stored, and that totals come only from `agent_model_invocations`.

- [ ] **Step 3: Run targeted feature verification fresh**

Run: `go test ./ai ./ai/providers/openai ./ai/providers/anthropic ./agent ./internal/observability ./internal/bootstrap ./internal/cli/conversation ./internal/cli/conversation/mysql ./tests/integration -count=1`

Expected: PASS with zero failures; MySQL integration may explicitly SKIP when its environment variable is absent.

- [ ] **Step 4: Run full repository verification fresh**

Run: `gofmt -w ai/message.go ai/model.go ai/providers/openai/client.go ai/providers/anthropic/client.go agent/run.go agent/loop_result.go agent/loop.go agent/agent.go internal/observability/tracker.go internal/bootstrap/module.go internal/cli/conversation/store.go internal/cli/conversation/runner.go internal/cli/conversation/mysql/model.go internal/cli/conversation/mysql/invocation.go internal/cli/conversation/mysql/store.go config.go config_validate.go bootstrap.go`

Run: `go test ./... -count=1`

Run: `go vet ./...`

Run: `git diff --check`

Expected: every command exits 0.

- [ ] **Step 5: Audit the port against both branches and the approved design**

Run: `git diff --stat feature/model-cost-observability...HEAD`

Run: `go list -json ./... | rg 'internal/(schema|provider|engine|conversation)'`

Expected: the first command shows architectural differences rather than missing cost layers; the second returns no legacy-package import matches.

Confirm from tests and code:

- OpenAI and Anthropic map upstream token Usage, including zero-token presence.
- Default SDK and CLI always wrap vendor clients in `CostTracker`.
- Thinking and every repeated Action response accepted by the loop has exact cost and one ordered invocation.
- Missing/invalid/incorrectly costed Usage terminates the Run.
- `Loop.Run` remains source-compatible.
- `Agent.RunResult`, root `reagent.RunResult`, visible Action message JSON, and CLI persistence carry Usage/invocations.
- Thinking content and complete provider requests have no persistence path.
- Message and invocation rows share one transaction.

- [ ] **Step 6: Commit documentation and final fixture updates**

```bash
git add config.example.json README.md docs/conversation-persistence.md docs/sdk-architecture.md docs/superpowers/plans/2026-08-04-model-cost-observability.md
git commit -m "docs: explain mandatory model cost observability"
```
