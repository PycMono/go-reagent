# Pi Error Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add bounded model retries, one-shot context compaction after a structured overflow error, and model-only tool recovery hints without changing the raw Reporter or persistence trail.

**Architecture:** Provider adapters normalize official SDK errors into the existing Pi `ErrorCode`; `Loop` decides retry, compact, or terminate. Harness compaction selects a bounded old-history slice, while ToolRuntime preserves raw structured tool failures and Loop derives a separate model-context message with a recovery hint.

**Tech Stack:** Go 1.26, OpenAI Go SDK v3.47.0, Anthropic Go SDK v1.61.0, existing Pi Core/Harness packages, standard `errors`, `net`, `io`, `context`, and `testing` packages.

**Spec:** `docs/superpowers/specs/2026-08-14-error-recovery-design.md`

## Global Constraints

- Use `pi/harness/errors.ErrorCode` for both AI and Tool recovery categories; do not add `GenerationFailure` or `ToolErrorCode`.
- Do not use fuzzy error-message matching to classify Provider or Tool failures.
- Retry only `ai_transient` and `ai_rate_limited`, with exactly two retries after 500 ms and 1 s.
- Do not add Recovery configuration, model capacity metadata, proactive compaction, recursive compaction, or Session Resume.
- Compact only after `ai_context_overflow`; perform at most one summary and one retried original request.
- Limit serialized summary history to 32 KiB, preserving leading System messages and the current User turn.
- Never split an Assistant ToolCall from its Tool Result.
- Keep Reporter events and `RunResult.NewMessages` raw; inject Tool Recovery Hint only into the next Provider context.
- Do not automatically replay a failed tool.
- Preserve concrete causes through `errors.Is` and `errors.As`.
- Do not add a compatibility layer for removed `GenerationError`, `WrapGeneration`, or `ErrGeneration` APIs.
- Use TDD for every behavioral change and commit each task independently.

---

## File Map

### Create

- `pi/ai/providers/error.go`: normalized Provider error facts and the shared HTTP/network-to-`ErrorCode` classifier.
- `pi/ai/providers/error_test.go`: white-box Provider classifier tests against both official SDK error types.
- `pi/recovery.go`: Loop retry, one-shot compaction orchestration, summary prompt, and model-only Tool Hint construction.
- `pi/harness/compaction.go`: pure construction and application of bounded compaction plans.
- `pi/test/recovery_test.go`: public Loop/Agent recovery behavior and persistence-boundary tests.

### Modify

- `pi/harness/errors/errors.go`: add stable AI/Tool codes, remove generation-specific wrappers, and classify standard Tool errors.
- `pi/harness/errors/errors_test.go`: stable value, cause, and Tool classification tests.
- `pi/ai/providers/openai.go`: disable SDK retries and normalize OpenAI API failures.
- `pi/ai/providers/anthropic.go`: disable SDK retries, normalize Anthropic API failures, and detect the structured context-window stop reason.
- `pi/harness/observability/tracker.go`: use the unified `ErrorCodeAIGeneration` wrapper for metering-contract failures.
- `pi/harness/observability/tracker_test.go`: assert unified ErrorCode rather than `ErrGeneration` identity.
- `pi/loop.go`: call recovery-aware generation, record Compaction invocations in order, and separate raw Tool messages from model messages.
- `pi/contract.go`: add `ModelInvocationPhaseCompaction`.
- `pi/event.go`: carry Tool ErrorCode on `ToolResult`.
- `pi/tool_runtime.go`: extract Tool ErrorCode before normalizing error text.
- `pi/middleware.go`: classify argument failures and rename panic-only recovery.
- `pi/harness/tools/edit.go`: return stable codes for no-match and non-unique edits.
- `pi/harness/tools/edit_test.go`: verify edit error codes without matching localized text.
- `pi/test/loop_test.go`: update generation assertions and add model-only Tool Hint coverage.
- `pi/test/agent_test.go`: verify Compaction invocation order and partial results.
- `pi/test/middleware_test.go`: verify `panic_recovery` and `tool_panic`.
- `pi/test/tool_runtime_test.go`: verify argument, filesystem, and general runtime codes.
- `pi/test/tool_runtime_public_test.go`: cover the public JSON shape of ToolResult ErrorCode.
- `docs/sdk-architecture.md`: document recovery decisions, raw/enhanced message split, and Compaction Usage.

---

### Task 1: Consolidate Pi failures on ErrorCode

**Files:**
- Modify: `pi/harness/errors/errors.go`
- Modify: `pi/harness/errors/errors_test.go`
- Modify: `pi/ai/providers/openai.go`
- Modify: `pi/ai/providers/anthropic.go`
- Modify: `pi/harness/observability/tracker.go`
- Modify: `pi/harness/observability/tracker_test.go`
- Modify: `pi/loop.go`
- Modify: `pi/test/loop_test.go`
- Modify: `pi/test/agent_test.go`

**Interfaces:**
- Produces: `ErrorCodeAITransient`, `ErrorCodeAIRateLimited`, `ErrorCodeAIContextOverflow`, `ErrorCodeAIUnauthorized`, `ErrorCodeAIQuotaExceeded`, `ErrorCodeAIInvalidRequest`, `ErrorCodeToolInvalidArguments`, `ErrorCodeToolResourceNotFound`, `ErrorCodeToolPermissionDenied`, `ErrorCodeToolEditNoMatch`, `ErrorCodeToolEditNotUnique`, `ErrorCodeToolTimeout`, and `ErrorCodeToolPanic`.
- Removes: `ErrGeneration`, `GenerationError`, and `WrapGeneration`.
- Preserves: `Wrap(code ErrorCode, op string, err error) error`, `ErrorCodeOf(error) ErrorCode`, and concrete cause unwrapping.

