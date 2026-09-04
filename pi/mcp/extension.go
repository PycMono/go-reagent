package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/extension"
)

var _ extension.Extension = (*mcpExtension)(nil)
var _ extension.Closer = (*mcpExtension)(nil)

const mcpClientVersion = "1"

var toolNamePartPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type extensionOptions struct {
	Name       string
	Transport  Transport
	AllowTools []string
	ToolPrefix string
}

// newExtension 校验并归一化选项后组装扩展：name/prefix/allowTools 必须
// 匹配 [A-Za-z0-9_-]+，allow_tools 去重后按名称排序（注册顺序由此确定）。
func newExtension(options extensionOptions) (extension.Extension, error) {
	if options.Transport == nil {
		return nil, errors.New("MCP extension transport is required")
	}
	options.Name = strings.TrimSpace(options.Name)
	if !toolNamePartPattern.MatchString(options.Name) {
		return nil, errors.New("MCP extension name is invalid")
	}

	options.ToolPrefix = strings.TrimSpace(options.ToolPrefix)
	if options.ToolPrefix != "" && !toolNamePartPattern.MatchString(options.ToolPrefix) {
		return nil, errors.New("MCP tool prefix is invalid")
	}
	if len(options.AllowTools) == 0 {
		return nil, errors.New("MCP extension allow_tools must not be empty")
	}

	seen := make(map[string]struct{}, len(options.AllowTools))
	allowTools := make([]string, 0, len(options.AllowTools))
	for _, rawName := range options.AllowTools {
		name := strings.TrimSpace(rawName)
		if !toolNamePartPattern.MatchString(name) {
			return nil, errors.New("MCP extension allowed tool name is invalid")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("MCP extension allowed tool %q is duplicated", name)
		}
		seen[name] = struct{}{}
		allowTools = append(allowTools, name)
	}
	sort.Strings(allowTools)

	client, err := NewClient(options.Transport, "go-reagent", mcpClientVersion)
	if err != nil {
		return nil, fmt.Errorf("create MCP extension %q client: %w", options.Name, err)
	}

	return &mcpExtension{
		name:       "mcp:" + options.Name,
		allowTools: allowTools,
		toolPrefix: options.ToolPrefix,
		client:     client,
	}, nil
}

type mcpExtension struct {
	name       string
	allowTools []string
	toolPrefix string
	client     *Client
}

func (e *mcpExtension) Name() string { return e.name }

// Register 初始化远端连接后，把 allow_tools 白名单内的远端工具逐一
// 包装为 proxyTool 注册进 Agent。allowTools 已按名称排序，注册顺序
// 确定且与白名单声明顺序无关。
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

	for _, allowedName := range e.allowTools {
		remote, exists := byName[allowedName]
		if !exists {
			return fmt.Errorf("extension %q did not expose required tool %q", e.name, allowedName)
		}
		if remote.InputSchema == nil {
			return fmt.Errorf("extension %q tool %q has no input schema", e.name, allowedName)
		}
		proxy := newProxyTool(e.client, remote, e.exposedName(remote.Name))
		if err := api.RegisterTool(proxy); err != nil {
			return fmt.Errorf("register extension %q tool %q: %w", e.name, proxy.definition.Name, err)
		}
	}

	return nil
}

// exposedName 返回工具在 Agent 侧的注册名：有前缀时为 prefix_name，
// 用于避免多个 MCP server 的工具重名。
func (e *mcpExtension) exposedName(remoteName string) string {
	if e.toolPrefix == "" {
		return remoteName
	}

	return e.toolPrefix + "_" + remoteName
}

func (e *mcpExtension) Close(ctx context.Context) error {
	return e.client.Close(ctx)
}
