package loopdetect

import (
	"errors"
	"fmt"
	"slices"

	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// ErrLoopDetected 是行为循环熔断的 sentinel 错误。
var ErrLoopDetected = errors.New("agent tool loop detected")

// Error 携带安全的干预元数据：不保存 hash、参数、结果或模型正文。
type Error struct {
	Pattern   Pattern
	Count     int
	ToolNames []string
}

func (err *Error) Error() string {
	return fmt.Sprintf("%v: pattern=%s count=%d tools=%v", ErrLoopDetected, err.Pattern, err.Count, err.ToolNames)
}

func (err *Error) Unwrap() error { return ErrLoopDetected }

// Code returns the stable Pi error code for loop detection failures.
// Keeping the code on the domain error lets callers classify it through the
// shared pi/errors int-code sentinels without ad-hoc conversions.
func (err *Error) Code() int { return pierrors.ErrRunLoopDetected.Code() }

// NewError 从干预证据构造终止错误；ToolNames 复制并去重排序，保证日志与
// 测试稳定。
func NewError(evidence *Intervention) *Error {
	toolNames := slices.Clone(evidence.ToolNames)
	slices.Sort(toolNames)
	return &Error{
		Pattern:   evidence.Pattern,
		Count:     evidence.Count,
		ToolNames: slices.Compact(toolNames),
	}
}
