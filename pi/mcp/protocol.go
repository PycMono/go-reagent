package mcp

import "encoding/json"

const ProtocolVersion = "2025-03-26"

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

type implementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    map[string]any     `json:"capabilities"`
	ClientInfo      implementationInfo `json:"clientInfo"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    map[string]any     `json:"capabilities"`
	ServerInfo      implementationInfo `json:"serverInfo"`
}

type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type listToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type listToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content           []Content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}
