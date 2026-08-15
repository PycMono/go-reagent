package application_test

import (
	"os/exec"
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
