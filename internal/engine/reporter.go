package engine

import "context"

// Reporter receives user-facing Agent lifecycle events.
type Reporter interface {
	OnThinking(ctx context.Context)
	OnToolCall(ctx context.Context, toolName string, args string)
	OnToolResult(ctx context.Context, toolName string, result string, isError bool)
	OnMessage(ctx context.Context, content string)
}

type multiReporter struct {
	reporters []Reporter
}

// NewMultiReporter broadcasts lifecycle events to every non-nil Reporter.
func NewMultiReporter(reporters ...Reporter) Reporter {
	filtered := make([]Reporter, 0, len(reporters))
	for _, reporter := range reporters {
		if reporter != nil {
			filtered = append(filtered, reporter)
		}
	}
	return &multiReporter{reporters: filtered}
}

func (r *multiReporter) OnThinking(ctx context.Context) {
	for _, reporter := range r.reporters {
		reporter.OnThinking(ctx)
	}
}

func (r *multiReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	for _, reporter := range r.reporters {
		reporter.OnToolCall(ctx, toolName, args)
	}
}

func (r *multiReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	for _, reporter := range r.reporters {
		reporter.OnToolResult(ctx, toolName, result, isError)
	}
}

func (r *multiReporter) OnMessage(ctx context.Context, content string) {
	for _, reporter := range r.reporters {
		reporter.OnMessage(ctx, content)
	}
}
