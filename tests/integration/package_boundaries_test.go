package integration_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/PycMono/go-reagent"

func TestPublicPackageDependencyBoundaries(t *testing.T) {
	tests := []struct {
		pkg       string
		forbidden func(string) bool
	}{
		{
			pkg: modulePath + "/pi/skills",
			forbidden: func(dependency string) bool {
				return dependency == modulePath ||
					dependency == modulePath+"/pi" ||
					strings.HasPrefix(dependency, modulePath+"/pi/") ||
					strings.HasPrefix(dependency, modulePath+"/application") ||
					strings.HasPrefix(dependency, modulePath+"/config") ||
					strings.HasPrefix(dependency, modulePath+"/conversation") ||
					strings.HasPrefix(dependency, modulePath+"/persistence") ||
					strings.HasPrefix(dependency, modulePath+"/transport")
			},
		},
		{
			pkg: modulePath + "/pi/ai",
			forbidden: func(dependency string) bool {
				return dependency == modulePath ||
					strings.HasPrefix(dependency, modulePath+"/pi/agent") ||
					strings.HasPrefix(dependency, modulePath+"/internal/")
			},
		},
		{
			pkg: modulePath + "/pi/agent",
			forbidden: func(dependency string) bool {
				return dependency == modulePath || strings.HasPrefix(dependency, modulePath+"/internal/")
			},
		},
	}

	for _, test := range tests {
		t.Run(strings.TrimPrefix(test.pkg, modulePath+"/"), func(t *testing.T) {
			for _, dependency := range goListDependencies(t, test.pkg) {
				if test.forbidden(dependency) {
					t.Fatalf("%s imports forbidden dependency %s", test.pkg, dependency)
				}
			}
		})
	}
}

func TestPiDoesNotImportServicePackages(t *testing.T) {
	forbidden := []string{
		modulePath + "/application",
		modulePath + "/config",
		modulePath + "/conversation",
		modulePath + "/persistence",
		modulePath + "/transport",
	}
	for _, dependency := range goListDependencies(t, modulePath+"/pi/...") {
		for _, prefix := range forbidden {
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Fatalf("pi imports service dependency %s", dependency)
			}
		}
	}
}

func TestLegacyInternalPackageImportsAreAbsent(t *testing.T) {
	legacyPrefixes := []string{
		modulePath + "/internal/schema",
		modulePath + "/internal/provider",
		modulePath + "/internal/engine",
		modulePath + "/internal/context",
		modulePath + "/internal/app",
		modulePath + "/internal/conversation",
		modulePath + "/internal/driver",
		modulePath + "/internal/dispatch",
	}

	command := exec.Command("go", "list", "-json", "./...")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -json ./...: %v", commandError(err))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg struct {
			ImportPath   string
			Imports      []string
			TestImports  []string
			XTestImports []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		imports := append(append(append([]string(nil), pkg.Imports...), pkg.TestImports...), pkg.XTestImports...)
		for _, imported := range imports {
			for _, legacy := range legacyPrefixes {
				if imported == legacy || strings.HasPrefix(imported, legacy+"/") {
					t.Fatalf("%s imports removed package %s", pkg.ImportPath, imported)
				}
			}
		}
	}
}

func goListDependencies(t *testing.T, pkg string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-deps", "-f={{.ImportPath}}", pkg)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list dependencies for %s: %v", pkg, commandError(err))
	}
	dependencies := strings.Fields(string(output))
	filtered := dependencies[:0]
	for _, dependency := range dependencies {
		if dependency != pkg {
			filtered = append(filtered, dependency)
		}
	}
	return filtered
}

func commandError(err error) error {
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return &commandOutputError{err: err, stderr: strings.TrimSpace(string(exitErr.Stderr))}
	}
	return err
}

type commandOutputError struct {
	err    error
	stderr string
}

func (e *commandOutputError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *commandOutputError) Unwrap() error { return e.err }
