package middleware

import "github.com/PycMono/go-reagent/pi/ai"

// EventForwarding 包装 Emit：Tool 发出的流式更新先通知 Observer，再转发给
// 原 Emit。返回前还原 Emit 保持可重入——即使被 Retry 重跑也不会多层
// 嵌套包装导致 Observer 重复通知。
func EventForwarding(e *Execution) {
	emit := e.Emit
	defer func() { e.Emit = emit }()
	e.Emit = func(update ai.ToolUpdate) {
		if e.Observer != nil {
			e.Observer(e.Ctx, e.Call, update)
		}
		if emit != nil {
			emit(update)
		}
	}
	e.Next()
}
