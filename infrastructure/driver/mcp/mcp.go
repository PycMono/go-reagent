// Package mcp 把 config 里声明的 MCP Server 装配为 pi Agent 扩展：
// 解析 header_env 环境变量为真实请求头，创建 Extension 并注册进
// agent_extensions 组。配置校验已在 config.Load 完成，本包零校验。
package mcp

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
	"go.uber.org/fx"
)

type ExtensionsOut struct {
	fx.Out
	Extensions []pi.Extension `group:"agent_extensions,flatten"`
}

func NewExtensions(cfg *config.Config) (ExtensionsOut, error) {
	out := ExtensionsOut{}
	for _, server := range cfg.MCP.Servers {
		if !server.Enabled {
			continue
		}

		headers := make(http.Header, len(server.HeaderEnv))
		// config.Load 已保证 header_env 引用的环境变量存在且非空。
		for headerName, envName := range server.HeaderEnv {
			headers.Set(headerName, os.Getenv(envName))
		}

		extension, err := pimcp.NewExtension(pimcp.ExtensionOptions{
			Name:       server.Name,
			Endpoint:   server.URL,
			Headers:    headers,
			Timeout:    time.Duration(server.Timeout) * time.Second,
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
