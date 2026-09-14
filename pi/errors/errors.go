// Package errors defines all stable error codes and coded errors used by Pi.
//
// 它是 pi 全域唯一的错误码与错误值框架所在（引用别名统一为 pierrors）。
// 分层规则：
//   - 稳定错误值（CodeError/CodeOf）只允许定义在本包；
//   - 需要跨包识别的领域错误类型（如 loopdetect.Error、governor 的
//     limitError）随领域包定义并实现 Code() int——集中到本包会造成
//     包循环并污染通用框架；
//   - 业务/HTTP 层的 BizError 码在 common/errors，不经本包。
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
)

// CodeError 携带稳定数字码与人类文案的错误值，对应 duserr 的
// CodeError/codeError：Code/Message 取码与文案，Wrap 挂原因链（Unwrap），
// Is 按码匹配，Params 填充文案占位符。
// 与 duserr 的差异：不做 %+v 栈采集与 Format 定制——pi 的错误最终进
// 工具输出与 termination 归因，不需要栈帧。
type CodeError struct {
	code  int
	msg   string
	cause error
}

// New 声明一个稳定码错误值，对应 duserr.NewBizError(code int, msg string)。
func New(code int, msg string) *CodeError {
	return &CodeError{code: code, msg: msg}
}

func (e *CodeError) Code() int       { return e.code }
func (e *CodeError) Message() string { return e.msg }

func (e *CodeError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%d|%s: %v", e.code, e.msg, e.cause)
	}
	return fmt.Sprintf("%d|%s", e.code, e.msg)
}

func (e *CodeError) Unwrap() error { return e.cause }

// Wrap 复用稳定码与文案，把原始原因挂到错误链上。
func (e *CodeError) Wrap(err error) error {
	if err == nil {
		return nil
	}
	return &CodeError{code: e.code, msg: e.msg, cause: err}
}

// Params 填充文案中的占位符（fmt.Sprintf 语义），码与原因链不变。
func (e *CodeError) Params(args ...any) *CodeError {
	return &CodeError{code: e.code, msg: fmt.Sprintf(e.msg, args...), cause: e.cause}
}

// Is 按码匹配（对应 duserr 的按码 Is），其余目标透传原因链。
func (e *CodeError) Is(target error) bool {
	if coded, ok := target.(*CodeError); ok {
		return coded.code == e.code
	}
	return stderrors.Is(e.Unwrap(), target)
}

// Match 判断 err 链是否归属本码——判断码的首选写法：
//
//	pierrors.ErrAITransient.Match(err) || pierrors.ErrAIRateLimited.Match(err)
//
// 经 CodeOf 归一，raw context.Canceled/DeadlineExceeded 与领域 coded
// 错误同样命中。
func (e *CodeError) Match(err error) bool {
	return CodeOf(err) == e.code
}

// coded 由领域错误类型实现（如 loopdetect.Error、governor limitError），
// 声明其归属的稳定码，即被 CodeOf 识别。
type coded interface {
	error
	Code() int
}

// 稳定码错误值集中声明（对应 bizerrors 的 var ErrXxx = duserr.NewBizError(code, msg)）。
// 码分段：10000 通用；20000 AI；30000 工具；40000 取消/超时；
// 50000 运行控制；60000 生命周期；90000 内部。
var (
	// 通用
	ErrUnknown          = New(10000, "未知错误")
	ErrInitialization   = New(10001, "初始化失败")
	ErrRequestInvalid   = New(10002, "请求无效")
	ErrWorkspaceInvalid = New(10003, "工作区无效")

	// AI 生成
	ErrAIGeneration      = New(20000, "AI 生成失败")
	ErrAITransient       = New(20001, "AI 调用暂时性失败")
	ErrAIRateLimited     = New(20002, "AI 调用被限频")
	ErrAIContextOverflow = New(20003, "上下文超出模型窗口")
	ErrAIUnauthorized    = New(20004, "AI 平台鉴权失败")
	ErrAIQuotaExceeded   = New(20005, "AI 平台配额不足")
	ErrAIInvalidRequest  = New(20006, "AI 请求参数无效")

	// 工具
	ErrToolRuntime          = New(30000, "工具执行失败")
	ErrToolInvalidArguments = New(30001, "工具参数无效")
	ErrToolResourceNotFound = New(30002, "工具资源不存在")
	ErrToolPermissionDenied = New(30003, "工具权限不足")
	ErrToolEditNoMatch      = New(30004, "未找到编辑目标")
	ErrToolEditNotUnique    = New(30005, "编辑目标不唯一")
	ErrToolTimeout          = New(30006, "工具执行超时")
	ErrToolPanic            = New(30007, "工具执行异常")

	// 取消与超时
	ErrCanceled         = New(40000, "已取消")
	ErrDeadlineExceeded = New(40001, "已超时")

	// 运行控制
	ErrRunLimitExceeded = New(50000, "运行预算超限")
	ErrRunLoopDetected  = New(50001, "检测到循环调用")

	// 生命周期
	ErrClosed = New(60000, "Agent 已关闭")

	// 内部
	ErrInternal = New(90000, "内部错误")
)

// CodeOf 沿错误链提取稳定码；nil 或未分类错误返回 0。
// context sentinel（Canceled/DeadlineExceeded）归入对应稳定码。
func CodeOf(err error) int {
	if err == nil {
		return 0
	}
	var classified coded
	if stderrors.As(err, &classified) {
		return classified.Code()
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return ErrCanceled.Code()
	case stderrors.Is(err, context.DeadlineExceeded):
		return ErrDeadlineExceeded.Code()
	}
	return 0
}

// AsCodeError 沿错误链找到第一个 CodeError 并返回，
// 对应 duserr 的 AsBizError/UnWrapBizError。
func AsCodeError(err error) (*CodeError, bool) {
	var classified *CodeError
	if stderrors.As(err, &classified) {
		return classified, true
	}
	return nil, false
}

// HTTPStatusToCode 将 provider HTTP 状态码归入通用 AI 错误值。
func HTTPStatusToCode(status int) *CodeError {
	switch {
	case status == http.StatusTooManyRequests:
		return ErrAIRateLimited
	case status == http.StatusRequestTimeout,
		status == http.StatusConflict,
		status >= http.StatusInternalServerError:
		return ErrAITransient
	case status == http.StatusUnauthorized,
		status == http.StatusForbidden:
		return ErrAIUnauthorized
	case status == http.StatusBadRequest:
		return ErrAIInvalidRequest
	default:
		return ErrAIGeneration
	}
}

// IsTransientNetwork 判断是否为可重试的传输层错误（EOF / 网络错误）。
func IsTransientNetwork(err error) bool {
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return stderrors.As(err, &networkError)
}

// ClassifyTool 在工具边界把 sentinel 错误翻译成稳定码错误值。
// 已携带稳定码（CodeError 或领域 coded）的错误不再重分类，
// 避免具体码被兜底的 tool_runtime_failed 覆盖。
func ClassifyTool(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := AsCodeError(err); ok {
		return err
	}
	var classified coded
	if stderrors.As(err, &classified) {
		return err
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return ErrCanceled.Wrap(err)
	case stderrors.Is(err, context.DeadlineExceeded):
		return ErrToolTimeout.Wrap(err)
	case stderrors.Is(err, fs.ErrNotExist):
		return ErrToolResourceNotFound.Wrap(err)
	case stderrors.Is(err, fs.ErrPermission):
		return ErrToolPermissionDenied.Wrap(err)
	default:
		return ErrToolRuntime.Wrap(err)
	}
}
