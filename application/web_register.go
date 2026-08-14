package application

import (
	"errors"
	"net"
	"strings"

	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/infrastructure"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

// WebRegister assembles the long-running local chat server without the
// one-shot CLI prompt or Agent lifecycle.
var WebRegister = fx.Options(
	pi.Register,
	infrastructure.WebRegister,
	conversation.Register,
	chatservice.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		NewWorkDir,
	),
	fx.Invoke(validateWebConfig),
)

func validateWebConfig(cfg *config.Config) error {
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
