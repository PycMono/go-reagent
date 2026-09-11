package agenttraining

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorWritesAssetsButCannotReachGitOrSibling(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".git", filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	tool, err := NewFileTool(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".git/config", "../outside", "skills/config", "AGENTS.md/other", "scratch/a"} {
		raw, _ := json.Marshal(map[string]string{"action": "write", "path": p, "content": "x"})
		if _, err := tool.Execute(context.Background(), raw, nil); err == nil {
			t.Fatalf("allowed %s", p)
		}
	}
	raw := json.RawMessage(`{"action":"write","path":"AGENTS.md","content":"new rules"}`)
	if _, err := tool.Execute(context.Background(), raw, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(b) != "new rules" {
		t.Fatalf("%s %v", b, err)
	}
}
