package mcp

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/pi/extension"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
)

// envReferencePattern 与 config 层的 ${NAME} 引用定义保持一致。
var envReferencePattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// ServerOptions 是单个 MCP Server 的装配选项，字段与 config 声明一一
// 对应；transport 在本包内部构造，装配层只做字段拷贝。
type ServerOptions struct {
	Name      string
	Transport string // "http" 或 "stdio"

	URL       string            // http: 端点 URL
	HeaderEnv map[string]string // http: header 名 → 环境变量名

	Command string            // stdio: 命令
	Args    []string          // stdio: 命令参数
	Env     map[string]string // stdio: 环境覆盖（支持 ${NAME} 引用）
	CWD     string            // stdio: 工作目录

	Timeout    time.Duration
	AllowTools []string
	ToolPrefix string
}

// New 批量装配扩展：环境引用在此取值，stdio 按 Runner 后端选择继承
// 宿主环境直拉或经沙箱包装拉起；任一 server 失败即整体失败。
func New(opts []ServerOptions, root tools.Root, runner sandbox.Runner) (extension.Extensions, error) {
	extensions := make(extension.Extensions, 0, len(opts))
	for _, options := range opts {
		ext, err := newServerExtension(options, root, runner)
		if err != nil {
			return nil, fmt.Errorf("create MCP server extension %q: %w", options.Name, err)
		}
		extensions = append(extensions, ext)
	}
	return extensions, nil
}

// newServerExtension 根据 transport 声明构造 HTTP 或 stdio 传输，并组装
// 单个扩展。stdio host 继承宿主环境；沙箱后端通过 Runner 包装拉起。
func newServerExtension(options ServerOptions, root tools.Root, runner sandbox.Runner) (extension.Extension, error) {
	var transport Transport
	switch options.Transport {
	case "http":
		headers := make(http.Header, len(options.HeaderEnv))
		for headerName, envName := range options.HeaderEnv {
			headers.Set(headerName, os.Getenv(envName))
		}
		httpTransport, err := NewHTTPTransport(HTTPTransportOptions{
			Endpoint: options.URL,
			Headers:  headers,
			Timeout:  options.Timeout,
		})
		if err != nil {
			return nil, err
		}
		transport = httpTransport

	case "stdio":
		keys := make([]string, 0, len(options.Env))
		for key := range options.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		resolvedEnv := make(map[string]string, len(options.Env))
		for _, key := range keys {
			value := options.Env[key]
			if match := envReferencePattern.FindStringSubmatch(value); match != nil {
				envValue, exists := os.LookupEnv(match[1])
				if !exists || strings.TrimSpace(envValue) == "" {
					return nil, fmt.Errorf("mcp.servers.env 引用的环境变量 %q 未设置或为空", match[1])
				}
				value = envValue
			}
			resolvedEnv[key] = value
		}

		if runner.Policy().Backend == "host" {
			childEnv, err := sandbox.HostPayloadEnv(resolvedEnv)
			if err != nil {
				return nil, err
			}
			stdioTransport, err := NewStdioTransport(StdioTransportOptions{
				Command: options.Command,
				Args:    append([]string(nil), options.Args...),
				Env:     childEnv,
				WorkDir: options.CWD,
				Timeout: options.Timeout,
			})
			if err != nil {
				return nil, err
			}
			transport = stdioTransport
			break
		}

		options.Env = resolvedEnv
		buildCommand, err := sandboxBuildCommand(runner, string(root), options)
		if err != nil {
			return nil, err
		}
		stdioTransport, err := NewStdioTransport(StdioTransportOptions{
			Timeout:      options.Timeout,
			BuildCommand: buildCommand,
		})
		if err != nil {
			return nil, err
		}
		transport = stdioTransport

	default:
		return nil, fmt.Errorf("mcp servers.transport %q 非法", options.Transport)
	}

	return newExtension(extensionOptions{
		Name:       options.Name,
		Transport:  transport,
		AllowTools: options.AllowTools,
		ToolPrefix: options.ToolPrefix,
	})
}
