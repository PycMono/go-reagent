// Package web assembles the long-running browser chat application separately
// from the one-shot CLI application package.
package web

import (
	"errors"
	"net"
	"strings"

	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/infrastructure/driver/openmeteo"
	infrastructureweb "github.com/PycMono/go-reagent/infrastructure/web"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

var agentRegister = fx.Options(
	pi.CoreRegister,
	pi.ReadOnlyToolsRegister,
	chattools.Register,
	openmeteo.Register,
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
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("web server http.host must be a loopback address")
	}
	return nil
}
