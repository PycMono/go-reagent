package infrastructure_test

import (
	"os/exec"
	"strings"
	"testing"
)

const redisDriverPackage = "github.com/PycMono/go-reagent/infrastructure/driver/redis"

func TestServerDependsOnRequiredRedisDriver(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./cmd/server")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list cmd/server: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == redisDriverPackage {
			return
		}
	}
	t.Fatalf("cmd/server does not depend on required Redis Driver %s", redisDriverPackage)
}
