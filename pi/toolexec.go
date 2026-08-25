package pi

import (
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Tool 执行域的公开 API 兼容别名，实现位于 pi/toolexec。
// 新代码可以直接使用 pi/toolexec 的类型。
type (
	ToolResult         = toolexec.Result
	ToolEvent          = toolexec.Event
	ToolEventPhase     = toolexec.EventPhase
	ToolEventObserver  = toolexec.EventObserver
	ToolRuntime        = toolexec.Executor
	ToolRuntimeOptions = toolexec.ExecutorOptions
	Scheduler          = toolexec.Scheduler
)

const (
	ToolEventStart  = toolexec.EventStart
	ToolEventUpdate = toolexec.EventUpdate
	ToolEventEnd    = toolexec.EventEnd
)

var (
	NewToolStart  = toolexec.NewStartEvent
	NewToolUpdate = toolexec.NewUpdateEvent
	NewToolEnd    = toolexec.NewEndEvent
	NewScheduler  = toolexec.NewScheduler
)

// NewToolRuntime 是 toolexec.NewExecutor 的兼容包装。
func NewToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
	return toolexec.NewExecutor(options)
}
