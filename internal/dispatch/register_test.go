package dispatch

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
)

func TestNewReporterAllowsDisabledWeCom(t *testing.T) {
	reporter, err := NewReporter(&config.Config{})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}
	if reporter == nil {
		t.Fatal("NewReporter() = nil")
	}
	reporter.Report(context.Background(), schema.NewThinkingEvent())
}

func TestNewReporterRejectsNilConfig(t *testing.T) {
	if _, err := NewReporter(nil); err == nil {
		t.Fatal("NewReporter(nil) error = nil")
	}
}
