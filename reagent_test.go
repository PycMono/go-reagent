package reagent_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent"
)

func TestNewRejectsNilConfigAndCloseIsIdempotent(t *testing.T) {
	_, err := reagent.New(nil)
	if reagent.ErrorCodeOf(err) != reagent.ErrorCodeConfigInvalid {
		t.Fatalf("New(nil) error = %v", err)
	}

	runtime := newHTTPBackedAgent(t)
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(context.Background(), reagent.RunRequest{Input: reagent.UserMessage("after close")})
	if !errors.Is(err, reagent.ErrClosed) || reagent.ErrorCodeOf(err) != reagent.ErrorCodeClosed {
		t.Fatalf("Run after Close error = %v", err)
	}
}

func newHTTPBackedAgent(t *testing.T) *reagent.Agent {
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
	configPath := filepath.Join(workDir, "config.json")
	document := fmt.Sprintf(`{
		"currentPlatform":"test",
		"platforms":[{"id":"test","protocol":"openai","baseURL":%q,"apiKey":"key","model":"model"}]
	}`, server.URL+"/v1/")
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := reagent.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	runtime, newErr := reagent.New(config)
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