- [ ] **Step 1: Extend the stable-code test and replace generation sentinel assertions**

Update `TestErrorCodeValuesAreStable` with these exact mappings:

```go
ErrorCodeAITransient:             "ai_transient",
ErrorCodeAIRateLimited:           "ai_rate_limited",
ErrorCodeAIContextOverflow:       "ai_context_overflow",
ErrorCodeAIUnauthorized:          "ai_unauthorized",
ErrorCodeAIQuotaExceeded:         "ai_quota_exceeded",
ErrorCodeAIInvalidRequest:        "ai_invalid_request",
ErrorCodeToolInvalidArguments:    "tool_invalid_arguments",
ErrorCodeToolResourceNotFound:    "tool_resource_not_found",
ErrorCodeToolPermissionDenied:    "tool_permission_denied",
ErrorCodeToolEditNoMatch:         "tool_edit_no_match",
ErrorCodeToolEditNotUnique:       "tool_edit_not_unique",
ErrorCodeToolTimeout:             "tool_timeout",
ErrorCodeToolPanic:               "tool_panic",
```

Replace the generation-wrapper identity test with direct code/cause preservation:

```go
func TestClassifyPreservesSpecificCodeAndCause(t *testing.T) {
	cause := stderrors.New("provider failed")
	err := Classify("Run", Wrap(ErrorCodeAITransient, "action", cause))
	if ErrorCodeOf(err) != ErrorCodeAITransient || !stderrors.Is(err, cause) {
		t.Fatalf("classified error = %v", err)
	}
	if ErrorCodeOf(context.Canceled) != ErrorCodeCanceled {
		t.Fatalf("canceled code = %q", ErrorCodeOf(context.Canceled))
	}
}
```

- [ ] **Step 2: Run the errors package test and verify it fails**

Run:

```bash
go test ./pi/harness/errors -count=1
```

Expected: compilation fails because the new ErrorCode constants do not exist.

- [ ] **Step 3: Add unified codes and remove generation-specific wrappers**

Add the constants exactly as listed in Step 1. Remove:

```go
ErrGeneration
type GenerationError struct
func (*GenerationError) Error
func (*GenerationError) Unwrap
func (*GenerationError) Is
func WrapGeneration
```

Remove the `ErrGeneration` branch from `Classify`; classified `*Error` values already survive because `Wrap` returns an existing classified error unchanged.

- [ ] **Step 4: Replace all WrapGeneration and ErrGeneration use sites**

Use the unified wrapper for model conversion, response-contract, usage, and cost failures:

```go
pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai generate", err)
pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic generate", err)
pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model cost tracking", err)
pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", err)
pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", err)
```

In tests, replace:

```go
errors.Is(err, pierrors.ErrGeneration)
```

with:

```go
pierrors.ErrorCodeOf(err) == pierrors.ErrorCodeAIGeneration
```

- [ ] **Step 5: Run focused Pi errors and metering tests**

Run:

```bash
go test ./pi/harness/errors ./pi/harness/observability ./pi/test -count=1
```

Expected: PASS, with no references to `ErrGeneration`, `GenerationError`, or `WrapGeneration`.

- [ ] **Step 6: Commit unified error codes**

```bash
git add pi/harness/errors/errors.go pi/harness/errors/errors_test.go pi/ai/providers/openai.go pi/ai/providers/anthropic.go pi/harness/observability/tracker.go pi/harness/observability/tracker_test.go pi/loop.go pi/test/loop_test.go pi/test/agent_test.go
git commit -m "refactor: unify pi recovery error codes"
```

---

### Task 2: Normalize and classify official Provider errors

**Files:**
- Create: `pi/ai/providers/error.go`
- Create: `pi/ai/providers/error_test.go`
- Modify: `pi/ai/providers/openai.go`
- Modify: `pi/ai/providers/anthropic.go`

**Interfaces:**
- Produces: private `providerErrorInfo`, package function `classifyError(providerErrorInfo) error`, and same-named receiver methods `(*OpenAIImpl).classifyError(error) error` and `(*AnthropicImpl).classifyError(error) error`.
- Consumes: unified `pierrors.Wrap` and AI ErrorCodes from Task 1.

- [ ] **Step 1: Write table tests for the shared classifier**

Create `pi/ai/providers/error_test.go` in package `providers`. Cover these exact facts:

```go
tests := []struct {
	name string
	info providerErrorInfo
	want pierrors.ErrorCode
}{
	{name: "context overflow", info: providerErrorInfo{contextOverflow: true, err: errors.New("overflow")}, want: pierrors.ErrorCodeAIContextOverflow},
	{name: "quota", info: providerErrorInfo{quotaExceeded: true, err: errors.New("quota")}, want: pierrors.ErrorCodeAIQuotaExceeded},
	{name: "rate limit", info: providerErrorInfo{statusCode: http.StatusTooManyRequests, err: errors.New("429")}, want: pierrors.ErrorCodeAIRateLimited},
	{name: "request timeout", info: providerErrorInfo{statusCode: http.StatusRequestTimeout, err: errors.New("408")}, want: pierrors.ErrorCodeAITransient},
	{name: "conflict", info: providerErrorInfo{statusCode: http.StatusConflict, err: errors.New("409")}, want: pierrors.ErrorCodeAITransient},
	{name: "server", info: providerErrorInfo{statusCode: http.StatusBadGateway, err: errors.New("502")}, want: pierrors.ErrorCodeAITransient},
	{name: "unauthorized", info: providerErrorInfo{statusCode: http.StatusUnauthorized, err: errors.New("401")}, want: pierrors.ErrorCodeAIUnauthorized},
	{name: "forbidden", info: providerErrorInfo{statusCode: http.StatusForbidden, err: errors.New("403")}, want: pierrors.ErrorCodeAIUnauthorized},
	{name: "bad request", info: providerErrorInfo{statusCode: http.StatusBadRequest, err: errors.New("400")}, want: pierrors.ErrorCodeAIInvalidRequest},
	{name: "unknown", info: providerErrorInfo{err: errors.New("unknown")}, want: pierrors.ErrorCodeAIGeneration},
}
```

