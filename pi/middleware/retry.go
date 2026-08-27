package middleware

import (
	"strings"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// retryableCodes 是允许重试的工具错误码：泛型运行时失败与执行超时。
// 参数校验、权限拒绝、资源不存在等确定性错误重试无意义；取消与整体
// 超期由 Run 层处置，绝不在此重试。
var retryableCodes = map[pierrors.ErrorCode]struct{}{
	pierrors.ErrorCodeToolRuntime: {},
	pierrors.ErrorCodeToolTimeout: {},
}

const (
	// DefaultRetryBackoff 是未配置 backoff（<=0）时第 N 次重试前的等待
	// 毫秒基数（线性递增），与 go-reagent 服务的默认配置保持一致。
	DefaultRetryBackoff = 200 * time.Millisecond
	// MaxRetryAttempts 是总尝试次数（含首次）上限；超过时钳制到该值。
	MaxRetryAttempts = 5
)

// Retry 返回按白名单重试瞬态失败的 Handler。只有 tools 列出的工具
// （应为幂等工具）且错误码属于 retryableCodes 时才重试，最多 attempts
// 次（含首次，上限 MaxRetryAttempts），第 N 次重试前等待 backoff*N；
// backoff<=0 时使用 DefaultRetryBackoff。
//
// 重试通过索引复位重跑本 Handler 之后的整条后缀链，因此后缀 Handler
// 必须可重入（内置 Handler 均满足）。默认装配顺序 Permission → Retry →
// Timeout 下，每次重试获得新的执行期限，而权限判定只执行一次。
func Retry(attempts int, backoff time.Duration, tools []string) Handler {
	if attempts > MaxRetryAttempts {
		attempts = MaxRetryAttempts
	}
	if backoff <= 0 {
		backoff = DefaultRetryBackoff
	}
	whitelist := make(map[string]struct{}, len(tools))
	for _, name := range tools {
		// 防御性归一化：跳过空白项，容忍前后空格（业务校验只在根
		// config 包按需执行，SDK 侧不做第二套校验）。
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
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
