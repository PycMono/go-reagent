# Model Cost Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record input/output tokens, USD cost, latency, model identity, and Thinking/Action phase for every successfully metered LLM call while preserving only customer input and visible model output as conversation content.

**Architecture:** Provider adapters normalize vendor token counts, then an Fx-decorated `observability.CostTracker` enriches responses with prices, cost, and latency. The engine converts enriched Usage into ordered invocation records, and the conversation MySQL store atomically writes those records beside the existing message turn without persisting hidden Thinking text.

**Tech Stack:** Go 1.26, OpenAI Go SDK v3, Anthropic Go SDK, Uber Fx, go-logger-sdk, GORM/MySQL, sqlmock, standard `testing` package.

## Global Constraints

- All prices and calculated costs use USD.
- Platform pricing is required, uses USD per one million tokens, must be finite and non-negative, and permits zero for free models.
- Persist customer-submitted messages and visible Action responses; never persist complete provider request snapshots or Thinking content.
- The invocation ledger is authoritative for totals; do not sum message Usage and ledger Usage together.
- A call without trustworthy provider Usage produces a warning but no fabricated zero-cost invocation.
- Preserve all pre-existing uncommitted workspace edits. Execution must start with `superpowers:using-git-worktrees` or otherwise isolate commits before editing files that already contain user changes.
- Do not add a dashboard, HTTP billing API, currency conversion, streaming usage, cache-tier pricing, or mutable session aggregate.

---

## File Structure

- `internal/schema/message.go`: normalized per-call Usage attached to assistant messages.
- `internal/schema/run.go`: invocation phase, invocation record, and `RunResult.Invocations`.
- `internal/config/config.go`: platform pricing configuration shape.
- `internal/config/validate.go`: required finite/non-negative pricing validation.
- `internal/provider/openai.go`: OpenAI-compatible Usage mapping.
- `internal/provider/claude.go`: Anthropic Usage mapping.
- `internal/observability/tracker.go`: cost/latency provider decorator and structured logs.
- `internal/observability/register.go`: Fx decoration boundary; avoids a provider/observability import cycle.
- `internal/engine/loop_result.go`: engine-internal result containing messages and invocations.
- `internal/engine/agent_loop.go`: provider-call ordinal and explicit Thinking/Action recording.
- `internal/engine/runtime.go`: copies loop output into the public `RunResult`.
- `internal/conversation/store.go`: invocation persistence input contract.
- `internal/conversation/runner.go`: deep cloning and forwarding of Usage/invocations.
- `internal/conversation/mysql/model.go`: invocation GORM row and fixed-scale decimal values.
- `internal/conversation/mysql/invocation.go`: focused validation and schema-to-row encoding.
- `internal/conversation/mysql/store.go`: atomic message and invocation insertion.
- `migrations/0002_model_invocation_observability.{up,down}.sql`: durable invocation ledger schema.
- `config.example.json`, `README.md`, `docs/conversation-persistence.md`: configuration and query documentation.

---

### Task 1: Add normalized Usage and required platform pricing contracts

**Files:**
- Modify: `internal/schema/message.go`
- Modify: `internal/schema/run.go`
- Modify: `internal/schema/message_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/register_test.go`
- Modify: `internal/provider/register_test.go`
- Modify: `tests/integration/fx_dependency_graph_test.go`
- Modify valid platform fixtures in exactly these test files: `internal/config/config_test.go`, `internal/config/register_test.go`, `internal/provider/register_test.go`, and `tests/integration/fx_dependency_graph_test.go`

**Interfaces:**
- Produces: `schema.Usage`, `schema.ModelInvocationPhase`, `schema.ModelInvocation`, and `RunResult.Invocations`.
- Produces: `config.PricingConfig` and `PlatformConfig.Pricing *PricingConfig`.
- Preserves: existing JSON decoding of messages without `usage` and run results without `invocations`.

- [ ] **Step 1: Write failing schema JSON tests**

Append tests that require a populated Usage object to round-trip and nil Usage to remain omitted:

```go
func TestMessageUsageJSONContract(t *testing.T) {
	message := schema.Message{
		Role: schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock("done")},
		Usage: &schema.Usage{
			InputTokens: 120,
			OutputTokens: 30,
			InputPriceUSDPerMillionTokens: 0.15,
			OutputPriceUSDPerMillionTokens: 0.60,
			CostUSD: 0.000036,
			LatencyMS: 245,
			PlatformID: "zhipu",
			Model: "glm-4.5-air",
		},
	}

	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded schema.Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, message) {
		t.Fatalf("decoded = %#v, want %#v", decoded, message)
	}

	withoutUsage, err := json.Marshal(schema.Message{Role: schema.RoleAssistant})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withoutUsage), "usage") {
		t.Fatalf("nil Usage serialized: %s", withoutUsage)
	}
}

func TestRunResultInvocationJSONContract(t *testing.T) {
	result := schema.RunResult{RunID: "run-1", Invocations: []schema.ModelInvocation{{
		Sequence: 2,
		Phase: schema.ModelInvocationPhaseAction,
		Usage: schema.Usage{InputTokens: 10, OutputTokens: 4},
	}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"phase":"action"`) ||
		!strings.Contains(string(encoded), `"input_tokens":10`) {
		t.Fatalf("RunResult JSON = %s", encoded)
	}
}
```

Add `reflect` and `strings` imports to the test.

- [ ] **Step 2: Run schema tests and verify RED**

Run: `go test ./internal/schema -run 'Test(MessageUsage|RunResultInvocation)' -count=1`

Expected: compilation fails because Usage and ModelInvocation contracts do not exist.

- [ ] **Step 3: Write failing pricing validation tests**

Add focused cases:

```go
func TestLoadParsesRequiredPlatformPricing(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m",
			"pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Pricing == nil || current.Pricing.InputUSDPerMillionTokens != 0.15 ||
		current.Pricing.OutputUSDPerMillionTokens != 0.60 {
		t.Fatalf("Pricing = %#v", current.Pricing)
	}
}

