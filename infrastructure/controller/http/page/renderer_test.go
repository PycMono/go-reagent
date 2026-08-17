package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRendererClonesCommonTemplatesPerPage(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "layouts/base.html", `{{define "base"}}base[{{template "content" .}}]{{end}}`)
	writeTemplate(t, root, "partials/head.html", `{{define "head"}}head{{end}}`)
	writeTemplate(t, root, "components/item.html", `{{define "item"}}item{{end}}`)
	writeTemplate(t, root, "pages/one.html", `{{define "layout"}}base{{end}}{{define "content"}}one {{.Name}}{{end}}`)
	writeTemplate(t, root, "pages/two.html", `{{define "layout"}}base{{end}}{{define "content"}}two {{.Name}}{{end}}`)

	renderer, err := NewRenderer(root)
	if err != nil {
		t.Fatal(err)
	}
	one, err := renderer.Render("one.html", map[string]string{"Name": "A"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := renderer.Render("two.html", map[string]string{"Name": "B"})
	if err != nil {
		t.Fatal(err)
	}
	if one != "base[one A]" || two != "base[two B]" {
		t.Fatalf("rendered pages = %q / %q", one, two)
	}
}

func TestRendererRejectsMissingOrInvalidTemplateTree(t *testing.T) {
	if _, err := NewRenderer(t.TempDir()); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("missing layout error = %v", err)
	}
	root := t.TempDir()
	writeTemplate(t, root, "layouts/base.html", `{{define "base"}}ok{{end}}`)
	writeTemplate(t, root, "pages/chat.html", `{{define "content"}}`)
	if _, err := NewRenderer(root); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("invalid page error = %v", err)
	}
}

func TestProductionChatPageRendersAgentProfileControls(t *testing.T) {
	renderer, err := NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	body, err := renderer.Render("chat.html", map[string]string{"Title": "Reagent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"profilePicker", "profileStarters", "sessionProfile", "profileFilter"} {
		if !strings.Contains(body, `id="`+id+`"`) {
			t.Fatalf("chat page missing Profile control %q", id)
		}
	}
}

func TestProductionRendererDoesNotDependOnProcessWorkingDirectory(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	renderer, err := NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render("chat.html", map[string]string{"Title": "Reagent"}); err != nil {
		t.Fatal(err)
	}
}

func writeTemplate(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
