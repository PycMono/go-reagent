package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/extension"
)

const mcpClientVersion = "1"

var toolNamePartPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type ExtensionOptions struct {
	Name string
	// Transport 是已构造完成的 Transport（HTTP 或 stdio），由基础设施
	// 装配层根据配置创建；NewExtension 要求其非 nil。
	Transport  Transport
	AllowTools []string
	ToolPrefix string
}

type extensionClient interface {
	Initialize(context.Context) error
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, json.RawMessage) (CallToolResult, error)
	Close(context.Context) error
}

type mcpExtension struct {
	name       string
	allowTools []string
	toolPrefix string
	client     extensionClient
}

func NewExtension(options ExtensionOptions) (extension.Extension, error) {
	normalized, err := normalizeExtensionOptions(options)
	if err != nil {
		return nil, err
	}
	client, err := NewClient(normalized.Transport, "go-reagent", mcpClientVersion)
	if err != nil {
		return nil, fmt.Errorf("create MCP extension %q client: %w", normalized.Name, err)
	}
	return buildExtension(normalized, client), nil
}

func buildExtension(options ExtensionOptions, client extensionClient) *mcpExtension {
	return &mcpExtension{
		name:       "mcp:" + options.Name,
		allowTools: append([]string(nil), options.AllowTools...),
		toolPrefix: options.ToolPrefix,
		client:     client,
	}
}

func normalizeExtensionOptions(options ExtensionOptions) (ExtensionOptions, error) {
	if options.Transport == nil {
		return ExtensionOptions{}, errors.New("MCP extension transport is required")
	}
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" || !toolNamePartPattern.MatchString(options.Name) {
		return ExtensionOptions{}, errors.New("MCP extension name is invalid")
	}
	options.ToolPrefix = strings.TrimSpace(options.ToolPrefix)
	if options.ToolPrefix != "" && !toolNamePartPattern.MatchString(options.ToolPrefix) {
		return ExtensionOptions{}, errors.New("MCP tool prefix is invalid")
	}
	if len(options.AllowTools) == 0 {
		return ExtensionOptions{}, errors.New("MCP extension allow_tools must not be empty")
	}
	seen := make(map[string]struct{}, len(options.AllowTools))
	allowed := make([]string, 0, len(options.AllowTools))
	for _, rawName := range options.AllowTools {
		name := strings.TrimSpace(rawName)
		if name == "" || !toolNamePartPattern.MatchString(name) {
			return ExtensionOptions{}, errors.New("MCP extension allowed tool name is invalid")
		}
		if _, exists := seen[name]; exists {
			return ExtensionOptions{}, fmt.Errorf("MCP extension allowed tool %q is duplicated", name)
		}
		seen[name] = struct{}{}
		allowed = append(allowed, name)
	}
	options.AllowTools = allowed
	return options, nil
}

func (e *mcpExtension) Name() string { return e.name }

func (e *mcpExtension) Register(ctx context.Context, api extension.API) error {
	if err := e.client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize extension %q: %w", e.name, err)
	}
	remoteTools, err := e.client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("discover tools for extension %q: %w", e.name, err)
	}
	byName := make(map[string]Tool, len(remoteTools))
	for _, remote := range remoteTools {
		byName[remote.Name] = remote
	}
	proxies := make([]*proxyTool, 0, len(e.allowTools))
	for _, allowedName := range e.allowTools {
		remote, exists := byName[allowedName]
		if !exists {
			return fmt.Errorf("extension %q did not expose required tool %q", e.name, allowedName)
		}
		if remote.InputSchema == nil {
			return fmt.Errorf("extension %q tool %q has no input schema", e.name, allowedName)
		}
		exposedName := remote.Name
		if e.toolPrefix != "" {
			exposedName = e.toolPrefix + "_" + remote.Name
		}
		proxies = append(proxies, newProxyTool(e.client, remote, exposedName))
	}
	sort.Slice(proxies, func(i, j int) bool {
		return proxies[i].Definition().Name < proxies[j].Definition().Name
	})
	for _, proxy := range proxies {
		if err := api.RegisterTool(proxy); err != nil {
			return fmt.Errorf("register extension %q tool %q: %w", e.name, proxy.Definition().Name, err)
		}
	}
	return nil
}

func (e *mcpExtension) Close(ctx context.Context) error {
	return e.client.Close(ctx)
}

var _ extension.Extension = (*mcpExtension)(nil)
var _ extension.Closer = (*mcpExtension)(nil)
