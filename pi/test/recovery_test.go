package test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type providerStep struct {
	response *ai.Message
	err      error
	stream   ai.Stream
}

type scriptedProvider struct {
	mu        sync.Mutex
	steps     []providerStep
	requests  [][]ai.Message
	tools     [][]ai.ToolDefinition
	afterCall func(int)
}

func (p *scriptedProvider) Stream(
	_ context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) ai.Stream {
	p.mu.Lock()
	call := len(p.requests) + 1
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	p.tools = append(p.tools, append([]ai.ToolDefinition(nil), tools...))
	step := p.steps[call-1]
	p.mu.Unlock()

	if p.afterCall != nil {
		p.afterCall(call)
	}
	if step.stream != nil {
		return step.stream
	}
	return newTestStream(withTestUsage(step.response), step.err)
}

func (p *scriptedProvider) snapshots() ([][]ai.Message, [][]ai.ToolDefinition) {
	p.mu.Lock()
	defer p.mu.Unlock()
	requests := make([][]ai.Message, len(p.requests))
	for index := range p.requests {
		requests[index] = append([]ai.Message(nil), p.requests[index]...)
	}
	tools := make([][]ai.ToolDefinition, len(p.tools))
	for index := range p.tools {
		tools[index] = append([]ai.ToolDefinition(nil), p.tools[index]...)
	}
	return requests, tools
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func TestLoopRetriesTransientGenerationTwice(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("first"))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIRateLimited, "test", errors.New("second"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done")}},
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	started := time.Now()
	result, err := runtime.Run(context.Background(), validAgentRequest("run"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 3 || len(result.NewMessages) != 1 {
		t.Fatalf("calls/messages = %d/%#v, want 3/one", provider.callCount(), result.NewMessages)
	}
	if elapsed := time.Since(started); elapsed < 1400*time.Millisecond {
		t.Fatalf("elapsed = %s, want both retry delays", elapsed)
	}
}

func TestLoopDoesNotRetryTerminalAICode(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{{
		err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("unauthorized")),
	}}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	_, err := runtime.Run(context.Background(), validAgentRequest("run"), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIUnauthorized || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
}

func TestLoopDoesNotRetryAfterPublishingStreamContent(t *testing.T) {
	streamErr := pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("stream interrupted"))
	provider := &scriptedProvider{steps: []providerStep{
		{
			stream: &contractStream{
				events: []ai.StreamEvent{
					{Type: ai.StreamEventStart},
					{Type: ai.StreamEventTextDelta, TextDelta: "partial"},
					{Type: ai.StreamEventError},
				},
				err: streamErr,
			},
		},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("duplicate")}},
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	_, err := runtime.Run(context.Background(), validAgentRequest("run"), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAITransient || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d, want transient/1", err, provider.callCount())
	}
}

func TestLoopCancelsDuringRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{
		steps: []providerStep{{
			err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("temporary")),
		}},
		afterCall: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	started := time.Now()
	_, err := runtime.Run(ctx, validAgentRequest("run"), nil)
	if !errors.Is(err, context.Canceled) || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("elapsed = %s, retry backoff ignored cancellation", elapsed)
	}
}

func TestLoopNormalizesRetryBackoffCancelCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("custom cancellation cause")
	provider := &scriptedProvider{
		steps: []providerStep{{
			err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("temporary")),
		}},
		afterCall: func(call int) {
			if call == 1 {
				cancel(cause)
			}
		},
	}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	result, err := runtime.Run(ctx, validAgentRequest("run"), nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, cause) || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d, want canceled without custom cause/1", err, provider.callCount())
	}
	if result.Termination.Reason != pi.RunTerminationCanceled {
		t.Fatalf("termination = %#v, want canceled", result.Termination)
	}
}

func TestLoopNormalizesRetryBackoffTimeoutCause(t *testing.T) {
	cause := errors.New("custom timeout cause")
	ctx, cancel := context.WithTimeoutCause(context.Background(), 100*time.Millisecond, cause)
	defer cancel()
	provider := &scriptedProvider{
		steps: []providerStep{{
			err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("temporary")),
		}},
		afterCall: func(int) {
			<-ctx.Done()
		},
	}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	result, err := runtime.Run(ctx, validAgentRequest("run"), nil)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, cause) || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d, want deadline without custom cause/1", err, provider.callCount())
	}
	if result.Termination.Reason != pi.RunTerminationDeadline {
		t.Fatalf("termination = %#v, want deadline", result.Termination)
	}
}

func TestAgentCompactsOnceAfterContextOverflow(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("old work summarized"), Usage: costedUsage(1)}},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done"), Usage: costedUsage(2)}},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	requests, tools := provider.snapshots()
	if len(requests) != 3 {
		t.Fatalf("provider calls = %d, want 3", len(requests))
	}
	if len(tools[1]) != 0 || !messagesContain(requests[1], "请总结所提供的早期对话") {
		t.Fatalf("summary request/tools = %#v/%#v", requests[1], tools[1])
	}
	if !messagesContain(requests[2], `<compacted-summary untrusted="true">`) ||
		!messagesContain(requests[2], "old work summarized") ||
		!messagesContain(requests[2], "current question") {
		t.Fatalf("retried request = %#v", requests[2])
	}
	if len(result.Invocations) != 2 ||
		result.Invocations[0].Sequence != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction ||
		result.Invocations[1].Sequence != 2 || result.Invocations[1].Phase != pi.ModelInvocationPhaseAction {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
	if len(result.NewMessages) != 1 || messagesContain(result.NewMessages, "old work summarized") {
		t.Fatalf("NewMessages = %#v", result.NewMessages)
	}
}

func TestAgentReturnsOriginalOverflowWhenSummaryFails(t *testing.T) {
	// Reactive 的辅助压缩错误默认不覆盖原始 overflow；无 L1 进展时直接返回。
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("summary failed"))},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow || provider.callCount() != 2 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentDoesNotCompactAgainWhenRetriedRequestOverflows(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("old work summarized"), Usage: costedUsage(1)}},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("still too long"))},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow || provider.callCount() != 3 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 1 ||
		result.Invocations[0].Sequence != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentReturnsOriginalOverflowWithoutOldHistory(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{{
		err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("current input is too long")),
	}}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), validAgentRequest("current question"), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func recoveryAgentRequest() pi.RunRequest {
	request := validAgentRequest("current question")
	request.History = []pi.Message{
		{ContentType: "text", Content: "old question", SenderType: "customer"},
		// 历史需要足够大，才能通过新范围选择的净缩减过滤（投影字节 − 预估摘要 > 0）。
		{ContentType: "text", Content: strings.Repeat("答", 4096), SenderType: "ai"},
	}
	return request
}

func messagesContain(messages []ai.Message, fragment string) bool {
	for _, message := range messages {
		text, _ := ai.TextContent(message.Content)
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}
