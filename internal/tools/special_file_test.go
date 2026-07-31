package tools

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

const specialFileTestTimeout = time.Second

func makeFIFO(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("named pipe regression requires mkfifo")
	}
	output, err := exec.Command("mkfifo", path).CombinedOutput()
	if err != nil {
		t.Fatalf("mkfifo %q: %v: %s", path, err, output)
	}
}

func mustReturnBefore(t *testing.T, run func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- run() }()
	select {
	case err := <-result:
		return err
	case <-time.After(specialFileTestTimeout):
		t.Fatalf("special-file operation blocked for %s", specialFileTestTimeout)
		return nil
	}
}
