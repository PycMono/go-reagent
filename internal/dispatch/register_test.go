package dispatch

import (
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
)

func TestNewReporterRegistrationsAllowsDisabledWeCom(t *testing.T) {
	registrations, err := NewReporterRegistrations(&config.Config{})
	if err != nil {
		t.Fatalf("NewReporterRegistrations() error = %v", err)
	}
	if len(registrations) != 0 {
		t.Fatalf("registrations = %#v, want none", registrations)
	}
}

func TestNewReporterRegistrationsRejectsNilConfig(t *testing.T) {
	if _, err := NewReporterRegistrations(nil); err == nil {
		t.Fatal("NewReporterRegistrations(nil) error = nil")
	}
}