Add cancellation, deadline, DNS, EOF, and cause preservation assertions:

```go
if got := pierrors.ErrorCodeOf(classifyError(providerErrorInfo{err: context.Canceled})); got != pierrors.ErrorCodeCanceled {
	t.Fatalf("canceled code = %q", got)
}
if got := pierrors.ErrorCodeOf(classifyError(providerErrorInfo{err: context.DeadlineExceeded})); got != pierrors.ErrorCodeDeadlineExceeded {
	t.Fatalf("deadline code = %q", got)
}
if got := pierrors.ErrorCodeOf(classifyError(providerErrorInfo{err: &net.DNSError{IsTimeout: true}})); got != pierrors.ErrorCodeAITransient {
	t.Fatalf("DNS code = %q", got)
}
if got := pierrors.ErrorCodeOf(classifyError(providerErrorInfo{err: io.ErrUnexpectedEOF})); got != pierrors.ErrorCodeAITransient {
	t.Fatalf("EOF code = %q", got)
}
```

- [ ] **Step 2: Write official SDK extraction tests**

Construct an exported OpenAI SDK error with real request/response values and assert receiver classification:

```go
request, _ := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", nil)
apiErr := &openaisdk.Error{
	Code:       "context_length_exceeded",
	StatusCode: http.StatusBadRequest,
	Request:    request,
	Response:   &http.Response{StatusCode: http.StatusBadRequest},
}
if got := pierrors.ErrorCodeOf((&OpenAIImpl{}).classifyError(apiErr)); got != pierrors.ErrorCodeAIContextOverflow {
	t.Fatalf("code = %q", got)
}
```

Unmarshal the Anthropic standard error envelope so its private error type is populated, then assert billing classification:

```go
var apiErr anthropicsdk.Error
if err := json.Unmarshal([]byte(`{"error":{"type":"billing_error","message":"billing"}}`), &apiErr); err != nil {
	t.Fatal(err)
}
apiErr.StatusCode = http.StatusBadRequest
apiErr.Request = request
apiErr.Response = &http.Response{StatusCode: http.StatusBadRequest}
if got := pierrors.ErrorCodeOf((&AnthropicImpl{}).classifyError(&apiErr)); got != pierrors.ErrorCodeAIQuotaExceeded {
	t.Fatalf("code = %q", got)
}
```

- [ ] **Step 3: Run Provider tests and verify they fail**

Run:

```bash
go test ./pi/ai/providers -count=1
```

Expected: compilation fails because `providerErrorInfo` and the classifier methods do not exist.

- [ ] **Step 4: Implement the shared classifier**

Create `pi/ai/providers/error.go` with:

```go
type providerErrorInfo struct {
	statusCode      int
	providerCode    string
	contextOverflow bool
	quotaExceeded   bool
	err             error
}

func classifyError(info providerErrorInfo) error {
	code := pierrors.ErrorCodeAIGeneration
	switch {
	case errors.Is(info.err, context.Canceled):
		code = pierrors.ErrorCodeCanceled
	case errors.Is(info.err, context.DeadlineExceeded):
		code = pierrors.ErrorCodeDeadlineExceeded
	case info.contextOverflow:
		code = pierrors.ErrorCodeAIContextOverflow
	case info.quotaExceeded:
		code = pierrors.ErrorCodeAIQuotaExceeded
	case info.statusCode == http.StatusTooManyRequests:
		code = pierrors.ErrorCodeAIRateLimited
	case info.statusCode == http.StatusRequestTimeout,
		info.statusCode == http.StatusConflict,
		info.statusCode >= http.StatusInternalServerError:
		code = pierrors.ErrorCodeAITransient
	case isTransientProviderError(info.err):
		code = pierrors.ErrorCodeAITransient
	case info.statusCode == http.StatusUnauthorized,
		info.statusCode == http.StatusForbidden:
		code = pierrors.ErrorCodeAIUnauthorized
	case info.statusCode == http.StatusBadRequest:
		code = pierrors.ErrorCodeAIInvalidRequest
	}
	return pierrors.Wrap(code, "provider generate", info.err)
}
```

Implement `isTransientProviderError` without inspecting `err.Error()`:

```go
func isTransientProviderError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
```

- [ ] **Step 5: Implement OpenAI extraction and disable built-in retries**

Add `option.WithMaxRetries(0)` in `NewOpenAi`.

Add:

```go
func (p *OpenAIImpl) classifyError(err error) error {
	info := providerErrorInfo{err: err}
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) {
		info.statusCode = apiErr.StatusCode
		info.providerCode = apiErr.Code
		info.contextOverflow = apiErr.Code == "context_length_exceeded"
		info.quotaExceeded = apiErr.Code == "insufficient_quota"
	}
	return classifyError(info)
}
```

Replace only the API-request error branch with `return nil, p.classifyError(err)`. Message/tool conversion and empty-response errors remain `ErrorCodeAIGeneration` contract failures.

- [ ] **Step 6: Implement Anthropic extraction and structured overflow detection**

Add `option.WithMaxRetries(0)` in `NewAnthropic`.

Add:

