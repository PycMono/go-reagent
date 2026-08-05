package reagent

// ErrorCode is a stable machine-readable SDK error category.
type ErrorCode string

const (
	ErrorCodeUnknown          ErrorCode = "unknown"
	ErrorCodeConfigLoad       ErrorCode = "config_load_failed"
	ErrorCodeConfigInvalid    ErrorCode = "config_invalid"
	ErrorCodeInitialization   ErrorCode = "initialization_failed"
	ErrorCodeRequestInvalid   ErrorCode = "request_invalid"
	ErrorCodeWorkspaceInvalid ErrorCode = "workspace_invalid"
	ErrorCodeAIGeneration     ErrorCode = "ai_generation_failed"
	ErrorCodeToolRuntime      ErrorCode = "tool_runtime_failed"
	ErrorCodeCanceled         ErrorCode = "canceled"
	ErrorCodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	ErrorCodeClosed           ErrorCode = "agent_closed"
	ErrorCodeInternal         ErrorCode = "internal"
)
