package page

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestChatPageContainsRequiredApplicationShell(t *testing.T) {
	renderer, err := NewRenderer(frontendTemplateRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	controller := NewController(renderer)
	router := gin.New()
	router.GET("/", controller.Chat)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status/body = %d / %s", response.Code, response.Body.String())
	}
	for _, required := range []string{
		`id="conversationSidebar"`, `id="chatMain"`, `id="chatMessages"`,
		`id="runStatus"`, `id="chatComposer"`, `id="chatInput"`, `id="sendBtn"`,
		`/static/css/pages/chat.css`, `/static/js/pages/chat.js`,
		`<link rel="icon" href="data:,">`,
	} {
		if !strings.Contains(response.Body.String(), required) {
			t.Errorf("page missing %s", required)
		}
	}
}

func TestChatJavaScriptContainsAPIAndSSEContract(t *testing.T) {
	root := filepath.Clean(filepath.Join(frontendTemplateRoot(t), "..", "static"))
	content, err := os.ReadFile(filepath.Join(root, "js", "pages", "chat.js"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, required := range []string{
		"/api/v1/conversations", "/messages", "/runs", "/cancel",
		"run.started", "agent.thinking", "tool.started", "tool.updated",
		"tool.completed", "message.started", "message.delta", "message.completed",
		"run.failed", "run.completed",
		"AbortController", "TextDecoder", "textContent",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("chat.js missing %q", required)
		}
	}
	for _, forbidden := range []string{"localStorage", "file upload", "regenerate"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("chat.js contains forbidden %q", forbidden)
		}
	}
}

func TestChatVisibilityJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root := filepath.Clean(filepath.Join(frontendTemplateRoot(t), "..", "static", "js", "pages"))
	command := exec.Command(node, "--test", filepath.Join(root, "chat_visibility_test.mjs"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("chat visibility tests failed: %v\n%s", err, output)
	}
}

func frontendTemplateRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "frontend", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
