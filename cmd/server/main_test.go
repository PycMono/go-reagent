package main

import (
	"testing"

	"go.uber.org/fx"
)

func TestServerApplicationOptionsFormAValidGraph(t *testing.T) {
	if err := fx.ValidateApp(newAppOptions()...); err != nil {
		t.Fatalf("server Fx graph is invalid: %v", err)
	}
}

func TestServerLoggerUsesWebModule(t *testing.T) {
	if logger := newApplicationLogger(); logger == nil {
		t.Fatal("newApplicationLogger() returned nil")
	}
}
