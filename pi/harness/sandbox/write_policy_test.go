package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
)

func TestRestrictedHostFailsClosed(t *testing.T) {
	root := t.TempDir()
	n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
		WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{".tmp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, error) {
		t.Fatal("host must not look up binaries")
		return "", nil
	}
	_, err = newRunnerForOSWithPolicy("windows", root, lookup, n, true)
	if !errors.Is(err, ErrWritePolicyUnsupported) {
		t.Fatalf("got %v", err)
	}
	r, err := newRunnerForOSWithPolicy("windows", root, lookup, n, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.BuildArgv([]string{"ignored"}, CommandSpec{}); !errors.Is(err, ErrProcessDisabled) {
		t.Fatalf("disabled runner executed: %v", err)
	}
}

func TestProcessDisabledDoesNotCreateTmp(t *testing.T) {
	root := t.TempDir()
	n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
		WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{".tmp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, error) {
		t.Fatal("disabled process must not probe a backend")
		return "", nil
	}
	r, err := newRunnerForOSWithPolicy("linux", root, lookup, n, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.BuildShell("ignored", CommandSpec{}); !errors.Is(err, ErrProcessDisabled) {
		t.Fatalf("BuildShell() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("disabled runner created .tmp or returned unexpected error: %v", err)
	}

	want := []string{".tmp"}
	policy := r.Policy()
	if policy.WriteMode != string(workspacepolicy.Restricted) || !reflect.DeepEqual(policy.WritablePrefixes, want) {
		t.Fatalf("Policy() = %+v", policy)
	}
	policy.WritablePrefixes[0] = "changed"
	if got := r.Policy().WritablePrefixes; !reflect.DeepEqual(got, want) {
		t.Fatalf("Policy() returned mutable prefixes: %v", got)
	}
}

func TestRestrictedProcessRequiresTmpPrefix(t *testing.T) {
	root := t.TempDir()
	n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
		WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{"scratch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, error) {
		t.Fatal("invalid policy must fail before backend lookup")
		return "", nil
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			_, err := newRunnerForOSWithPolicy(goos, root, lookup, n, true)
			if !errors.Is(err, ErrWritePolicyUnsupported) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("rejected policy created .tmp or returned unexpected error: %v", err)
	}
}

func TestNativeRunnerPolicyReturnsImmutablePrefixes(t *testing.T) {
	want := []string{".tmp", "scratch"}
	for name, runner := range map[string]Runner{
		"seatbelt":   &SeatbeltRunner{policy: Policy{WritablePrefixes: append([]string(nil), want...)}},
		"bubblewrap": &BubblewrapRunner{policy: Policy{WritablePrefixes: append([]string(nil), want...)}},
	} {
		t.Run(name, func(t *testing.T) {
			returned := runner.Policy()
			returned.WritablePrefixes[0] = "changed"
			if got := runner.Policy().WritablePrefixes; !reflect.DeepEqual(got, want) {
				t.Fatalf("Policy() returned mutable prefixes: %v", got)
			}
		})
	}
}
