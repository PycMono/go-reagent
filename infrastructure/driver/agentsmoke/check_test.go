package agentsmoke

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	"os"
	"path/filepath"
	"testing"
)

func TestScriptsRequirePinnedInterpreter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.py"), []byte("print('x')"), 0600); err != nil {
		t.Fatal(err)
	}
	check, err := New(config.AgentTrainingConfig{SmokeTimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := check.Check(context.Background(), root, agentversion.Snapshot{}); err == nil {
		t.Fatal("unapproved interpreter accepted")
	}
}

func TestNativeShellSyntaxDoesNotExecuteCandidate(t *testing.T) {
	if os.Getenv("AGENT_NATIVE_SMOKE_TEST") != "1" {
		t.Skip("set AGENT_NATIVE_SMOKE_TEST=1 for native sandbox test")
	}
	root := t.TempDir()
	script := filepath.Join(root, "test.sh")
	if err := os.WriteFile(script, []byte("touch executed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(config.AgentTrainingConfig{SmokeTimeoutSeconds: 10, ApprovedInterpreters: []config.ApprovedInterpreter{{Extension: ".sh", Executable: "/bin/sh", SHA256: fmt.Sprintf("%x", sha256.Sum256(bytes))}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Check(context.Background(), root, agentversion.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "executed")); !os.IsNotExist(err) {
		t.Fatal("candidate script executed")
	}
	if err := os.WriteFile(script, []byte("if then\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Check(context.Background(), root, agentversion.Snapshot{}); err == nil {
		t.Fatal("invalid syntax accepted")
	}
}
