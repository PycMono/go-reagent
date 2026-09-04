package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// maxMessageBytes 是单条消息（HTTP 响应体 / stdio JSONL 行）的上限。
const maxMessageBytes int64 = 16 << 20

// DefaultTimeout 是未配置 Timeout（<=0）时单次请求的默认期限，与
// go-reagent 服务的默认配置（config.example.json）保持一致。
const DefaultTimeout = 60 * time.Second

// Transport 是消息传输抽象：负责把单条 JSON-RPC 消息送达远端并取回
// 响应（streamable HTTP、stdio 等各自实现）。不感知 MCP 协议语义。
type Transport interface {
	Send(context.Context, Request) (Response, error)
	Close(context.Context) error
}

// Request / Response / RPCError 是 JSON-RPC 消息信封，是 Transport
// 契约的收发单元；MCP 方法的参数与结果类型见 client.go。
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// transportError 是 transport 层错误的统一形状：HTTP 用 status，
// 远端 JSON-RPC 错误用 code，本地操作失败用 kind/cause。
type transportError struct {
	op     string
	kind   string
	status int
	code   int
	cause  error
}

func (err *transportError) Error() string {
	switch {
	case err.status != 0:
		return fmt.Sprintf("mcp %s: HTTP status %d", err.op, err.status)
	case err.code != 0:
		return fmt.Sprintf("mcp %s: remote JSON-RPC error code %d", err.op, err.code)
	case err.cause != nil:
		return fmt.Sprintf("mcp %s: %s: %v", err.op, err.kind, err.cause)
	default:
		return fmt.Sprintf("mcp %s: %s", err.op, err.kind)
	}
}

func (err *transportError) Unwrap() error { return err.cause }

// ErrorCode 把 transport 失败归入 pi 的通用错误码（pierrors.ErrorCodeOf
// 经 CodedError 读取）：本地读写/远端错误归 tool_runtime_failed，关闭中
// 的请求归 agent_closed；取消与超时不在此判断，由 cause 链命中
// canceled / deadline_exceeded。
func (err *transportError) ErrorCode() pierrors.ErrorCode {
	if err.kind == "transport closed" {
		return pierrors.ErrorCodeClosed
	}
	return pierrors.ErrorCodeToolRuntime
}
