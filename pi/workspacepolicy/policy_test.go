package workspacepolicy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeRejectsUnsafePrefixes(t *testing.T) {
	for _, p := range []string{"", ".", "..", "../out", "a/../scratch", "/tmp", "C:/tmp", `a\b`, "a\x00b", "a//b", "a/"} {
		p := p
		t.Run(p, func(t *testing.T) {
			_, err := Normalize(t.TempDir(), Policy{WriteMode: Restricted, WritablePrefixes: []string{p}})
			if err == nil {
				t.Fatalf("accepted %q", p)
			}
		})
	}
}

func TestNormalizeRequiresExplicitConsistentMode(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []Policy{
		{},
		{WriteMode: Mode("unknown")},
		{WriteMode: All, WritablePrefixes: []string{"scratch"}},
	} {
		if _, err := Normalize(root, tc); err == nil {
			t.Fatalf("accepted policy %+v", tc)
		}
	}

	for _, tc := range []Policy{{WriteMode: Restricted}, {WriteMode: All}} {
		if _, err := Normalize(root, tc); err != nil {
			t.Fatalf("rejected policy %+v: %v", tc, err)
		}
	}
}

func TestNormalizeCanonicalizesRootAndRequiresDirectory(t *testing.T) {
	if _, err := Normalize("", Policy{WriteMode: Restricted}); err == nil {
		t.Fatal("accepted empty root")
	}

	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	n, err := Normalize(filepath.Join(parent, ".", "workspace"), Policy{WriteMode: Restricted})
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err = filepath.Abs(want)
	if err != nil {
		t.Fatal(err)
	}
	if n.Root() != want {
		t.Fatalf("root = %q, want %q", n.Root(), want)
	}

	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(file, Policy{WriteMode: Restricted}); err == nil {
		t.Fatal("accepted non-directory root")
	}
}

func TestNormalizeRejectsSymlinkPrefixAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Normalize(root, Policy{WriteMode: Restricted, WritablePrefixes: []string{"link/nested"}}); err == nil {
		t.Fatal("accepted symlink prefix ancestor")
	}
}

func TestNormalizeRejectsNonDirectoryPrefixAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(root, Policy{WriteMode: Restricted, WritablePrefixes: []string{"file/nested"}}); err == nil {
		t.Fatal("accepted non-directory prefix ancestor")
	}
}

func TestNormalizeAllowsMissingPrefixesWithoutCreatingThem(t *testing.T) {
	root := t.TempDir()
	prefix := "missing/nested"
	if _, err := Normalize(root, Policy{WriteMode: Restricted, WritablePrefixes: []string{prefix}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(prefix))); !os.IsNotExist(err) {
		t.Fatalf("Normalize created prefix or returned unexpected error: %v", err)
	}
}

func TestNormalizeProducesMinimalStableImmutablePrefixes(t *testing.T) {
	root := t.TempDir()
	input := []string{"scratch/nested", ".tmp", "scratch", ".tmp", "artifacts/output"}
	n, err := Normalize(root, Policy{WriteMode: Restricted, WritablePrefixes: input})
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "changed"
	want := []string{".tmp", "artifacts/output", "scratch"}
	if got := n.Prefixes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("prefixes = %v, want %v", got, want)
	}
	returned := n.Prefixes()
	returned[0] = "changed"
	if got := n.Prefixes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("returned slice mutated policy: %v", got)
	}
	if n.Mode() != Restricted {
		t.Fatalf("mode = %q", n.Mode())
	}

	again, err := Normalize(root, Policy{WriteMode: Restricted, WritablePrefixes: []string{"artifacts/output", "scratch", ".tmp"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Root() != n.Root() || again.Mode() != n.Mode() || !reflect.DeepEqual(again.Prefixes(), n.Prefixes()) {
		t.Fatalf("normalization is unstable: first=%q %q %v second=%q %q %v",
			n.Root(), n.Mode(), n.Prefixes(), again.Root(), again.Mode(), again.Prefixes())
	}
}

func TestMatchWriteUsesPathComponents(t *testing.T) {
	n, err := Normalize(t.TempDir(), Policy{WriteMode: Restricted, WritablePrefixes: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"scratch-evil/file", "scratch", "scratch/../AGENTS.md", "/scratch/file", `scratch\file`, "scratch/file\x00name"} {
		if _, _, err := n.MatchWrite(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	prefix, relative, err := n.MatchWrite("scratch/result.txt")
	if err != nil || prefix != "scratch" || relative != "result.txt" {
		t.Fatalf("match: %q %q %v", prefix, relative, err)
	}
}

func TestMatchWriteAllAllowsSafeRelativeTargetsOnly(t *testing.T) {
	n, err := Normalize(t.TempDir(), Policy{WriteMode: All})
	if err != nil {
		t.Fatal(err)
	}
	prefix, relative, err := n.MatchWrite("nested/result.txt")
	if err != nil || prefix != "" || relative != "nested/result.txt" {
		t.Fatalf("match: %q %q %v", prefix, relative, err)
	}
	for _, path := range []string{"", ".", "../outside", "nested/../outside", "/tmp/file", `nested\file`, "a\x00b"} {
		if _, _, err := n.MatchWrite(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
