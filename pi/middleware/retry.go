package middleware

import (
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// retryableCodes 是允许重试的工具错误码：泛型运行时失败与执行超时。
// 参数校验、权限拒绝、资源不存在等确定性错误重试无意义；取消与整体
// 超期由 Run 层处置，绝不在此重试。
var retryableCodes = map[pierrors.ErrorCode]struct{}{
	pierrors.ErrorCodeToolRuntime: {},
	pierrors.ErrorCodeToolTimeout: {},
}

// Retry 返回按白名单重试瞬态失败的 Handler。只有 tools 列出的工具
// （应为幂等工具）且错误码属于 retryableCodes 时才重试，最多 attempts
// 次（含首次），第 N 次重试前等待 backoff*N。
//
// 重试通过索引复位重跑本 Handler 之后的整条后缀链，因此后缀 Handler
// 必须可重入（内置 Handler 均满足）。默认装配顺序 Permission → Retry →
// Timeout 下，每次重试获得新的执行期限，而权限判定只执行一次。
func Retry(attempts int, backoff time.Duration, tools []string) Handler {
	whitelist := make(map[string]struct{}, len(tools))
	for _, name := range tools {
		whitelist[name] = struct{}{}
	}
	return func(e *Execution) {
		if attempts < 2 {
			e.Next()
			return
		}
		if _, ok := whitelist[e.Definition.Name]; !ok {
			e.Next()
			return
		}
		// 进入本 Handler 时 e.index 正是自身位置；每次重试复位到该位置，
		// Next 即从后缀第一个 Handler 重新执行。
		start := e.index
		for attempt := 1; ; attempt++ {
			e.index = start
			e.Err = nil
			e.Output = ai.ToolOutput{}
			e.Next()
			if e.Err == nil || attempt >= attempts || !isRetryable(e.Err) {
				return
			}
			select {
			case <-e.Ctx.Done():
				// 整体取消/超期：保留当前错误返回，由 ToolRuntime 归一。
				return
			case <-time.After(backoff * time.Duration(attempt)):
			}
		}
	}
}

func isRetryable(err error) bool {
	_, ok := retryableCodes[pierrors.ErrorCodeOf(err)]
	return ok
}
