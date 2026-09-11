//go:build darwin || linux

package agentstate

import "testing"

func TestDataDirectoryAllowsOnlyOneServer(t *testing.T) {
	root := t.TempDir()
	first, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(root); err == nil {
		second.Close()
		t.Fatal("second server acquired data")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}
