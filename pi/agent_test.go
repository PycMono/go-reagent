package pi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent/pi"
)

func TestNewRejectsNilConfigAndCloseIsIdempotent(t *testing.T) {
	_, err := pi.New(nil)
	if pi.ErrorCodeOf(err) != pi.ErrorCodeConfigInvalid {
		t.Fatalf("New(nil) error = %v", err)
	}

	runtime := newHTTPBackedAgent(t)
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(context.Background(), pi.RunRequest{Input: pi.UserMessage("after close")})
	if !errors.Is(err, pi.ErrClosed) || pi.ErrorCodeOf(err) != pi.ErrorCodeClosed {
		t.Fatalf("Run after Close error = %v", err)
	}
}

func newHTTPBackedAgent(t *testing.T) *pi.Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("model server should not be called while testing lifecycle")
	}))
	t.Cleanup(server.Close)

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a lifecycle test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(workDir, "skills", "lifecycle")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: lifecycle\ndescription: Test lifecycle behavior\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := &pi.Config{
		CurrentPlatform: "test",
		Platforms: []pi.PlatformConfig{{
			ID: "test", Protocol: pi.ProtocolOpenAI, BaseURL: server.URL + "/v1/", APIKey: "key", Model: "model",
			Pricing: &pi.PricingConfig{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		}},
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	runtime, newErr := pi.New(config)
	restoreErr := os.Chdir(originalDir)
	if newErr != nil {
		t.Fatalf("New() error = %v", newErr)
	}
	if restoreErr != nil {
		t.Fatalf("restore workDir: %v", restoreErr)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	return runtime
}
