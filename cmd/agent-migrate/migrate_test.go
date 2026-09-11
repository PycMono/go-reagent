package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOwnershipRequiresUnambiguousHostMapping(t *testing.T) {
	for _, raw := range []string{`[{"conversation_id":"c","tenant_id":"a","role":"admin"}]`, `[{"conversation_id":"c","tenant_id":"a"},{"conversation_id":"c","tenant_id":"b"}]`, `[{"conversation_id":"c","tenant_id":"a","tenant_id":"b"}]`} {
		path := filepath.Join(t.TempDir(), "mapping.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readOwnership(path); err == nil {
			t.Fatal("ambiguous mapping accepted")
		}
	}
}
