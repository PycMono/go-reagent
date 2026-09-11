package agentstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestJournalPersistsAndRecoversPreparation(t *testing.T) {
	root := t.TempDir()
	journal, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	record := Preparation{SchemaVersion: 1, TenantID: "tenant-a", AgentID: "agent-a", VersionID: "version-a", ExpectedRowVersion: 3, Phase: PhaseMaterialized, Tag: "versions/version-a", TempSource: filepath.Join(root, "tenants", "tenant-a", "agents", "agent-a", "state", "tmp", "version-a"), Materialized: filepath.Join(root, "tenants", "tenant-a", "agents", "agent-a", "versions", "version-a")}
	if err := journal.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	loaded, err := journal.Load(context.Background(), "tenant-a", "agent-a", "version-a")
	if err != nil || loaded != record {
		t.Fatalf("Load() = %#v, %v", loaded, err)
	}
	recovered, err := journal.List(context.Background())
	if err != nil || len(recovered) != 1 || recovered[0] != record {
		t.Fatalf("List() = %#v, %v", recovered, err)
	}
	if err := journal.Remove(context.Background(), "tenant-a", "agent-a", "version-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal.path("tenant-a", "agent-a", "version-a")); !os.IsNotExist(err) {
		t.Fatalf("journal still exists: %v", err)
	}
}

func TestJournalRejectsUnsafeOrUnownedReferences(t *testing.T) {
	root := t.TempDir()
	journal, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	base := Preparation{TenantID: "tenant-a", AgentID: "agent-a", VersionID: "version-a", Phase: PhaseReserved, Tag: "versions/version-a"}
	for name, mutate := range map[string]func(*Preparation){
		"unsafe tenant": func(r *Preparation) { r.TenantID = "../other" },
		"wrong tag":     func(r *Preparation) { r.Tag = "versions/other" },
		"external temp": func(r *Preparation) { r.TempSource = filepath.Join(root, "other") },
	} {
		t.Run(name, func(t *testing.T) {
			record := base
			mutate(&record)
			if err := journal.Save(context.Background(), record); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestJournalRejectsUnknownFieldsOnRecovery(t *testing.T) {
	root := t.TempDir()
	journal, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := journal.path("tenant-a", "agent-a", "version-a")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"tenant_id":"tenant-a","agent_id":"agent-a","version_id":"version-a","expected_row_version":0,"phase":"reserved","tag":"versions/version-a","temp_source":"","materialized":"","bundle_commit":"","bundle_digest":"","spec_digest":"","secret":"leak"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Load(context.Background(), "tenant-a", "agent-a", "version-a"); err == nil {
		t.Fatal("unknown journal field accepted")
	}
}