func TestPlatformPricingValidation(t *testing.T) {
	tests := []struct {
		name    string
		pricing *PricingConfig
	}{
		{name: "missing"},
		{name: "negative input", pricing: &PricingConfig{InputUSDPerMillionTokens: -1}},
		{name: "negative output", pricing: &PricingConfig{OutputUSDPerMillionTokens: -1}},
		{name: "NaN input", pricing: &PricingConfig{InputUSDPerMillionTokens: math.NaN()}},
		{name: "infinite output", pricing: &PricingConfig{OutputUSDPerMillionTokens: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := PlatformConfig{
				ID: "x", Protocol: ProtocolOpenAI, BaseURL: "https://x.test/",
				APIKey: "k", Model: "m", Pricing: tt.pricing,
			}
			if err := platform.validate(0); err == nil || !strings.Contains(err.Error(), "pricing") {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
}

func TestPlatformPricingAllowsFreeModel(t *testing.T) {
	platform := PlatformConfig{
		ID: "x", Protocol: ProtocolOpenAI, BaseURL: "https://x.test/",
		APIKey: "k", Model: "m", Pricing: &PricingConfig{},
	}
	if err := platform.validate(0); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}
```

- [ ] **Step 4: Run config tests and verify RED**

Run: `go test ./internal/config -run 'Test(LoadParsesRequiredPlatformPricing|PlatformPricing)' -count=1`

Expected: compilation fails because PricingConfig and PlatformConfig.Pricing do not exist.

- [ ] **Step 5: Implement schema contracts**

Add to `message.go`:

```go
type Usage struct {
	InputTokens                      int64   `json:"input_tokens"`
	OutputTokens                     int64   `json:"output_tokens"`
	InputPriceUSDPerMillionTokens    float64 `json:"input_price_usd_per_million_tokens"`
	OutputPriceUSDPerMillionTokens   float64 `json:"output_price_usd_per_million_tokens"`
	CostUSD                          float64 `json:"cost_usd"`
	LatencyMS                        int64   `json:"latency_ms"`
	PlatformID                       string  `json:"platform_id"`
	Model                            string  `json:"model"`
}
```

Add this field to `Message`:

```go
Usage *Usage `json:"usage,omitempty"`
```

Add to `run.go`:

```go
type ModelInvocationPhase string

const (
	ModelInvocationPhaseThinking ModelInvocationPhase = "thinking"
	ModelInvocationPhaseAction   ModelInvocationPhase = "action"
)

type ModelInvocation struct {
	Sequence uint32               `json:"sequence"`
	Phase    ModelInvocationPhase `json:"phase"`
	Usage    Usage                `json:"usage"`
}
```

Extend RunResult with:

```go
Invocations []ModelInvocation `json:"invocations,omitempty"`
```

- [ ] **Step 6: Implement pricing config and validation**

Add:

```go
type PricingConfig struct {
	InputUSDPerMillionTokens  float64 `json:"input_usd_per_million_tokens" yaml:"input_usd_per_million_tokens" toml:"input_usd_per_million_tokens"`
	OutputUSDPerMillionTokens float64 `json:"output_usd_per_million_tokens" yaml:"output_usd_per_million_tokens" toml:"output_usd_per_million_tokens"`
}
```

Add `Pricing *PricingConfig` with matching `json/yaml/toml:"pricing"` tags to PlatformConfig. In `PlatformConfig.validate`, after model validation, call:

```go
func (p *PricingConfig) validate(prefix string) error {
	if p == nil {
		return fmt.Errorf("%s.pricing 不能为空", prefix)
	}
	if math.IsNaN(p.InputUSDPerMillionTokens) || math.IsInf(p.InputUSDPerMillionTokens, 0) ||
		p.InputUSDPerMillionTokens < 0 {
		return fmt.Errorf("%s.pricing.input_usd_per_million_tokens 必须是有限非负数", prefix)
	}
	if math.IsNaN(p.OutputUSDPerMillionTokens) || math.IsInf(p.OutputUSDPerMillionTokens, 0) ||
		p.OutputUSDPerMillionTokens < 0 {
		return fmt.Errorf("%s.pricing.output_usd_per_million_tokens 必须是有限非负数", prefix)
	}
	return nil
}
```

- [ ] **Step 7: Update all valid configuration fixtures explicitly**

Add this object to every platform fixture that is intended to pass validation:

```json
"pricing": {
  "input_usd_per_million_tokens": 0.15,
  "output_usd_per_million_tokens": 0.60
}
```

For YAML use:

```yaml
pricing:
  input_usd_per_million_tokens: 0.15
  output_usd_per_million_tokens: 0.60
```

For TOML use:

```toml
[platforms.pricing]
input_usd_per_million_tokens = 0.15
output_usd_per_million_tokens = 0.60
```

For programmatic configs use `Pricing: &config.PricingConfig{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60}`. Preserve intentionally invalid fixtures by adding valid pricing unless the test specifically targets missing pricing.

- [ ] **Step 8: Run schema/config and dependent fixture tests**

Run: `gofmt -w internal/schema/message.go internal/schema/run.go internal/schema/message_test.go internal/config/config.go internal/config/validate.go internal/config/config_test.go internal/config/register_test.go internal/provider/register_test.go tests/integration/fx_dependency_graph_test.go`

Run: `go test ./internal/schema ./internal/config ./internal/provider ./tests/integration -count=1`

Expected: PASS.

- [ ] **Step 9: Commit the contract**

```bash
git add internal/schema internal/config internal/provider/register_test.go tests/integration/fx_dependency_graph_test.go
git commit -m "feat: define model usage and pricing contracts"
```

---

### Task 2: Extract vendor Usage in both Provider adapters

**Files:**
- Modify: `internal/provider/openai_test.go`
- Modify: `internal/provider/openai.go`
- Modify: `internal/provider/claude_test.go`
- Modify: `internal/provider/claude.go`

**Interfaces:**
- Consumes: `schema.Usage` from Task 1.
- Produces: successful assistant responses with raw normalized input/output tokens when upstream Usage is present.
- Preserves: content/tool-call translation and omission of Usage from outbound history mapping.

- [ ] **Step 1: Write failing Provider Usage tests**

Change the existing response fixtures to distinct values and assert them after Generate:

```go
if result.Usage == nil || result.Usage.InputTokens != 123 || result.Usage.OutputTokens != 45 {
	t.Fatalf("Usage = %#v, want input=123 output=45", result.Usage)
}
```

Use `"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}` for OpenAI and `"usage":{"input_tokens":123,"output_tokens":45}` for Anthropic.

Add one omitted-Usage test per adapter:

```go
func TestOpenAICompatibleProviderLeavesUsageNilWhenOmitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-no-usage","object":"chat.completion","created":1,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]
		}`))
	}))
	defer server.Close()
	p := newOpenAICompatibleProvider("test-key", server.URL+"/", "test-model", "test")
	result, err := p.Generate(context.Background(), []schema.Message{{
		Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("hello")},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage != nil {
		t.Fatalf("Usage = %#v, want nil", result.Usage)
	}
}

func TestClaudeProviderLeavesUsageNilWhenOmitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-no-usage","type":"message","role":"assistant","model":"test-model",
			"content":[{"type":"text","text":"done"}],
			"stop_reason":"end_turn","stop_sequence":null
		}`))
	}))
	defer server.Close()
	p := newClaudeProvider("test-key", server.URL+"/", "test-model", "test")
	result, err := p.Generate(context.Background(), []schema.Message{{
		Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("hello")},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage != nil {
		t.Fatalf("Usage = %#v, want nil", result.Usage)
	}
}
```

