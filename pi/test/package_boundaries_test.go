package test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/PycMono/go-reagent"

func TestHarnessPackagesDoNotDependOnAgentCore(t *testing.T) {
	packages := []string{
		modulePath + "/pi/harness",
		modulePath + "/pi/harness/errors",
		modulePath + "/pi/harness/skills",
		modulePath + "/pi/harness/tools",
		modulePath + "/pi/harness/observability",
	}
	for _, pkg := range packages {
		t.Run(strings.TrimPrefix(pkg, modulePath+"/pi/"), func(t *testing.T) {
			for _, dependency := range goListDependencies(t, pkg) {
				if dependency == modulePath+"/pi" {
					t.Fatalf("%s depends on Agent Core %s", pkg, dependency)
				}
			}
		})
	}
}

func TestMCPDependencyDirection(t *testing.T) {
	for _, pkg := range []string{modulePath + "/pi", modulePath + "/pi/ai", modulePath + "/pi/harness"} {
		for _, dependency := range goListDependencies(t, pkg) {
			if dependency == modulePath+"/pi/mcp" {
				t.Fatalf("%s must not depend on pi/mcp", pkg)
			}
		}
	}
	dependencies := goListDependencies(t, modulePath+"/pi/mcp")
	if !slices.Contains(dependencies, modulePath+"/pi") || !slices.Contains(dependencies, modulePath+"/pi/ai") {
		t.Fatalf("pi/mcp dependencies do not include Pi contracts: %v", dependencies)
	}
}

func TestRemovedPiPackagesAreNotPublished(t *testing.T) {
	removed := map[string]bool{
		modulePath + "/pi/agent":         true,
		modulePath + "/pi/errors":        true,
		modulePath + "/pi/skills":        true,
		modulePath + "/pi/tools":         true,
		modulePath + "/pi/observability": true,
		modulePath + "/pi/utils":         true,
	}
	command := exec.Command("go", "list", "-f={{.ImportPath}}", "./pi/...")
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list ./pi/...: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, pkg := range strings.Fields(string(output)) {
		if removed[pkg] {
			t.Fatalf("removed package is still published: %s", pkg)
		}
	}
}

func goListDependencies(t *testing.T, pkg string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-deps", "-f={{.ImportPath}}", pkg)
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list dependencies for %s: %v: %s", pkg, err, strings.TrimSpace(string(output)))
	}
	return strings.Fields(string(output))
}