```go
func (p *AnthropicImpl) classifyError(err error) error {
	info := providerErrorInfo{err: err}
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		info.statusCode = apiErr.StatusCode
		info.providerCode = string(apiErr.Type())
		switch apiErr.Type() {
		case anthropicsdk.ErrorTypeBillingError:
			info.quotaExceeded = true
		case anthropicsdk.ErrorTypeRateLimitError:
			info.statusCode = http.StatusTooManyRequests
		case anthropicsdk.ErrorTypeTimeoutError:
			info.statusCode = http.StatusRequestTimeout
		case anthropicsdk.ErrorTypeOverloadedError,
			anthropicsdk.ErrorTypeAPIError:
			info.statusCode = http.StatusInternalServerError
		case anthropicsdk.ErrorTypeAuthenticationError:
			info.statusCode = http.StatusUnauthorized
		case anthropicsdk.ErrorTypePermissionError:
			info.statusCode = http.StatusForbidden
		}
	}
	return classifyError(info)
}
```

Before converting a successful response, classify the official structured stop reason without text matching:

```go
if response.StopReason == anthropicsdk.StopReasonModelContextWindowExceeded {
	return nil, pierrors.Wrap(
		pierrors.ErrorCodeAIContextOverflow,
		"anthropic generate",
		errors.New("model context window exceeded"),
	)
}
```

The switch above deliberately normalizes stable Anthropic types even when an intermediary omits or changes the expected HTTP status.

- [ ] **Step 7: Run Provider tests**

Run:

```bash
go test ./pi/ai/providers -count=1
```

Expected: PASS; official SDK errors unwrap through the returned Pi error.

- [ ] **Step 8: Commit Provider classification**

```bash
git add pi/ai/providers/error.go pi/ai/providers/error_test.go pi/ai/providers/openai.go pi/ai/providers/anthropic.go
git commit -m "feat: classify provider recovery errors"
```

---

### Task 3: Add bounded, cancelable model retry

**Files:**
- Create: `pi/recovery.go`
- Create: `pi/test/recovery_test.go`
- Modify: `pi/loop.go`

**Interfaces:**
- Produces: `func (l *Loop) generateWithRetry(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error)`.
- Consumes: Provider ErrorCodes from Tasks 1 and 2.

- [ ] **Step 1: Write public Loop retry tests**

In `pi/test/recovery_test.go`, define a concurrency-safe scripted Provider whose steps contain either a response or an error and whose `Generate` records cloned requests. Use `withTestUsage` for successful responses.

Add these tests:

```go
func TestLoopRetriesTransientGenerationTwice(t *testing.T)
func TestLoopDoesNotRetryTerminalAICode(t *testing.T)
func TestLoopCancelsDuringRetryBackoff(t *testing.T)
```

The transient test script is:

```go
[]providerStep{
	{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("first"))},
	{err: pierrors.Wrap(pierrors.ErrorCodeAIRateLimited, "test", errors.New("second"))},
	{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done")}},
}
```

Assert three calls and elapsed time of at least 1.4 seconds. The terminal test uses `ErrorCodeAIUnauthorized` and asserts one call. The cancellation test cancels after the first call, asserts `errors.Is(err, context.Canceled)`, one call, and elapsed time below 500 ms.

- [ ] **Step 2: Run retry tests and verify they fail**

Run:

```bash
go test ./pi/test -run 'TestLoop(Retries|DoesNotRetry|Cancels)' -count=1
```

Expected: transient test observes one call instead of three.

- [ ] **Step 3: Implement generateWithRetry**

Create `pi/recovery.go` with fixed constants and no public policy object:

```go
const maxGenerateRetries = 2

func retryDelay(retry int) time.Duration {
	if retry == 0 {
		return 500 * time.Millisecond
	}
	return time.Second
}
```

Implement the loop:

```go
func (l *Loop) generateWithRetry(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) (*ai.Message, error) {
	for attempt := 0; ; attempt++ {
		response, err := l.provider.Generate(ctx, messages, tools)
		if err == nil {
			return response, nil
		}
		code := pierrors.ErrorCodeOf(err)
		if attempt >= maxGenerateRetries ||
			(code != pierrors.ErrorCodeAITransient && code != pierrors.ErrorCodeAIRateLimited) {
			return response, err
		}
		delay := retryDelay(attempt)
		logsdk.Warn(ctx, "model generation retry",
			logsdk.Any("component", "model_recovery"),
			logsdk.Any("error_code", code),
			logsdk.Any("retry", attempt+1),
			logsdk.Any("delay_ms", delay.Milliseconds()),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
```

- [ ] **Step 4: Route Thinking and Action through generateWithRetry**

Replace both direct `l.provider.Generate` calls in `pi/loop.go` with `l.generateWithRetry`. Preserve existing validation, Usage checks, message appends, and error wrapping.

- [ ] **Step 5: Run retry and existing Loop tests**

Run:

```bash
go test ./pi/test -run 'TestLoop' -count=1
```

Expected: PASS. Fixed retry tests take about 1.5 seconds; cancellation does not wait for the timer.

- [ ] **Step 6: Commit bounded retry**

```bash
git add pi/recovery.go pi/test/recovery_test.go pi/loop.go
git commit -m "feat: retry transient model failures"
```

---

### Task 4: Build bounded Harness compaction plans

**Files:**
- Create: `pi/harness/compaction.go`
- Create: `pi/test/compaction_test.go`

**Interfaces:**
- Produces: `harness.CompactionPlan`, `harness.BuildCompactionPlan([]ai.Message) (CompactionPlan, error)`, and `harness.ApplySummary(CompactionPlan, string) []ai.Message`.
- Does not consume a Provider and does not perform a model call.

- [ ] **Step 1: Write compaction-plan tests**

Create `pi/test/compaction_test.go` and add:

```go
func TestBuildCompactionPlanPreservesSystemAndCurrentTurn(t *testing.T)
func TestBuildCompactionPlanKeepsToolCallAndResultTogether(t *testing.T)
func TestBuildCompactionPlanLimitsSerializedSummaryTo32KiB(t *testing.T)
func TestBuildCompactionPlanRejectsContextWithoutOldHistory(t *testing.T)
func TestApplySummaryInsertsInternalSystemMessage(t *testing.T)
```

