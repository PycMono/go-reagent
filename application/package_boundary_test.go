package application

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCLIEntryDoesNotDependOnWebOrCacheSDKs(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./cmd/reagent")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list cmd/reagent: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/PycMono/go-gin-sdk" ||
			dependency == "github.com/PycMono/go-cache-sdk" ||
			dependency == "github.com/gin-gonic/gin" {
			t.Fatalf("CLI unexpectedly depends on Web package %s", dependency)
		}
	}
}
