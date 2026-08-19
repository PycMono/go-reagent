package application_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestServerEntryDoesNotDependOnOneShotTransport(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./cmd/server")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list cmd/server: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/PycMono/go-reagent/transport" {
			t.Fatalf("Web server unexpectedly depends on one-shot transport package %s", dependency)
		}
	}
}

func TestAgentPublicInformationUsesMCPBoundary(t *testing.T) {
	if slices.Contains(goListImports(t, "./application/tool/chat"), "net/http") {
		t.Fatal("application/tool/chat directly imports net/http")
	}
	if slices.Contains(goListImports(t, "./application/web"), "github.com/PycMono/go-reagent/infrastructure/driver/openmeteo") {
		t.Fatal("application/web still imports Open-Meteo")
	}
	if _, err := os.Stat(filepath.Join("..", "infrastructure", "driver", "openmeteo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open-Meteo production package still exists: %v", err)
	}
}

func goListImports(t *testing.T, pkg string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg)
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list imports %s: %v: %s", pkg, err, strings.TrimSpace(string(output)))
	}
	return strings.Fields(string(output))
}