Use this representative context:

```go
messages := []ai.Message{
	{Role: ai.RoleSystem, Content: blocks("system")},
	{Role: ai.RoleUser, Content: blocks("old question")},
	{Role: ai.RoleAssistant, Content: blocks("old answer")},
	{Role: ai.RoleUser, Content: blocks("current question")},
	{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
	{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", Content: blocks("result")},
}
```

Assert old question/answer enter `SummaryMessages`, while System/current/tool messages enter `PreservedMessages`. Marshal `SummaryMessages` and assert `len(encoded) <= 32*1024`.

- [ ] **Step 2: Run compaction tests and verify they fail**

Run:

```bash
go test ./pi/test -run 'Test(BuildCompactionPlan|ApplySummary)' -count=1
```

Expected: compilation fails because the Harness compaction API does not exist.

- [ ] **Step 3: Implement CompactionPlan and turn grouping**

Create `pi/harness/compaction.go` with:

```go
const maxCompactionInputBytes = 32 * 1024

type CompactionPlan struct {
	SummaryMessages   []ai.Message
	PreservedMessages []ai.Message
}
```

Implement the plan builder with complete User-delimited turns:

```go
func BuildCompactionPlan(messages []ai.Message) (CompactionPlan, error) {
	systemEnd := 0
	for systemEnd < len(messages) && messages[systemEnd].Role == ai.RoleSystem {
		systemEnd++
	}
	currentStart := -1
	for index := len(messages) - 1; index >= systemEnd; index-- {
		if messages[index].Role == ai.RoleUser {
			currentStart = index
			break
		}
	}
	if currentStart < 0 {
		return CompactionPlan{}, errors.New("compaction requires a current user turn")
	}

	turns := splitCompactionTurns(messages[systemEnd:currentStart])
	selected := make([]ai.Message, 0)
	for index := len(turns) - 1; index >= 0; index-- {
		candidate := make([]ai.Message, 0, len(turns[index])+len(selected))
		candidate = append(candidate, turns[index]...)
		candidate = append(candidate, selected...)
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return CompactionPlan{}, fmt.Errorf("encode compaction history: %w", err)
		}
		if len(encoded) > maxCompactionInputBytes {
			break
		}
		selected = candidate
	}
	if len(selected) == 0 {
		return CompactionPlan{}, errors.New("compaction has no bounded old history")
	}

	preserved := make([]ai.Message, 0, systemEnd+len(messages)-currentStart)
	preserved = append(preserved, messages[:systemEnd]...)
	preserved = append(preserved, messages[currentStart:]...)
	return CompactionPlan{
		SummaryMessages:   append([]ai.Message(nil), selected...),
		PreservedMessages: append([]ai.Message(nil), preserved...),
	}, nil
}

func splitCompactionTurns(messages []ai.Message) [][]ai.Message {
	if len(messages) == 0 {
		return nil
	}
	start := 0
	turns := make([][]ai.Message, 0)
	for index := 1; index < len(messages); index++ {
		if messages[index].Role != ai.RoleUser {
			continue
		}
		turns = append(turns, append([]ai.Message(nil), messages[start:index]...))
		start = index
	}
	turns = append(turns, append([]ai.Message(nil), messages[start:]...))
	return turns
}
```

Because every ToolCall and Tool Result following one User message remain inside the same turn slice, selection cannot cut their protocol pair apart.

- [ ] **Step 4: Implement summary application**

`ApplySummary` finds the leading System count inside `PreservedMessages`, inserts this exact internal message after them, and appends the preserved current turn:

```go
summaryMessage := ai.Message{
	Role: ai.RoleSystem,
	Content: []ai.ContentBlock{
		ai.TextBlock("# Earlier conversation summary\n" + strings.TrimSpace(summary)),
	},
}
```

Use this fresh-slice assembly so the plan is never mutated:

```go
func ApplySummary(plan CompactionPlan, summary string) []ai.Message {
	systemEnd := 0
	for systemEnd < len(plan.PreservedMessages) && plan.PreservedMessages[systemEnd].Role == ai.RoleSystem {
		systemEnd++
	}
	result := make([]ai.Message, 0, len(plan.PreservedMessages)+1)
	result = append(result, plan.PreservedMessages[:systemEnd]...)
	result = append(result, ai.Message{
		Role: ai.RoleSystem,
		Content: []ai.ContentBlock{
			ai.TextBlock("# Earlier conversation summary\n" + strings.TrimSpace(summary)),
		},
	})
	result = append(result, plan.PreservedMessages[systemEnd:]...)
	return result
}
```

- [ ] **Step 5: Run compaction tests**

Run:

```bash
go test ./pi/test -run 'Test(BuildCompactionPlan|ApplySummary)' -count=1
```

Expected: PASS, including ToolCall/Tool Result grouping and the exact byte bound.

- [ ] **Step 6: Commit Harness compaction planning**

```bash
git add pi/harness/compaction.go pi/test/compaction_test.go
git commit -m "feat: plan bounded context compaction"
```

---

### Task 5: Recover once from Context Overflow and meter the summary

**Files:**
- Modify: `pi/recovery.go`
- Modify: `pi/loop.go`
- Modify: `pi/contract.go`
- Modify: `pi/test/recovery_test.go`
- Modify: `pi/test/agent_test.go`

**Interfaces:**
- Produces: `ModelInvocationPhaseCompaction`, private `generationResult`, `(*Loop).generate`, and `(*Loop).compact`.
- Consumes: `generateWithRetry` from Task 3 and Harness compaction API from Task 4.

- [ ] **Step 1: Write one-shot overflow recovery tests**

Extend the scripted Provider in `pi/test/recovery_test.go` and add:

