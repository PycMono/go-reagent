package engine

import (
	"context"

	logsdk "github.com/PycMono/go-logger-sdk"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
)

// logSkillDiagnostics 将 Skill 发现和解析过程中产生的诊断信息写入日志，
// 并根据严重程度分别使用 Error、Warning 或 Info 级别，方便定位无效 Skill。
func logSkillDiagnostics(ctx context.Context, diagnostics []ctxpkg.SkillDiagnostic) {
	for _, diagnostic := range diagnostics {
		fields := []logsdk.Fields{
			logsdk.Any("component", "engine"),
			logsdk.Any("code", diagnostic.Code),
			logsdk.Any("path", diagnostic.Path),
			logsdk.Any("severity", diagnostic.Severity),
			logsdk.Any("detail", diagnostic.Message),
		}
		switch diagnostic.Severity {
		case ctxpkg.DiagnosticSeverityError:
			logsdk.Error(ctx, "[Engine] Agent Skill 诊断", fields...)
		case ctxpkg.DiagnosticSeverityWarning:
			logsdk.Warn(ctx, "[Engine] Agent Skill 诊断", fields...)
		default:
			logsdk.Info(ctx, "[Engine] Agent Skill 诊断", fields...)
		}
	}
}
