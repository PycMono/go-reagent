package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/pi"
)

const mcpClientVersion = "1"

var toolNamePartPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type ExtensionOptions struct {
	Name       string
	Endpoint   string
	Headers    http.Header
	Timeout    time.Duration
	AllowTools []string
	ToolPrefix string
	HTTPClient *http.Client
}

type extensionClient interface {
	Initialize(context.Context) error
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, json.RawMessage) (CallToolResult, error)
	Close(context.Context) error
}

type Extension struct {
	name       string
	allowTools []string
	toolPrefix string
	client     extensionClient
}

func NewExtension(options ExtensionOptions) (pi.Extension, error) {
	normalized, err := normalizeExtensionOptions(options)
	if err != nil {
		return nil, err
	}
	transport, err := NewHTTPTransport(HTTPTransportOptions{
		Endpoint:   normalized.Endpoint,
		Headers:    normalized.Headers,
		Timeout:    normalized.Timeout,
		HTTPClient: normalized.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("create MCP extension %q transport: %w", normalized.Name, err)
	}
	client, err := NewClient(transport, "go-reagent", mcpClientVersion)
	if err != nil {
		return nil, fmt.Errorf("create MCP extension %q client: %w", normalized.Name, err)
	}
	return buildExtension(normalized, client), nil
}

func newExtensionWithClient(options ExtensionOptions, client extensionClient) (*Extension, error) {
	normalized, err := normalizeExtensionOptions(options)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("MCP extension client is required")
	}
	return buildExtension(normalized, client), nil
}

func buildExtension(options ExtensionOptions, client extensionClient) *Extension {
	return &Extension{
		name:       "mcp:" + options.Name,
		allowTools: append([]string(nil), options.AllowTools...),
		toolPrefix: options.ToolPrefix,
		client:     client,
	}
}

func normalizeExtensionOptions(options ExtensionOptions) (ExtensionOptions, error) {
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

func (extension *Extension) Name() string { return extension.name }

func (extension *Extension) Register(ctx context.Context, api pi.ExtensionAPI) error {
	if err := extension.client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize extension %q: %w", extension.name, err)
	}
	remoteTools, err := extension.client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("discover tools for extension %q: %w", extension.name, err)
	}
	byName := make(map[string]Tool, len(remoteTools))
	for _, remote := range remoteTools {
		byName[remote.Name] = remote
	}
	proxies := make([]*proxyTool, 0, len(extension.allowTools))
	for _, allowedName := range extension.allowTools {
		remote, exists := byName[allowedName]
		if !exists {
			return fmt.Errorf("extension %q did not expose required tool %q", extension.name, allowedName)
		}
		if remote.InputSchema == nil {
			return fmt.Errorf("extension %q tool %q has no input schema", extension.name, allowedName)
		}
		exposedName := remote.Name
		if extension.toolPrefix != "" {
			exposedName = extension.toolPrefix + "_" + remote.Name
		}
		proxies = append(proxies, newProxyTool(extension.client, remote, exposedName))
	}
	sort.Slice(proxies, func(i, j int) bool {
		return proxies[i].Definition().Name < proxies[j].Definition().Name
	})
	for _, proxy := range proxies {
		if err := api.RegisterTool(proxy); err != nil {
			return fmt.Errorf("register extension %q tool %q: %w", extension.name, proxy.Definition().Name, err)
		}
	}
	return nil
}

func (extension *Extension) Close(ctx context.Context) error {
	return extension.client.Close(ctx)
}

var _ pi.Extension = (*Extension)(nil)
var _ pi.ExtensionCloser = (*Extension)(nil)
