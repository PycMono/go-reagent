//go:build darwin || linux

package agentstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Lock enforces the platform's single-server data-directory contract. Closing
// the descriptor releases the kernel lock, including after an abnormal exit.
func Lock(root string) (*os.File, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("Agent data directory is already owned by another server")
	}
	return f, nil
}
