// Package middleware 提供 Tool 执行链的中间件机制，术语对齐 pi.dev：
// 链上的单元叫 Handler（pi 文档的 tool_call handlers），被拦截的一次
// Tool 执行叫 Execution（对应 tool_execution_start/end 事件），阻断
// 叫 Block（对应 {block: true, reason}）。链式结构即中间件模式：
// 切片顺序即执行顺序、前后置沿链折返、Block 短路。
//
// 两条语义铁律：
//
//  1. 短路必须 Block，不能只 return：Next 的索引循环中，handler 不调
//     Next 直接返回时，外层循环仍会继续执行后续 handler。
//  2. panic 恢复后必须 Block：recover 后控制流回到外层 Next 循环，
//     不 Block 会继续执行后面的 handler。
package middleware

import (
	"context"
	"encoding/json"

	"github.com/PycMono/go-reagent/pi/ai"
)

// Handler 是 Tool 执行链上的一环，签名为 func(*Execution)。
type Handler func(*Execution)

// UpdateObserver 接收 Tool 执行过程中的流式更新。主包用它桥接 ToolEvent，
// 避免本包反向依赖 pi 主包。
type UpdateObserver func(context.Context, ai.ToolCall, ai.ToolUpdate)

// Execution 表示一次被拦截的 Tool 执行：既携带调用的静态数据
// （Call/Definition/Tool 等），也承载链式处理的产出（Output/Err）。
type Execution struct {
	// Ctx 是执行上下文；Tracing handler 会把它替换为 Span ctx。
	Ctx          context.Context
	Call         ai.ToolCall
	Definition   ai.ToolDefinition
	Tool         ai.Tool
	Observer     UpdateObserver
	ValidateArgs func(json.RawMessage) error
	Emit         ai.UpdateEmitter
	Output       ai.ToolOutput
	Err          error

	handlers []Handler
	index    int
}

// Run 以 handlers 为链驱动执行：索引初始化为 -1，从 handlers[0] 开始。
func (e *Execution) Run(handlers []Handler) {
	e.handlers = handlers
	e.index = -1
	e.Next()
}

// Next 执行链上的下一个 handler。Block 之后循环立即结束。
func (e *Execution) Next() {
	e.index++
	for e.index < len(e.handlers) {
		e.handlers[e.index](e)
		e.index++
	}
}

// Block 记录失败原因并阻断链式执行：当前 handler 返回后，外层 Next 循环
// 不再继续。
func (e *Execution) Block(err error) {
	e.Err = err
	e.index = len(e.handlers)
}
