package pi

import (
	"context"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

const maxGenerateRetries = 2

func retryDelay(retry int) time.Duration {
	if retry == 0 {
		return 500 * time.Millisecond
	}
	return time.Second
}

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