```go
func TestAgentCompactsOnceAfterContextOverflow(t *testing.T)
func TestAgentReturnsSummaryFailureWithoutFallback(t *testing.T)
func TestAgentDoesNotCompactAgainWhenRetriedRequestOverflows(t *testing.T)
```

For the successful test, use this script:

```go
[]providerStep{
	{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
	{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("old work summarized"), Usage: costedUsage(1)}},
	{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done"), Usage: costedUsage(2)}},
}
```

Give `RunRequest.History` an old customer/AI pair so there is history to summarize. Assert:

- Provider is called three times.
- The second request contains no tools and contains the fixed summary instruction.
- The third request contains `# Earlier conversation summary` and the current input.
- `RunResult.Invocations` contains `compaction` sequence 1 and `action` sequence 2.
- The summary is absent from `RunResult.NewMessages`.

The failure tests assert exactly two or three calls respectively and no recursive compaction.

- [ ] **Step 2: Run overflow tests and verify they fail**

Run:

```bash
go test ./pi/test -run 'TestAgent(Compacts|ReturnsSummary|DoesNotCompact)' -count=1
```

Expected: first overflow is returned immediately and no Compaction invocation exists.

- [ ] **Step 3: Add the Compaction invocation phase**

In `pi/contract.go` add:

```go
ModelInvocationPhaseCompaction ModelInvocationPhase = "compaction"
```

Do not add a second invocation type.

- [ ] **Step 4: Implement summary generation**

Add to `pi/recovery.go`:

```go
const compactionSystemPrompt = `Summarize the supplied earlier conversation for another agent to continue the same task.
Preserve user goals, explicit constraints, accepted decisions, completed work, pending work, exact file paths, identifiers, tool results, and stable error codes.
Do not answer the user and do not continue the task.`
```

`(*Loop).compact` must:

1. Call `harness.BuildCompactionPlan`.
2. JSON-marshal only `plan.SummaryMessages`.
3. Call `generateWithRetry` with one System summary instruction, one User message containing the JSON history, and no tools.
4. Require an Assistant response with non-empty text, no ToolCall, a non-nil Usage, and valid metered cost.
5. Return `harness.ApplySummary(plan, text)` and a copied `ai.Usage`.

Summary failure returns directly. It must not call the recovery-aware `generate` method and therefore cannot recursively compact.

- [ ] **Step 5: Implement one-shot generation orchestration**

Add:

```go
type generationResult struct {
	message         *ai.Message
	context         []ai.Message
	compactionUsage *ai.Usage
}
```

`(*Loop).generate` first calls `generateWithRetry`. When the returned code is not `ErrorCodeAIContextOverflow`, return the response/error unchanged. On Context Overflow, call `compact` once, then call `generateWithRetry` once with the compacted context. A second Context Overflow is returned directly.

- [ ] **Step 6: Record invocation sequence only for completed calls**

In `runDetailed`, replace pre-call `callSequence++` with a closure invoked only when a valid response Usage is accepted:

```go
recordInvocation := func(phase ModelInvocationPhase, usage ai.Usage) {
	callSequence++
	invocations = append(invocations, ModelInvocation{
		Sequence: callSequence,
		Phase:    phase,
		Usage:    usage,
	})
}
```

For Thinking and Action:

1. Call `l.generate`.
2. Replace `contextHistory` with `generated.context`.
3. Record `generated.compactionUsage` first when non-nil.
4. Validate and record the main response phase.

Do not append the summary to `newMessages`.

- [ ] **Step 7: Run recovery and invocation tests**

Run:

```bash
go test ./pi/test -run 'TestAgent|TestLoop' -count=1
```

Expected: PASS; completed Invocation sequence has no gap for failed Provider attempts.

- [ ] **Step 8: Commit Context Overflow recovery**

```bash
git add pi/recovery.go pi/loop.go pi/contract.go pi/test/recovery_test.go pi/test/agent_test.go
git commit -m "feat: compact context after overflow"
```

---

### Task 6: Attach stable codes to raw Tool failures

**Files:**
- Modify: `pi/harness/errors/errors.go`
- Modify: `pi/harness/errors/errors_test.go`
- Modify: `pi/event.go`
- Modify: `pi/tool_runtime.go`
- Modify: `pi/middleware.go`
- Modify: `pi/harness/tools/edit.go`
- Modify: `pi/harness/tools/edit_test.go`
- Modify: `pi/test/middleware_test.go`
- Modify: `pi/test/tool_runtime_test.go`
- Modify: `pi/test/tool_runtime_public_test.go`

**Interfaces:**
- Produces: `ClassifyTool(op string, err error) error` and `ToolResult.ErrorCode pierrors.ErrorCode`.
- Consumes: Tool ErrorCodes from Task 1.

- [ ] **Step 1: Write standard Tool error classification tests**

Add to `pi/harness/errors/errors_test.go`:

```go
func TestClassifyToolUsesStableCodes(t *testing.T) {
	tests := []struct {
		err  error
		want ErrorCode
	}{
		{err: fs.ErrNotExist, want: ErrorCodeToolResourceNotFound},
		{err: fs.ErrPermission, want: ErrorCodeToolPermissionDenied},
		{err: context.DeadlineExceeded, want: ErrorCodeToolTimeout},
		{err: stderrors.New("failed"), want: ErrorCodeToolRuntime},
	}
	for _, tt := range tests {
		got := ClassifyTool("execute", tt.err)
		if ErrorCodeOf(got) != tt.want || !stderrors.Is(got, tt.err) {
			t.Fatalf("ClassifyTool(%v) = %v", tt.err, got)
		}
	}
}
```

- [ ] **Step 2: Write ToolRuntime and middleware code tests**

Update public ToolRuntime tests to assert:

```go
result.ErrorCode == pierrors.ErrorCodeToolInvalidArguments
```

for schema failures, and:

```go
result.ErrorCode == pierrors.ErrorCodeToolPanic
```

