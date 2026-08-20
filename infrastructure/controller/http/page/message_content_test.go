package page

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestChatMessageContentJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root := filepath.Clean(filepath.Join(frontendTemplateRoot(t), "..", "static", "js", "pages"))
	command := exec.Command(node, "--test", filepath.Join(root, "chat_message_content_test.mjs"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("chat message content tests failed: %v\n%s", err, output)
	}
}
