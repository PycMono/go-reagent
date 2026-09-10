package agentcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCursorKeyPersistsPrivateKey(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadCursorKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadCursorKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || string(first) != string(second) {
		t.Fatal("cursor key was not reused")
	}
	info, err := os.Stat(filepath.Join(dir, "state", "cursor.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v err=%v", info.Mode().Perm(), err)
	}
}

func TestLoadCursorKeyRejectsCorruptOrSymlinkKey(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		dir := t.TempDir()
		state := filepath.Join(dir, "state")
		if err := os.MkdirAll(state, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(state, "cursor.key")
		if symlink {
			target := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(target, make([]byte, 32), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCursorKey(dir); err == nil {
			t.Fatalf("unsafe key accepted (symlink=%v)", symlink)
		}
	}
}

func TestLoadCursorKeyRejectsSymlinkStateDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "state")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCursorKey(dir); err == nil {
		t.Fatal("symlink state directory accepted")
	}
}
