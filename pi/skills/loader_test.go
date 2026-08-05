package skills

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestLoaderClassifiesMissingWorkspace verifies direct callers can classify an inaccessible Skill root.
func TestLoaderClassifiesMissingWorkspace(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := NewLoader(missing).Discover(Environment{
		GOOS:      "linux",
		BinLookup: func(string) bool { return true },
		EnvLookup: func(string) bool { return true },
	})
	if err == nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("Discover() error = %v, want ErrInvalid", err)
	}
}
