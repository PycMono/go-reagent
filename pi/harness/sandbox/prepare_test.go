package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCommandSpecHostPreservesEnvironmentAndDirectory(t *testing.T) {
	t.Setenv("REAGENT_PREPARE_INHERITED", "host-value")
	outside := t.TempDir()
	spec, err := PrepareCommandSpec(NewHostRunner(), t.TempDir(), outside, map[string]string{"EXPLICIT": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	joined := "\n" + strings.Join(spec.PayloadEnv, "\n") + "\n"
	if spec.WorkDir != outside || !strings.Contains(joined, "\nREAGENT_PREPARE_INHERITED=host-value\n") || !strings.Contains(joined, "\nEXPLICIT=ok\n") {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	spec, err = PrepareCommandSpec(NewHostRunner(), t.TempDir(), "", nil)
	if err != nil || spec.WorkDir != "" {
		t.Fatalf("host default cwd changed: %#v, %v", spec, err)
	}
}

func TestPrepareCommandSpecIsolatesEnvironmentAndResolvesDirectory(t *testing.T) {
	t.Setenv("REAGENT_PREPARE_SECRET", "must-not-leak")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewSeatbeltRunner("/usr/bin/sandbox-exec", root)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := PrepareCommandSpec(runner, root, "", map[string]string{"EXPLICIT": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	joined := "\n" + strings.Join(spec.PayloadEnv, "\n") + "\n"
	if spec.WorkDir != root || strings.Contains(joined, "REAGENT_PREPARE_SECRET=") || !strings.Contains(joined, "\nEXPLICIT=ok\n") || !strings.Contains(joined, "\nTMPDIR="+filepath.Join(root, ".tmp")+"\n") {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCommandSpec(runner, root, link, nil); err == nil {
		t.Fatal("accepted cwd symlink escaping workspace")
	}
	for _, key := range []string{"HOME", "PATH", "TMPDIR", "BAD=NAME", ""} {
		if _, err := PrepareCommandSpec(runner, root, "", map[string]string{key: "override"}); err == nil {
			t.Fatalf("accepted invalid override %q", key)
		}
	}
}
