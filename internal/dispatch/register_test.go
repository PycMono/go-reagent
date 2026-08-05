package dispatch

import (
	"testing"

	"github.com/PycMono/go-reagent"
)

func TestNewReporterRegistrationsAllowsDisabledWeCom(t *testing.T) {
	registrations, err := NewReporterRegistrations(&reagent.Config{})
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
