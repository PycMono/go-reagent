package middleware

import (
	"fmt"
	"regexp"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// PermissionRule 是一条 deny 规则：工具名精确匹配，且参数正则命中
// tool_call 的原始参数 JSON 时拒绝执行。
type PermissionRule struct {
	Tool    string
	Pattern *regexp.Regexp
	// Reason 是可选的拒绝原因，随错误返回给模型阅读；为空时使用默认文案。
	Reason string
}

// Permission 返回按 deny 规则拦截高危 Tool 调用的 Handler。它是 opt-in
// 中间件，不在 Defaults 中——由装配层（组合根）按配置追加在默认链之后。
// 规则为空时零开销直接放行。
func Permission(rules []PermissionRule) Handler {
	return func(e *Execution) {
		for _, rule := range rules {
			if rule.Tool != e.Definition.Name {
				continue
			}
			if !rule.Pattern.Match(e.Call.Arguments) {
				continue
			}
			reason := rule.Reason
			if reason == "" {
				reason = "命中权限 deny 规则"
			}
			e.Output = ai.ToolOutput{}
			e.Block(pierrors.Wrap(
				pierrors.ErrorCodeToolPermissionDenied,
				"tool permission",
				fmt.Errorf("tool %q: %s", e.Definition.Name, reason),
			))
			return
		}
		e.Next()
	}
}