for a panic. Add a fake tool returning `fmt.Errorf("read: %w", fs.ErrNotExist)` and assert `ErrorCodeToolResourceNotFound` while the original error text remains in `Content`.

Update `pi/harness/tools/edit_test.go` so ambiguity and no-match tests assert `pierrors.ErrorCodeOf(err)` equals `ErrorCodeToolEditNotUnique` and `ErrorCodeToolEditNoMatch`, rather than using `strings.Contains` to determine the failure type.

- [ ] **Step 3: Run Tool tests and verify they fail**

Run:

```bash
go test ./pi/harness/errors ./pi/harness/tools ./pi/test -run 'Test(ClassifyTool|ToolRuntime|PanicRecoveryMiddleware|FindUniqueTextMatch)' -count=1
```

Expected: ErrorCode assertions fail because ToolResult does not yet carry a code.

- [ ] **Step 4: Implement ClassifyTool**

Add:

```go
func ClassifyTool(op string, err error) error {
	if err == nil {
		return nil
	}
	var classified *Error
	if stderrors.As(err, &classified) {
		return err
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return Wrap(ErrorCodeCanceled, op, err)
	case stderrors.Is(err, context.DeadlineExceeded):
		return Wrap(ErrorCodeToolTimeout, op, err)
	case stderrors.Is(err, fs.ErrNotExist):
		return Wrap(ErrorCodeToolResourceNotFound, op, err)
	case stderrors.Is(err, fs.ErrPermission):
		return Wrap(ErrorCodeToolPermissionDenied, op, err)
	default:
		return Wrap(ErrorCodeToolRuntime, op, err)
	}
}
```

- [ ] **Step 5: Carry ErrorCode through ToolResult normalization**

Add to `ToolResult`:

```go
ErrorCode pierrors.ErrorCode `json:"error_code,omitempty"`
```

In `normalizeToolResult`, classify before replacing empty content:

```go
var errorCode pierrors.ErrorCode
if err != nil {
	errorCode = pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", err))
}
```

Set the field on the returned ToolResult. Keep `Content` based on the original `err.Error()`, not the wrapped Pi error string.

Because schema, panic, and edit errors may already be classified before normalization, unwrap the Pi classification only for display:

```go
func toolErrorText(err error) string {
	var classified *pierrors.Error
	if errors.As(err, &classified) {
		return classified.Err.Error()
	}
	return err.Error()
}
```

Use `toolErrorText(err)` when `output.Content` is empty. Keep the classified error itself for `ErrorCodeOf` and cancellation checks.

- [ ] **Step 6: Classify schema, panic, and edit errors at their source**

Rename `recoveryMiddleware` to `panicRecoveryMiddleware` and the default registration name to `panic_recovery`. Its returned error becomes:

```go
pierrors.Wrap(
	pierrors.ErrorCodeToolPanic,
	"tool panic",
	errors.New("tool execution failed"),
)
```

Wrap schema validation errors with `ErrorCodeToolInvalidArguments`.

In `findUniqueTextMatch` and `requireUniqueMatch`, wrap no-match and ambiguity errors with `ErrorCodeToolEditNoMatch` and `ErrorCodeToolEditNotUnique`. Keep the existing user-readable messages as causes.

- [ ] **Step 7: Run Tool tests**

Run:

```bash
go test ./pi/harness/errors ./pi/harness/tools ./pi/test -run 'Test(ClassifyTool|ToolRuntime|PanicRecoveryMiddleware|FindUniqueTextMatch)' -count=1
```

Expected: PASS; raw content remains readable and all recovery decisions use ErrorCode.

- [ ] **Step 8: Commit Tool error codes**

```bash
git add pi/harness/errors/errors.go pi/harness/errors/errors_test.go pi/event.go pi/tool_runtime.go pi/middleware.go pi/harness/tools/edit.go pi/harness/tools/edit_test.go pi/test/middleware_test.go pi/test/tool_runtime_test.go pi/test/tool_runtime_public_test.go
git commit -m "feat: classify tool execution failures"
```

---

### Task 7: Inject Tool Recovery Hint only into model context

**Files:**
- Modify: `pi/recovery.go`
- Modify: `pi/loop.go`
- Modify: `pi/test/loop_test.go`
- Modify: `pi/test/recovery_test.go`

**Interfaces:**
- Produces: private `toolRecoveryMessage(ai.Message, pierrors.ErrorCode) ai.Message` and `recoveryHint(pierrors.ErrorCode) string`.
- Consumes: `ToolResult.ErrorCode` from Task 6.

- [ ] **Step 1: Write the raw/enhanced boundary test**

Add `TestLoopInjectsToolRecoveryHintOnlyIntoProviderContext` with a fake ToolRuntime result:

```go
pi.ToolResult{
	ToolCallID: "call-1",
	ToolName:   "edit",
	Content:    blocks("在文件中未找到 oldText"),
	IsError:    true,
	ErrorCode:  pierrors.ErrorCodeToolEditNoMatch,
}
```

Script one Assistant ToolCall followed by final Assistant text. Assert:

- Reporter ToolEnd content is exactly the raw error and contains no `Recovery Hint`.
- The Tool Message returned by `Loop.Run` is exactly the raw error.
- The second Provider request contains the raw error and the instruction to call `read`.
- The fake ToolRuntime executes exactly once.

Add a second test with `ErrorCodeToolRuntime` and assert no hint is injected.

- [ ] **Step 2: Run Tool Hint tests and verify they fail**

Run:

```bash
go test ./pi/test -run 'TestLoopInjectsToolRecoveryHint' -count=1
```

Expected: the second Provider request contains only the raw Tool error.

- [ ] **Step 3: Implement the three approved hints**

Add to `pi/recovery.go`:

