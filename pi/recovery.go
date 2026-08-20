package pi

import (
	"context"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/avast/retry-go/v4"
)

const maxGenerateRetries = 2

type generationResult struct {
	message *ai.Message
	context []ai.Message
}

func (l *Loop) generateWithRetry(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
) (*ai.Message, bool, error) {
	var response *ai.Message
	var published bool
	waitingForRetry := false
	err := retry.Do(func() error {
		waitingForRetry = false
		var err error
		response, published, err = consumeStream(l.provider.Stream(ctx, messages, tools), onText)
		return err
	},
		retry.Attempts(maxGenerateRetries+1),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
		retry.RetryIf(func(err error) bool {
			code := pierrors.ErrorCodeOf(err)
			return !published && (code == pierrors.ErrorCodeAITransient || code == pierrors.ErrorCodeAIRateLimited)
		}),
		retry.DelayType(func(attempt uint, _ error, _ *retry.Config) time.Duration {
			return retryDelay(int(attempt - 1))
		}),
		retry.OnRetry(func(attempt uint, err error) {
			if attempt >= maxGenerateRetries {
				return
			}

			waitingForRetry = true
			delay := retryDelay(int(attempt))
			logsdk.Warn(ctx, "model generation retry",
				logsdk.Any("component", "model_recovery"),
				logsdk.Any("error_code", pierrors.ErrorCodeOf(err)),
				logsdk.Any("retry", attempt+1),
				logsdk.Any("delay_ms", delay.Milliseconds()),
			)
		}),
	)
	if waitingForRetry && ctx.Err() != nil {
		return nil, false, ctx.Err()
	}

	return response, published, err
}

func consumeStream(stream ai.Stream, onText func(ai.ContentBlock)) (*ai.Message, bool, error) {
	defer stream.Close()

	published := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case ai.StreamEventTextDelta:
			if onText != nil && event.TextDelta != "" {
				published = true
				onText(ai.TextBlock(event.TextDelta))
			}
		case ai.StreamEventDone, ai.StreamEventError:
			message, err := stream.Result()
			return message, published, err
		}
	}
	message, err := stream.Result()

	return message, published, err
}

// generate 执行一次 Thinking 或 Action 调用。遇到 Context Overflow 且尚未
// 发布内容时，转交 recoverOverflow 走 reactive 压缩兜底。
func (l *Loop) generate(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	onText func(ai.ContentBlock),
	onCompactionUsage func(ai.Usage) error,
	rt *compactionRuntime,
) (generationResult, error) {
	response, published, err := l.generateWithRetry(ctx, messages, tools, onText)
	if err == nil || published || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow {
		return generationResult{message: response, context: messages}, err
	}
	return l.recoverOverflow(ctx, response, err, messages, tools, onText, onCompactionUsage, rt)
}

func retryDelay(retry int) time.Duration {
	if retry == 0 {
		return 500 * time.Millisecond
	}
	return time.Second
}