- [ ] **Step 2: Run Provider tests and verify RED**

Run: `go test ./internal/provider -run 'Test(OpenAICompatibleProvider|ClaudeProvider).*(Usage|Translates)' -count=1`

Expected: Usage assertions fail because adapters leave `Message.Usage` nil.

- [ ] **Step 3: Implement OpenAI Usage presence mapping**

After creating the result and before returning it:

```go
if response.JSON.Usage.Valid() {
	result.Usage = &schema.Usage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
}
```

Do not use `> 0` checks; explicit zero-token Usage is present data.

- [ ] **Step 4: Implement Anthropic Usage presence mapping**

After creating the result:

```go
if response.JSON.Usage.Valid() {
	result.Usage = &schema.Usage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}
}
```

Do not add cache creation/read counts to InputTokens; the selected normalized contract maps the SDK's authoritative input/output fields directly.

- [ ] **Step 5: Verify both adapters**

Run: `gofmt -w internal/provider/openai.go internal/provider/openai_test.go internal/provider/claude.go internal/provider/claude_test.go`

Run: `go test ./internal/provider -count=1`

Expected: PASS.

- [ ] **Step 6: Commit Provider mapping**

```bash
git add internal/provider/openai.go internal/provider/openai_test.go internal/provider/claude.go internal/provider/claude_test.go
git commit -m "feat: capture provider token usage"
```

---

### Task 3: Add the cost and latency Provider decorator

**Files:**
- Create: `internal/observability/tracker.go`
- Create: `internal/observability/tracker_test.go`
- Create: `internal/observability/register.go`
- Create: `internal/observability/register_test.go`
- Modify: `internal/register.go`
- Modify: `internal/provider/register_test.go`