```go
func recoveryHint(code pierrors.ErrorCode) string {
	switch code {
	case pierrors.ErrorCodeToolEditNoMatch:
		return "先使用 read 获取文件最新内容，再使用精确的 oldText 重新编辑。"
	case pierrors.ErrorCodeToolEditNotUnique:
		return "增加 oldText 的相邻代码，确保只匹配一处后再编辑。"
	case pierrors.ErrorCodeToolResourceNotFound:
		return "不要继续猜测路径；先检查真实目录结构和文件名。"
	default:
		return ""
	}
}
```

Build the enhanced copy without mutating raw content:

```go
func toolRecoveryMessage(message ai.Message, code pierrors.ErrorCode) ai.Message {
	hint := recoveryHint(code)
	if hint == "" {
		return message
	}
	enhanced := message
	enhanced.Content = append([]ai.ContentBlock(nil), message.Content...)
	enhanced.Content = append(enhanced.Content, ai.TextBlock("\n\n[Recovery Hint]\n"+hint))
	return enhanced
}
```

- [ ] **Step 4: Split raw NewMessages from enhanced contextHistory**

Replace the single Tool message append in `pi/loop.go` with:

```go
rawMessage := ai.Message{
	Role:       ai.RoleTool,
	Content:    append([]ai.ContentBlock(nil), result.Content...),
	ToolCallID: result.ToolCallID,
	ToolName:   result.ToolName,
	IsError:    result.IsError,
}
modelMessage := toolRecoveryMessage(rawMessage, result.ErrorCode)
contextHistory = append(contextHistory, modelMessage)
newMessages = append(newMessages, rawMessage)
```

Do not change ToolEnd event emission; it already happens inside ToolRuntime before Loop enhancement.

- [ ] **Step 5: Run Tool Hint and Loop tests**

Run:

```bash
go test ./pi/test -run 'TestLoop' -count=1
```

Expected: PASS; raw result and enhanced model context are independently asserted.

- [ ] **Step 6: Commit model-only Tool Hints**

```bash
git add pi/recovery.go pi/loop.go pi/test/loop_test.go pi/test/recovery_test.go
git commit -m "feat: guide model after tool failures"
```

---

### Task 8: Document and verify the complete recovery flow

**Files:**
- Modify: `docs/sdk-architecture.md`

**Interfaces:**
- Consumes: all recovery behavior from Tasks 1–7.
- Produces: final architecture documentation and a clean verified repository.

- [ ] **Step 1: Update SDK architecture documentation**

Replace the statement that Pi performs no retry or summary with these exact behavioral rules:

```text
- A Run retries only classified transient and rate-limit Provider failures, at 500 ms and 1 s.
- A structured context-overflow error triggers one bounded summary and one retried original request.
- Compaction summary Usage is returned as a compaction ModelInvocation.
- Tool Recovery Hint is derived from ErrorCode and exists only in the next Provider context.
- Reporter, NewMessages, and Conversation persistence retain the raw Tool Result.
- Cancellation, deadline, authentication, quota, invalid request, and unknown failures terminate immediately.
```

Extend the ErrorCode table with all new AI and Tool values.

- [ ] **Step 2: Run formatting and focused package tests**

Run:

```bash
gofmt -w pi/ai/providers/error.go pi/ai/providers/error_test.go pi/ai/providers/openai.go pi/ai/providers/anthropic.go pi/recovery.go pi/harness/compaction.go pi/harness/errors/errors.go pi/harness/errors/errors_test.go pi/harness/observability/tracker.go pi/loop.go pi/contract.go pi/event.go pi/tool_runtime.go pi/middleware.go pi/harness/tools/edit.go pi/harness/tools/edit_test.go pi/test/recovery_test.go pi/test/compaction_test.go pi/test/loop_test.go pi/test/agent_test.go pi/test/middleware_test.go pi/test/tool_runtime_test.go pi/test/tool_runtime_public_test.go pi/test/package_boundaries_test.go
go test ./pi/... -count=1
```

Expected: all Pi package tests PASS.

- [ ] **Step 3: Run full repository verification**

Run:

```bash
go test ./... -count=1
go vet ./...
git diff --check
```

Expected: all commands exit 0. If a Transport test cannot bind a local port in the sandbox, rerun only that unchanged test command with the required sandbox approval and record the result.

- [ ] **Step 4: Verify prohibited design elements are absent**

Run:

```bash
rg -n "GenerationFailure|GenerationError|WrapGeneration|ErrGeneration|RecoveryManager|type ProviderInfo|ContextWindow[[:space:]]+int|MaxOutputTokens[[:space:]]+int|ShouldCompact" pi
```

Expected: no matches. `providers.Options` remains unchanged except for no recovery-related additions.

- [ ] **Step 5: Inspect the final recovery diff**

Run:

```bash
git diff --stat HEAD~7
git diff --check HEAD~7
```

Expected: only the files listed in this plan changed, with no whitespace errors or compatibility aliases.

- [ ] **Step 6: Commit documentation and final test adjustments**

```bash
git add docs/sdk-architecture.md
git commit -m "docs: document pi error recovery"
```

---

## Completion Criteria

- The official SDKs perform no hidden retry; Loop owns the only model retry budget.
- `ErrorCodeOf` distinguishes retryable, overflow, terminal AI failures, and high-value Tool failures.
- Thinking and Action share one cancelable retry implementation.
- Context Overflow performs one bounded summary and one retried original request, never recursively.
- Compaction Summary Usage is validated, costed, ordered, and returned as `ModelInvocationPhaseCompaction`.
- Tool ErrorCode is visible on the raw ToolResult while its Hint exists only in Provider context.
- Reporter and `RunResult.NewMessages` contain no Recovery Hint or internal summary.
- All focused tests, `go test ./... -count=1`, `go vet ./...`, and `git diff --check` pass.
