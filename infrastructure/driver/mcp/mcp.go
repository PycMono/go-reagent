// Package mcp 把 config 里声明的 MCP Server 装配为 pi Agent 扩展：
// 按必填 transport 分支创建 HTTP 或 stdio Transport，解析 header_env /
// env 环境引用为真实值，创建 Extension 并注册进 agent_extensions 组。
// 配置校验已在 config.Load 完成，本包不做结构性校验，只保留装配期
// 环境引用的 fail-fast。
package mcp

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/extension"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
	"go.uber.org/fx"
)

// envReferencePattern 与 config 层的 ${NAME} 引用定义保持一致。
var envReferencePattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

type ExtensionsOut struct {
	fx.Out
	Extensions []extension.Extension `group:"agent_extensions,flatten"`
}

// NewExtensions 装配 MCP 扩展。runner 来自 Runner（fx.Decorate 后
// 的实际生效后端）：host 维持现状路径，沙箱后端经 BuildArgv 回调拉起（设计 §7）。
func NewExtensions(cfg *config.Config, root tools.Root, runner sandbox.Runner) (ExtensionsOut, error) {
	out := ExtensionsOut{}
	for _, server := range cfg.MCP.Servers {
		if !server.Enabled {
			continue
		}

		// Transport 分支不设置 default：config.Load 已保证只可能进入
		// http 或 stdio。
		var transport pimcp.Transport
		var err error
		switch server.Transport {
		case "http":
			transport, err = newHTTPTransport(server)
		case "stdio":
			transport, err = newStdioTransport(runner, string(root), server)
		default:
			err = fmt.Errorf("mcp.servers.transport %q 非法", server.Transport)
		}
		if err != nil {
			return ExtensionsOut{}, fmt.Errorf("create MCP server extension %q transport: %w", server.Name, err)
		}

		extension, err := pimcp.NewExtension(pimcp.ExtensionOptions{
			Name:       server.Name,
			Transport:  transport,
			AllowTools: append([]string(nil), server.AllowTools...),
			ToolPrefix: server.ToolPrefix,
		})
		if err != nil {
			return ExtensionsOut{}, fmt.Errorf("create MCP server extension %q: %w", server.Name, err)
		}

		out.Extensions = append(out.Extensions, extension)
	}

	return out, nil
}

func newHTTPTransport(server config.MCPServerConfig) (pimcp.Transport, error) {
	headers := make(http.Header, len(server.HeaderEnv))
	// config.Load 已保证 header_env 引用的环境变量存在且非空。
	for headerName, envName := range server.HeaderEnv {
		headers.Set(headerName, os.Getenv(envName))
	}
	return pimcp.NewHTTPTransport(pimcp.HTTPTransportOptions{
		Endpoint: server.URL,
		Headers:  headers,
		Timeout:  time.Duration(server.Timeout) * time.Second,
	})
}

func newStdioTransport(runner sandbox.Runner, workspaceRoot string, server config.MCPServerConfig) (pimcp.Transport, error) {
	if runner.Policy().Backend == "host" {
		// host 现状路径：完全兼容（含空 cwd 继承，§5.6）。
		env, err := resolveChildEnv(server.Env)
		if err != nil {
			return nil, err
		}
		return pimcp.NewStdioTransport(pimcp.StdioTransportOptions{
			Command: server.Command,
			Args:    append([]string(nil), server.Args...),
			Env:     env,
			WorkDir: server.CWD,
			Timeout: time.Duration(server.Timeout) * time.Second,
		})
	}
	return sandboxStdioTransport(runner, workspaceRoot, server)
}

// sandboxStdioTransport 沙箱路径：cwd 空→归一化为 WorkspaceRoot，显式 cwd
// 必须位于工作区内；env 只包含固定基础项与 server 显式配置，不继承宿主。
func sandboxStdioTransport(runner sandbox.Runner, workspaceRoot string, server config.MCPServerConfig) (pimcp.Transport, error) {
	canonicalRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("解析工作区真实路径失败: %w", err)
	}
	cwd := strings.TrimSpace(server.CWD)
	if cwd == "" {
		cwd = canonicalRoot
	} else {
		cwd, err = sandbox.ResolveWorkDir(cwd, canonicalRoot)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q cwd 被拒: %w", server.Name, err)
		}
	}
	resolved, err := resolveEnvOverrides(server.Env)
	if err != nil {
		return nil, err
	}
	extras := make([]string, 0, len(resolved))
	for _, name := range sortedKeys(resolved) {
		extras = append(extras, name+"="+resolved[name])
	}
	tmpDir, err := sandboxTmpDir(runner.Policy().Backend, canonicalRoot)
	if err != nil {
		return nil, err
	}
	payload, err := sandbox.BuildSandboxPayloadEnv(canonicalRoot, tmpDir, extras)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q 环境构造被拒: %w", server.Name, err)
	}
	spec := sandbox.CommandSpec{WorkDir: cwd, PayloadEnv: payload}
	return pimcp.NewStdioTransport(pimcp.StdioTransportOptions{
		Timeout: time.Duration(server.Timeout) * time.Second,
		BuildCommand: func() (*exec.Cmd, error) {
			return runner.BuildArgv(append([]string{server.Command}, server.Args...), spec)
		},
	})
}

// sandboxTmpDir 返回所选后端的 TMPDIR 策略（§5.4）；host 不走此路径。
func sandboxTmpDir(backend, workspaceRoot string) (string, error) {
	switch backend {
	case "bubblewrap":
		return "/tmp", nil
	case "seatbelt":
		return filepath.Join(workspaceRoot, ".tmp"), nil
	default:
		return "", fmt.Errorf("未知沙箱后端 %q", backend)
	}
}

// resolveChildEnv 合并子进程环境：继承父进程环境，再由配置中的同名键
// 覆盖。不修改父进程环境。`${NAME}` 完整引用在装配期取值；沿用 config
// 层“存在且非空”规则，缺失时 fail-fast，错误只包含变量名。
func resolveChildEnv(overrides map[string]string) ([]string, error) {
	resolved, err := resolveEnvOverrides(overrides)
	if err != nil {
		return nil, err
	}
	parent := os.Environ()
	child := make([]string, 0, len(parent)+len(resolved))
	for _, entry := range parent {
		key, _, ok := splitEnvEntry(entry)
		if !ok {
			continue
		}
		if _, overridden := resolved[key]; overridden {
			continue
		}
		child = append(child, entry)
	}
	for _, name := range sortedKeys(resolved) {
		child = append(child, name+"="+resolved[name])
	}
	return child, nil
}

// resolveEnvOverrides 解析配置中的 ${NAME} 引用为真实值（沿用 config 层
// "存在且非空"规则，缺失时 fail-fast，错误只包含变量名）。
func resolveEnvOverrides(overrides map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(overrides))
	for _, name := range sortedKeys(overrides) {
		value := overrides[name]
		if match := envReferencePattern.FindStringSubmatch(value); match != nil {
			envValue, exists := os.LookupEnv(match[1])
			if !exists || strings.TrimSpace(envValue) == "" {
				return nil, fmt.Errorf("mcp.servers.env 引用的环境变量 %q 未设置或为空", match[1])
			}
			value = envValue
		}
		resolved[name] = value
	}
	return resolved, nil
}

func splitEnvEntry(entry string) (string, string, bool) {
	index := strings.IndexByte(entry, '=')
	if index <= 0 {
		return "", "", false
	}
	return entry[:index], entry[index+1:], true
}

func sortedKeys(mapping map[string]string) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