**Interfaces:**
- Consumes: `provider.LLMProvider`, current PlatformConfig, and raw `schema.Usage`.
- Produces: `NewCostTracker(provider.LLMProvider, string, string, Pricing) (*CostTracker, error)`.
- Produces: `observability.Register`, which uses Fx decoration and returns the same `provider.LLMProvider` contract.

- [ ] **Step 1: Write failing deterministic tracker tests**

Use a provider function and a deterministic two-value clock:

```go
type providerFunc func(context.Context, []schema.Message, []schema.ToolDefinition) (*schema.Message, error)

func (f providerFunc) Generate(ctx context.Context, messages []schema.Message, tools []schema.ToolDefinition) (*schema.Message, error) {
	return f(ctx, messages, tools)
}

func TestCostTrackerEnrichesUsageWithoutMutatingDelegateUsage(t *testing.T) {
	original := &schema.Message{Role: schema.RoleAssistant, Usage: &schema.Usage{
		InputTokens: 2_000_000, OutputTokens: 500_000,
	}}
	times := []time.Time{time.Unix(0, 0), time.Unix(0, 0).Add(2500 * time.Millisecond)}
	index := 0
	tracker, err := newCostTracker(
		providerFunc(func(context.Context, []schema.Message, []schema.ToolDefinition) (*schema.Message, error) {
			return original, nil
		}),
		"zhipu", "glm-4.5-air",
		Pricing{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		func() time.Time { value := times[index]; index++; return value },
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.CostUSD != 0.60 || result.Usage.LatencyMS != 2500 ||
		result.Usage.PlatformID != "zhipu" || result.Usage.Model != "glm-4.5-air" {
		t.Fatalf("Usage = %#v", result.Usage)
	}
	if original.Usage.CostUSD != 0 || original.Usage.LatencyMS != 0 {
		t.Fatalf("delegate Usage mutated: %#v", original.Usage)
	}
}
```

Add separate tests that require:

- free prices produce an exact zero cost;
- nil Usage passes through unchanged;
- negative token Usage is removed from the returned copy;
- delegate error and response are returned unchanged;
- nil delegate, blank platform/model, negative/NaN/infinite prices are rejected by construction.

- [ ] **Step 2: Run tracker tests and verify RED**

Run: `go test ./internal/observability -run TestCostTracker -count=1`

Expected: package or symbols do not exist.

- [ ] **Step 3: Implement immutable tracker construction**

Define:

```go
type Pricing struct {
	InputUSDPerMillionTokens  float64
	OutputUSDPerMillionTokens float64
}

type CostTracker struct {
	next       provider.LLMProvider
	platformID string
	model      string
	pricing    Pricing
	now        func() time.Time
}

func NewCostTracker(next provider.LLMProvider, platformID, model string, pricing Pricing) (*CostTracker, error) {
	return newCostTracker(next, platformID, model, pricing, time.Now)
}
```

`newCostTracker` trims identities, rejects nil/blank/invalid inputs, and stores immutable values. Add `var _ provider.LLMProvider = (*CostTracker)(nil)`.

- [ ] **Step 4: Implement Generate enrichment and structured logs**

Call `now` immediately before and after exactly one delegated call. Clamp a negative synthetic duration to zero. On success with non-negative Usage, copy both the message and Usage before enrichment:

```go
cost := (float64(usage.InputTokens)*t.pricing.InputUSDPerMillionTokens +
	float64(usage.OutputTokens)*t.pricing.OutputUSDPerMillionTokens) / 1_000_000
usage.InputPriceUSDPerMillionTokens = t.pricing.InputUSDPerMillionTokens
usage.OutputPriceUSDPerMillionTokens = t.pricing.OutputUSDPerMillionTokens
usage.CostUSD = cost
usage.LatencyMS = latency.Milliseconds()
usage.PlatformID = t.platformID
usage.Model = t.model
result := *response
result.Usage = &usage
```

Use `logsdk.Info/Warn/Error` with stable fields: `component=observability`, `code`, `platform_id`, `model`, `latency_ms`, token counts, both rates, and `cost_usd`. Codes are `model_call_completed`, `usage_missing`, `usage_invalid`, and `model_call_failed`. Never log messages, tool arguments, API keys, or full request payloads.

- [ ] **Step 5: Write failing Fx decoration test**

```go
func TestRegisterDecoratesConfiguredProvider(t *testing.T) {
	cfg := &config.Config{
		CurrentPlatform: "test",
		Platforms: []config.PlatformConfig{{
			ID: "test", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/",
			APIKey: "key", Model: "model",
			Pricing: &config.PricingConfig{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		}},
	}
	base := providerFunc(func(context.Context, []schema.Message, []schema.ToolDefinition) (*schema.Message, error) {
		return &schema.Message{Role: schema.RoleAssistant}, nil
	})
	var got provider.LLMProvider
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Supply(fx.Annotate(base, fx.As(new(provider.LLMProvider)))),
		Register,
		fx.Populate(&got),
	)
	app.RequireStart()
	defer app.RequireStop()
	if _, ok := got.(*CostTracker); !ok {
		t.Fatalf("provider = %T, want *CostTracker", got)
	}
}
```

- [ ] **Step 6: Implement Fx decoration without an import cycle**

In `register.go`:

