package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentsmoke"
	"go.uber.org/fx"
)

func TestPlatformServerGraphHasAllDependencies(t *testing.T) {
	if err := fx.ValidateApp(Register, fx.NopLogger); err != nil {
		t.Fatal(err)
	}
}

func TestUnapprovedScriptsCannotReceiveValidationEvidence(t *testing.T) {
	checker, err := agentsmoke.New(config.AgentTrainingConfig{SmokeTimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := checker.Check(context.Background(), root, agentversion.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "check.py"), []byte("print('hello')"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checker.Check(context.Background(), root, agentversion.Snapshot{}); err == nil {
		t.Fatal("unapproved script was validated")
	}
}
