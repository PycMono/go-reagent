package utils

import (
	"strings"
	"testing"
)

func TestNewChildProcessRejectsPathOverride(t *testing.T) {
	_, err := NewChildProcess("exit 0", t.TempDir(), map[string]string{"PATH": "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("NewChildProcess() error = %v", err)
	}
}

func TestNewChildProcessSetsWorkingDirectory(t *testing.T) {
	workDir := t.TempDir()
	child, err := NewChildProcess("exit 0", workDir, map[string]string{"PI_TEST_VALUE": "ok"})
	if err != nil {
		t.Fatalf("NewChildProcess() error = %v", err)
	}
	if child.Dir != workDir {
		t.Fatalf("child.Dir = %q, want %q", child.Dir, workDir)
	}
}