```go
var Register = fx.Options(fx.Decorate(decorateLLMProvider))

func decorateLLMProvider(next provider.LLMProvider, cfg *config.Config) (provider.LLMProvider, error) {
	platform, err := cfg.Current()
	if err != nil {
		return nil, err
	}
	if platform.Pricing == nil {
		return nil, fmt.Errorf("初始化模型可观测性: 平台 %q 缺少 pricing", platform.ID)
	}
	tracker, err := NewCostTracker(next, platform.ID, platform.Model, Pricing{
		InputUSDPerMillionTokens: platform.Pricing.InputUSDPerMillionTokens,
		OutputUSDPerMillionTokens: platform.Pricing.OutputUSDPerMillionTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化模型可观测性: %w", err)
	}
	return tracker, nil
}
```

Register `observability.Register` immediately after `provider.Register` in `internal.Register`. Do not import observability from provider.

- [ ] **Step 7: Verify tracker and dependency graph**

Run: `gofmt -w internal/observability/tracker.go internal/observability/tracker_test.go internal/observability/register.go internal/observability/register_test.go internal/register.go internal/provider/register_test.go`

Run: `go test ./internal/observability ./internal/provider ./tests/integration -count=1`

Expected: PASS and no import cycle.

- [ ] **Step 8: Commit observability decoration**

```bash
git add internal/observability internal/register.go internal/provider/register_test.go
git commit -m "feat: track model cost and latency"
```

---

### Task 4: Return ordered Thinking and Action invocation records from Engine

**Files:**
- Create: `internal/engine/loop_result.go`
- Modify: `internal/engine/agent_loop.go`
- Modify: `internal/engine/runtime.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/run_messages_test.go`
- Modify: `internal/engine/runtime_test.go`

**Interfaces:**
- Consumes: enriched `Message.Usage` from Task 3.
- Produces: internal `LoopResult{NewMessages, Invocations}`.
- Produces: public `RunResult.Invocations`, including metered Thinking calls while excluding Thinking content.

- [ ] **Step 1: Write failing multi-call invocation test**

Extend the existing Thinking/tool-loop scenario with distinct Usage values on all four fake responses, then assert:

```go
result, err := loop.Run(context.Background(), runContext, reporter)
if err != nil {
	t.Fatal(err)
}
if len(result.Invocations) != 4 {
	t.Fatalf("Invocations = %#v", result.Invocations)
}
wantPhases := []schema.ModelInvocationPhase{
	schema.ModelInvocationPhaseThinking,
	schema.ModelInvocationPhaseAction,
	schema.ModelInvocationPhaseThinking,
	schema.ModelInvocationPhaseAction,
}
for index, invocation := range result.Invocations {
	if invocation.Sequence != uint32(index+1) || invocation.Phase != wantPhases[index] {
		t.Fatalf("Invocations[%d] = %#v", index, invocation)
	}
}
if len(result.NewMessages) != 3 {
	t.Fatalf("NewMessages = %#v; hidden Thinking content must stay excluded", result.NewMessages)
}
```

Add a failure test where call 1 succeeds with Usage and call 2 errors; returned Invocations must retain call 1. Add a missing-Usage call between metered calls and assert sequences `1` and `3`, proving call ordinals can contain gaps.

- [ ] **Step 2: Run Engine tests and verify RED**

Run: `go test ./internal/engine -run 'TestAgentLoop.*Invocation' -count=1`

Expected: AgentLoop still returns `[]schema.Message` and exposes no invocation data.

- [ ] **Step 3: Introduce focused LoopResult**

Create:

```go
type LoopResult struct {
	NewMessages []schema.Message
	Invocations []schema.ModelInvocation
}
```

Change `AgentLoop.Run` and the private `agentLoopRunner` interface to return `(LoopResult, error)`. Change `finish` to clone both slices so partial results survive an error.

- [ ] **Step 4: Record explicit phase and provider-call ordinal**

Maintain `callSequence uint32`. Increment it immediately before every `Generate`, including both Thinking and Action. After a non-nil response passes phase-specific validation, copy its Usage into:

```go
func appendInvocation(
	destination []schema.ModelInvocation,
	sequence uint32,
	phase schema.ModelInvocationPhase,
	usage *schema.Usage,
) []schema.ModelInvocation {
	if usage == nil {
		return destination
	}
	return append(destination, schema.ModelInvocation{
		Sequence: sequence,
		Phase: phase,
		Usage: *usage,
	})
}
```

Call it with `ModelInvocationPhaseThinking` and `ModelInvocationPhaseAction` at their explicit call sites. Do not infer phase from available tools.

- [ ] **Step 5: Map LoopResult to RunResult**

In runtime:

```go
loopResult, err := r.loop.Run(ctx, runContext, reporter)
result.NewMessages = append([]schema.Message(nil), loopResult.NewMessages...)
result.Invocations = append([]schema.ModelInvocation(nil), loopResult.Invocations...)
return result, err
```

Update runtime fakes to return LoopResult and assert both slices are copied into RunResult.

- [ ] **Step 6: Update existing Engine tests for the return shape**

Replace direct `newMessages` variables with `loopResult.NewMessages`; preserve every existing content, tool ordering, cancellation, Reporter, and validation assertion. Do not weaken tests merely to satisfy the signature change.

