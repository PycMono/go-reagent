package application

import (
	"strings"
	"testing"

	ginsdk "github.com/PycMono/go-gin-sdk"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/config"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

func TestWebRegisterResolvesWebGraphWithoutCLIInputs(t *testing.T) {
	var (
		engine     *gin.Engine
		server     *ginsdk.HTTPServer
		service    *chatservice.Service
		store      conversationrepo.IConversationRepository
		management conversationrepo.IConversationManagementRepository
		page       *pagectl.Controller
	)
	err := fx.ValidateApp(
		WebRegister,
		fx.Populate(&engine, &server, &service, &store, &management, &page),
	)
	if err != nil {
		t.Fatalf("WebRegister graph is invalid: %v", err)
	}
}

func TestValidateWebConfigRequiresPersistenceAndLoopback(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "nil", want: "config"},
		{name: "persistence disabled", cfg: &config.Config{HTTP: config.HTTPConfig{Host: "127.0.0.1"}}, want: "conversation.enabled"},
		{name: "public bind", cfg: &config.Config{Conversation: config.ConversationConfig{Enabled: true}, HTTP: config.HTTPConfig{Host: "0.0.0.0"}}, want: "loopback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateWebConfig(test.cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateWebConfig() error = %v, want %q", err, test.want)
			}
		})
	}
	if err := validateWebConfig(&config.Config{
		Conversation: config.ConversationConfig{Enabled: true}, HTTP: config.HTTPConfig{Host: "::1"},
	}); err != nil {
		t.Fatalf("loopback config rejected: %v", err)
	}
}
