package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentconfig "github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/dispatch"
	"github.com/PycMono/go-reagent/internal/schema"
)

func TestNewReporterSendsWeComEventWhenConfigured(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := dispatch.NewReporter(&agentconfig.Config{
		Bot: agentconfig.BotConfig{
			WeCom: agentconfig.WeComConfig{WebhookURL: server.URL},
		},
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}
	reporter.Report(context.Background(), schema.NewToolStartEvent(schema.ToolCall{Name: "read_file"}))

	select {
	case <-received:
	default:
		t.Fatal("configured WeCom Reporter did not send lifecycle event")
	}
}