- [ ] **Step 7: Verify Engine behavior**

Run: `gofmt -w internal/engine/loop_result.go internal/engine/agent_loop.go internal/engine/runtime.go internal/engine/loop_test.go internal/engine/run_messages_test.go internal/engine/runtime_test.go`

Run: `go test ./internal/engine -count=1`

Expected: PASS, including hidden Thinking content and partial-result regressions.

- [ ] **Step 8: Commit Engine invocation flow**

```bash
git add internal/engine
git commit -m "feat: expose ordered model invocations"
```

---

### Task 5: Carry Usage and invocations through the conversation boundary

**Files:**
- Modify: `internal/conversation/store.go`
- Modify: `internal/conversation/runner.go`
- Modify: `internal/conversation/runner_test.go`
- Modify: `internal/conversation/mysql/codec_test.go`

**Interfaces:**
- Consumes: `schema.RunResult.Invocations`.
- Produces: `conversation.AppendRequest.Invocations []schema.ModelInvocation`.
- Preserves: no append for a failed run with zero completed messages.

- [ ] **Step 1: Write failing conversation forwarding test**

Have the runtime fake return one Action message with Usage and two invocation records. Assert the captured AppendRequest includes customer input, Action output, and both invocations in order:

```go
if !reflect.DeepEqual(store.request.Invocations, runtimeResult.Invocations) {
	t.Fatalf("Invocations = %#v, want %#v", store.request.Invocations, runtimeResult.Invocations)
}
if store.request.Messages[1].Usage == runtimeResult.NewMessages[0].Usage {
	t.Fatal("persisted Message Usage aliases runtime result")
}
```

Mutate the runtime result after Run and assert the captured Message Usage and invocation values do not change.

- [ ] **Step 2: Run conversation tests and verify RED**

Run: `go test ./internal/conversation -run 'TestRunner.*(Usage|Invocation)' -count=1`

Expected: AppendRequest has no Invocations and cloneMessage aliases Usage.

- [ ] **Step 3: Extend and deep-clone conversation contracts**

Add:

```go
type AppendRequest struct {
	ConversationPK  uint64
	ExpectedVersion uint64
	RunID           string
	Messages        []schema.Message
	Invocations     []schema.ModelInvocation
}
```

In `cloneMessage`:

```go
if message.Usage != nil {
	usage := *message.Usage
	cloned.Usage = &usage
}
```

Add `cloneInvocations` that copies the slice, and pass it into AppendRequest from `runtimeResult.Invocations`.

- [ ] **Step 4: Prove visible Usage survives the existing JSON codec**

Add populated Usage to the assistant fixture in `TestMessageCodecRoundTripsSupportedMessages`. `reflect.DeepEqual` must continue to pass without codec production changes.

- [ ] **Step 5: Verify conversation boundary and codec**

Run: `gofmt -w internal/conversation/store.go internal/conversation/runner.go internal/conversation/runner_test.go internal/conversation/mysql/codec_test.go`

