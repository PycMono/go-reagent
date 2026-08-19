package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

type Transport interface {
	Send(context.Context, Request) (Response, error)
	Close(context.Context) error
}

type Client struct {
	transport Transport
	nextID    atomic.Int64

	stateMu     sync.RWMutex
	initialized bool
	closed      bool
	name        string
	version     string
}

func NewClient(transport Transport, name, version string) (*Client, error) {
	if transport == nil {
		return nil, errors.New("mcp transport is required")
	}
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
	if err := decodeResult("initialize", response.Result, &result); err != nil {
		return err
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
		if err := decodeResult("tools/list", response.Result, &result); err != nil {
			return nil, err
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
	arguments, err := decodeArguments(rawArguments)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("mcp tools/call: invalid arguments: %w", err)
	}
	response, err := client.send(ctx, "tools/call", callToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallToolResult{}, err
	}
	var result CallToolResult
	if err := decodeResult("tools/call", response.Result, &result); err != nil {
		return CallToolResult{}, err
	}
	return result, nil
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

func decodeResult(method string, raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("mcp %s: decode result: %w", method, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("mcp %s: decode trailing result: %w", method, err)
		}
		return fmt.Errorf("mcp %s: result contains trailing JSON", method)
	}
	return nil
}

func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil {
		return nil, err
	}
	if arguments == nil {
		return nil, errors.New("arguments must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("arguments contain trailing JSON")
	}
	return arguments, nil
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
