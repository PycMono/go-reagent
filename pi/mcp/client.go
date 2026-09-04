package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Client 是 MCP 规范中 Client 角色的实现：维护与单个 MCP Server 的
// 协议会话（initialize 握手、请求 ID、tools 分页、结果校验），通过
// Transport 收发消息。一个 Client 对应一个 server 连接。
// 本文件同时定义各方法的消息形状（参数 / 结果）；JSON-RPC 信封在
// transport.go。
type Client struct {
	transport Transport
	nextID    atomic.Int64

	stateMu     sync.RWMutex
	initialized bool
	closed      bool
	name        string
	version     string
}

const ProtocolVersion = "2025-03-26"

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

// text 返回结果的可读文本：优先拼接全部 text 内容块；没有文本内容时
// 序列化 structuredContent 兜底；两者皆无时返回空串。
func (result CallToolResult) text() (string, error) {
	texts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if content.Type != "text" {
			return "", fmt.Errorf("unsupported content type %q", content.Type)
		}
		texts = append(texts, content.Text)
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}

	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("encode structured content: %w", err)
		}
		return string(data), nil
	}

	return "", nil
}

func NewClient(transport Transport, name, version string) (*Client, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return nil, errors.New("mcp client name and version are required")
	}

	return &Client{transport: transport, name: name, version: version}, nil
}

func (client *Client) Initialize(ctx context.Context) error {
	client.stateMu.Lock()
	defer client.stateMu.Unlock()

	if client.closed {
		return errors.New("mcp client is closed")
	}
	if client.initialized {
		return nil
	}

	response, err := client.send(ctx, "initialize", initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      implementationInfo{Name: client.name, Version: client.version},
	})
	if err != nil {
		return err
	}

	var result initializeResult
	if err := decodeStrictJSON(response.Result, &result); err != nil {
		return fmt.Errorf("mcp initialize: decode result: %w", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("mcp initialize: unsupported protocol version %q", result.ProtocolVersion)
	}

	if _, err := client.transport.Send(ctx, Request{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return fmt.Errorf("mcp notifications/initialized: %w", err)
	}

	client.initialized = true
	return nil
}

func (client *Client) ListTools(ctx context.Context) ([]Tool, error) {
	client.stateMu.RLock()
	defer client.stateMu.RUnlock()
	if err := client.ready(); err != nil {
		return nil, err
	}

	var tools []Tool
	seenTools := make(map[string]struct{})
	seenCursors := make(map[string]struct{})
	cursor := ""
	for {
		response, err := client.send(ctx, "tools/list", listToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		var result listToolsResult
		if err := decodeStrictJSON(response.Result, &result); err != nil {
			return nil, fmt.Errorf("mcp tools/list: decode result: %w", err)
		}
		for _, tool := range result.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" || name != tool.Name {
				return nil, fmt.Errorf("mcp tools/list: invalid blank or padded tool name")
			}
			if _, exists := seenTools[name]; exists {
				return nil, fmt.Errorf("mcp tools/list: duplicate tool %q", name)
			}
			seenTools[name] = struct{}{}
			tools = append(tools, tool)
		}
		next := strings.TrimSpace(result.NextCursor)
		if next == "" {
			return tools, nil
		}
		if _, exists := seenCursors[next]; exists {
			return nil, fmt.Errorf("mcp tools/list: repeated cursor")
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}
}

func (client *Client) CallTool(ctx context.Context, name string, rawArguments json.RawMessage) (CallToolResult, error) {
	client.stateMu.RLock()
	defer client.stateMu.RUnlock()

	if err := client.ready(); err != nil {
		return CallToolResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return CallToolResult{}, errors.New("mcp tools/call: tool name is required")
	}

	var arguments map[string]any
	if err := decodeStrictJSON(rawArguments, &arguments); err != nil {
		return CallToolResult{}, fmt.Errorf("mcp tools/call: invalid arguments: %w", err)
	}
	if arguments == nil {
		return CallToolResult{}, errors.New("mcp tools/call: arguments must be a JSON object")
	}

	response, err := client.send(ctx, "tools/call", callToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallToolResult{}, err
	}
	var result CallToolResult
	if err := decodeStrictJSON(response.Result, &result); err != nil {
		return CallToolResult{}, fmt.Errorf("mcp tools/call: decode result: %w", err)
	}
	return result, nil
}

func (client *Client) Close(ctx context.Context) error {
	client.stateMu.Lock()
	defer client.stateMu.Unlock()
	if client.closed {
		return nil
	}
	if err := client.transport.Close(ctx); err != nil {
		return fmt.Errorf("mcp client close: %w", err)
	}
	client.closed = true
	return nil
}

func (client *Client) ready() error {
	if client.closed {
		return errors.New("mcp client is closed")
	}
	if !client.initialized {
		return errors.New("mcp client is not initialized")
	}
	return nil
}

func (client *Client) send(ctx context.Context, method string, params any) (Response, error) {
	id := client.nextID.Add(1)
	response, err := client.transport.Send(ctx, Request{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return Response{}, fmt.Errorf("mcp %s: %w", method, err)
	}
	if response.Error != nil {
		return Response{}, fmt.Errorf("mcp %s: remote JSON-RPC error code %d", method, response.Error.Code)
	}
	return response, nil
}

// decodeStrictJSON 解码单个 JSON 值并拒绝尾部多余内容；UseNumber 保证
// map[string]any 中的数字保持 json.Number 精度。
func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing JSON")
	}
	return nil
}
