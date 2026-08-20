package web

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/mcp"
	"go.uber.org/fx"
)

type McpExtensionsOut struct {
	fx.Out
	Extensions []pi.Extension `group:"agent_extensions,flatten"`
}

func RegisterMCPExtensions(cfg *config.Config) (McpExtensionsOut, error) {
	out := McpExtensionsOut{}
	for _, server := range cfg.MCP.Servers {
		if !server.Enabled {
			continue
		}

		headers := make(http.Header, len(server.HeaderEnv))
		for headerName, envName := range server.HeaderEnv {
			value, exists := os.LookupEnv(envName)
			if !exists || strings.TrimSpace(value) == "" {
				return McpExtensionsOut{}, fmt.Errorf(
					"MCP server %q header %q requires non-empty environment variable %q",
					server.Name, headerName, envName,
				)
			}
			headers.Set(headerName, value)
		}

		extension, err := mcp.NewExtension(mcp.ExtensionOptions{
			Name:       server.Name,
			Endpoint:   server.URL,
			Headers:    headers,
			Timeout:    time.Duration(server.Timeout) * time.Second,
			AllowTools: append([]string(nil), server.AllowTools...),
			ToolPrefix: server.ToolPrefix,
		})
		if err != nil {
			return McpExtensionsOut{}, fmt.Errorf("create MCP server extension %q: %w", server.Name, err)
		}

		out.Extensions = append(out.Extensions, extension)
	}

	return out, nil
}