Run: `go test ./internal/conversation ./internal/conversation/mysql -run 'Test(Runner|MessageCodec)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit forwarding behavior**

```bash
git add internal/conversation/store.go internal/conversation/runner.go internal/conversation/runner_test.go internal/conversation/mysql/codec_test.go
git commit -m "feat: carry usage through conversations"
```

---

### Task 6: Define and validate the durable invocation ledger

**Files:**
- Create: `migrations/0002_model_invocation_observability.up.sql`
- Create: `migrations/0002_model_invocation_observability.down.sql`
- Modify: `internal/conversation/mysql/migration_test.go`
- Modify: `internal/conversation/mysql/model.go`
- Create: `internal/conversation/mysql/invocation.go`
- Create: `internal/conversation/mysql/invocation_test.go`

**Interfaces:**
- Consumes: `schema.ModelInvocation` and persistence-assigned conversation/turn identifiers.
- Produces: `invocationRow` and `encodeInvocation` with fixed-scale decimal strings.

- [ ] **Step 1: Write failing migration contract test**

```go
func TestModelInvocationMigrationDefinesBillingLedger(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/0002_model_invocation_observability.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, want := range []string{
		"agent_model_invocations", "turn_version", "sequence", "phase",
		"input_tokens", "output_tokens", "DECIMAL(20,12)", "cost_usd",
		"uq_agent_model_invocations_order", "fk_agent_model_invocations_conversation",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	down, err := os.ReadFile("../../../migrations/0002_model_invocation_observability.down.sql")
	if err != nil || !strings.Contains(string(down), "DROP TABLE IF EXISTS agent_model_invocations") {
		t.Fatalf("down migration = %q, error = %v", down, err)
	}
}
```

- [ ] **Step 2: Run migration test and verify RED**

Run: `go test ./internal/conversation/mysql -run TestModelInvocationMigration -count=1`

Expected: migration file not found.

- [ ] **Step 3: Add exact up/down migration**

Use:

```sql
CREATE TABLE IF NOT EXISTS agent_model_invocations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_pk BIGINT UNSIGNED NOT NULL,
    turn_version BIGINT UNSIGNED NOT NULL,
    run_id VARCHAR(128) NULL,
    sequence INT UNSIGNED NOT NULL,
    phase VARCHAR(16) NOT NULL,
    platform_id VARCHAR(128) NOT NULL,
    model VARCHAR(255) NOT NULL,
    input_tokens BIGINT UNSIGNED NOT NULL,
    output_tokens BIGINT UNSIGNED NOT NULL,
    input_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL,
    output_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL,
    cost_usd DECIMAL(20,12) NOT NULL,
    latency_ms BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_model_invocations_order (conversation_pk, turn_version, sequence),
    KEY idx_agent_model_invocations_conversation_created (conversation_pk, created_at),
    CONSTRAINT fk_agent_model_invocations_conversation FOREIGN KEY (conversation_pk)
        REFERENCES agent_conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

The down migration contains only `DROP TABLE IF EXISTS agent_model_invocations;`.

- [ ] **Step 4: Write failing invocation encoding tests**

Require exact row values:

```go
func TestEncodeInvocationProducesFixedScaleLedgerRow(t *testing.T) {
	runID := "run-8"
	row, err := encodeInvocation(schema.ModelInvocation{
		Sequence: 2,
		Phase: schema.ModelInvocationPhaseThinking,
		Usage: schema.Usage{
			InputTokens: 120, OutputTokens: 30,
			InputPriceUSDPerMillionTokens: 0.15,
			OutputPriceUSDPerMillionTokens: 0.60,
			CostUSD: 0.000036, LatencyMS: 245,
			PlatformID: "zhipu", Model: "glm-4.5-air",
		},
	}, 11, 8, &runID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ConversationPK != 11 || row.TurnVersion != 8 || row.Sequence != 2 ||
		row.InputPriceUSDPerMillionTokens != "0.150000000000" ||
		row.OutputPriceUSDPerMillionTokens != "0.600000000000" ||
		row.CostUSD != "0.000036000000" {
		t.Fatalf("row = %#v", row)
	}
}
```

Add table cases for zero sequence, unknown phase, negative counts, negative latency, blank platform/model, negative/NaN/infinite price or cost. Every case must return an error before SQL.

- [ ] **Step 5: Run encoding tests and verify RED**

Run: `go test ./internal/conversation/mysql -run TestEncodeInvocation -count=1`

Expected: invocationRow and encodeInvocation do not exist.

- [ ] **Step 6: Implement row and encoder**

Define `invocationRow` with unsigned integer fields and string fields for the three DECIMAL columns. Implement `TableName() string { return "agent_model_invocations" }`.

`encodeInvocation` validates all invariants, stores trimmed platform/model values, converts non-negative int64 values to uint64, and formats decimals with:

```go
func decimal12(value float64) string {
	return strconv.FormatFloat(value, 'f', 12, 64)
}
```

Reject `math.IsNaN`, `math.IsInf`, and negative values before calling decimal12.

- [ ] **Step 7: Verify migration and encoding**

Run: `gofmt -w internal/conversation/mysql/model.go internal/conversation/mysql/invocation.go internal/conversation/mysql/invocation_test.go internal/conversation/mysql/migration_test.go`

Run: `go test ./internal/conversation/mysql -run 'Test(ModelInvocationMigration|EncodeInvocation)' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit ledger schema and encoder**

```bash
git add migrations/0002_model_invocation_observability.up.sql migrations/0002_model_invocation_observability.down.sql internal/conversation/mysql/model.go internal/conversation/mysql/invocation.go internal/conversation/mysql/invocation_test.go internal/conversation/mysql/migration_test.go
git commit -m "feat: define model invocation ledger"
```

---

### Task 7: Atomically persist messages and invocation rows

**Files:**
- Modify: `internal/conversation/mysql/store.go`
- Modify: `internal/conversation/mysql/store_test.go`

**Interfaces:**
- Consumes: `AppendRequest.Invocations` and `encodeInvocation`.
- Produces: one transaction containing version update, message insert, and optional invocation insert.
- Preserves: message-only callers and optimistic conflict behavior.

- [ ] **Step 1: Write failing successful transaction test**

Extend `validAppendRequest()` with one Action invocation. After the expected message insert, require:

```go
mock.ExpectExec("INSERT INTO .*agent_model_invocations").
	WithArgs(
		11, 8, "run-8", 1, "action", "test-platform", "test-model",
		120, 30, "0.150000000000", "0.600000000000", "0.000036000000", 245,
		sqlmock.AnyArg(),
	).
	WillReturnResult(sqlmock.NewResult(1, 1))
```

Then expect commit. Keep a separate message-only test that expects no invocation SQL.

- [ ] **Step 2: Write failing rollback and preflight validation tests**

Add a test where the invocation insert returns `insert invocations failed`; expect rollback and `errors.Is` to preserve the insert error. Add invalid invocation cases to `TestStoreAppendTurnRejectsInvalidRequestBeforeTransaction` and assert no SQL expectations were consumed.

- [ ] **Step 3: Run store tests and verify RED**

Run: `go test ./internal/conversation/mysql -run 'TestStoreAppendTurn' -count=1`

Expected: no invocation insert occurs and invalid invocations are accepted.

- [ ] **Step 4: Encode all rows before starting the transaction**

After calculating `turnVersion` and `runID`, build invocation rows:

```go
invocationRows := make([]invocationRow, len(request.Invocations))
for index := range request.Invocations {
	row, err := encodeInvocation(request.Invocations[index], request.ConversationPK, turnVersion, runID)
	if err != nil {
		return fmt.Errorf("mysql conversation: encode invocation %d: %w", index, err)
	}
	invocationRows[index] = row
}
```

Also reject invocation sequences that are not strictly increasing. Gaps are valid; duplicates and descending order are not.

- [ ] **Step 5: Insert invocation rows inside the existing transaction**

After message insertion:

```go
if len(invocationRows) > 0 {
	if err := db.Create(&invocationRows).Error; err != nil {
		return err
	}
}
```

Do not start a second transaction and do not update cached totals on `agent_conversations`.

- [ ] **Step 6: Verify persistence**

Run: `gofmt -w internal/conversation/mysql/store.go internal/conversation/mysql/store_test.go`

Run: `go test ./internal/conversation/mysql -count=1`

Expected: PASS; existing environment-gated integration tests may explicitly SKIP under their existing guard.

- [ ] **Step 7: Commit atomic persistence**

```bash
git add internal/conversation/mysql/store.go internal/conversation/mysql/store_test.go
git commit -m "feat: persist model invocation metrics"
```

---

### Task 8: Document configuration and verify the complete feature

**Files:**
- Modify: `config.example.json`
- Modify: `README.md`
- Modify: `docs/conversation-persistence.md`
- Modify: `docs/superpowers/specs/2026-08-04-model-cost-observability-design.md` only for the already identified Fx decoration wording
- Test: all packages under `./...`

**Interfaces:**
- Documents: required pricing configuration, metric semantics, privacy boundary, migration order, and authoritative aggregation queries.
- Verifies: complete repository graph and all existing behavior.

- [ ] **Step 1: Update example configuration**

Add `pricing` to every platform in `config.example.json`, with explicit USD-per-million values. Use example values only when clearly labeled; do not call them official/current prices.

- [ ] **Step 2: Document runtime and persistence behavior**

Add a concise README section covering:

```text
pricing.input_usd_per_million_tokens
pricing.output_usd_per_million_tokens
```

State that logs and RunResult expose per-call Usage, while durable aggregation uses `agent_model_invocations`. In `docs/conversation-persistence.md`, document applying migration 0002 after 0001 and include:

```sql
SELECT
    SUM(input_tokens) AS input_tokens,
    SUM(output_tokens) AS output_tokens,
    SUM(cost_usd) AS cost_usd,
    SUM(latency_ms) AS latency_ms
FROM agent_model_invocations
WHERE conversation_pk = ?;
```

Explicitly state that hidden Thinking text and complete provider requests are not stored.

- [ ] **Step 3: Run targeted feature tests fresh**

Run: `go test ./internal/schema ./internal/config ./internal/provider ./internal/observability ./internal/engine ./internal/conversation ./internal/conversation/mysql -count=1`

Expected: PASS with zero failures.

- [ ] **Step 4: Run full repository verification**

Run:

```bash
gofmt -w internal/schema/message.go internal/schema/run.go internal/schema/message_test.go internal/config/config.go internal/config/validate.go internal/config/config_test.go internal/config/register_test.go internal/provider/register_test.go internal/provider/openai.go internal/provider/openai_test.go internal/provider/claude.go internal/provider/claude_test.go internal/observability/tracker.go internal/observability/tracker_test.go internal/observability/register.go internal/observability/register_test.go internal/register.go internal/engine/loop_result.go internal/engine/agent_loop.go internal/engine/runtime.go internal/engine/loop_test.go internal/engine/run_messages_test.go internal/engine/runtime_test.go internal/conversation/store.go internal/conversation/runner.go internal/conversation/runner_test.go internal/conversation/mysql/codec_test.go internal/conversation/mysql/model.go internal/conversation/mysql/invocation.go internal/conversation/mysql/invocation_test.go internal/conversation/mysql/migration_test.go internal/conversation/mysql/store.go internal/conversation/mysql/store_test.go tests/integration/fx_dependency_graph_test.go
```

Run: `go test ./... -count=1`

Run: `go vet ./...`

Run: `git diff --check`

Expected: all commands exit 0. If MySQL integration tests are environment-gated, report their explicit skip output rather than claiming live-database verification.

- [ ] **Step 5: Review requirements against the design**

Confirm from the final diff and tests:

- both provider protocols populate raw input/output tokens;
- tracker calculates USD using the selected platform's configured rates;
- visible Action Message Usage is serializable and persisted;
- every successfully metered Thinking/Action call appears once in RunResult invocation order;
- hidden Thinking content and full provider requests have no persistence path;
- message and invocation writes share one transaction;
- ledger totals cannot double-count message Usage;
- all pre-existing user changes remain present and were not overwritten.

- [ ] **Step 6: Commit documentation and final fixture updates**

```bash
git add config.example.json README.md docs/conversation-persistence.md docs/superpowers/specs/2026-08-04-model-cost-observability-design.md
git commit -m "docs: explain model cost observability"
```
