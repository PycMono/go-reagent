// Package web assembles the long-running browser chat application separately
// from the one-shot CLI application package.
package web

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	infrastructureweb "github.com/PycMono/go-reagent/infrastructure/web"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

const exaMCPEndpoint = "https://mcp.exa.ai/mcp"

var agentRegister = fx.Options(
	pi.CoreRegister,
	pi.ReadOnlyToolsRegister,
	chattools.Register,
	fx.Supply(pi.ThinkingEnabled(false)),
)

var Register = fx.Options(
	agentRegister,
	infrastructureweb.Register,
	conversation.Register,
	chatservice.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		NewChatWorkDir,
		agentprofiledriver.NewCatalog,
		newMCPExtensions,
	),
	fx.Invoke(validateConfig),
)

func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("web config is required")
	}
	if !cfg.Conversation.Enabled {
		return errors.New("web server requires conversation.enabled=true")
	}
	host := strings.Trim(strings.TrimSpace(cfg.HTTP.Host), "[]")
	if strings.EqualFold(host, "localhost") {
		return validateRequiredExa(cfg.MCP)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("web server http.host must be a loopback address")
	}
	return validateRequiredExa(cfg.MCP)
}

func validateRequiredExa(mcpConfig config.MCPConfig) error {
	for _, server := range mcpConfig.Servers {
		if server.Name != "exa" {
			continue
		}
		if !server.Enabled || !server.Required {
			return errors.New("required Exa MCP server must be enabled")
		}
		if server.URL != exaMCPEndpoint {
			return fmt.Errorf("required Exa MCP URL must be %s", exaMCPEndpoint)
		}
		if server.ToolPrefix != "" {
			return errors.New("required Exa MCP tool_prefix must be empty")
		}
		if len(server.AllowTools) != 2 ||
			!slices.Contains(server.AllowTools, "web_search_exa") ||
			!slices.Contains(server.AllowTools, "web_fetch_exa") {
			return errors.New("required Exa MCP must allow exactly web_search_exa and web_fetch_exa")
		}
		if len(server.HeaderEnv) != 1 || server.HeaderEnv["X-Api-Key"] != "EXA_API_KEY" {
			return errors.New("required Exa MCP X-Api-Key must use EXA_API_KEY")
		}
		return nil
	}
	return errors.New("web server requires required Exa MCP configuration")
}
