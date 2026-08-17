package page

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PycMono/go-reagent/frontend"
)

type Renderer struct {
	templates map[string]*template.Template
	layouts   map[string]string
}

// NewRenderer parses shared templates once, then clones them per page so
// identically named content blocks never leak between pages.
func NewRenderer(root string) (*Renderer, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	return newRenderer(os.DirFS(root), root)
}

func newRenderer(templateFS fs.FS, label string) (*Renderer, error) {
	commonFiles, err := templateFiles(templateFS, "layouts", "partials", "components")
	if err != nil {
		return nil, err
	}
	if len(commonFiles) == 0 {
		return nil, fmt.Errorf("template layout files not found under %s", label)
	}
	base := template.New("shared")
	for _, file := range commonFiles {
		content, readErr := fs.ReadFile(templateFS, file)
		if readErr != nil {
			return nil, fmt.Errorf("read template %s: %w", file, readErr)
		}
		if _, parseErr := base.Parse(string(content)); parseErr != nil {
			return nil, fmt.Errorf("parse template %s: %w", file, parseErr)
		}
	}
	if base.Lookup("base") == nil {
		return nil, fmt.Errorf("template layout definition %q not found", "base")
	}
	pages, err := fs.Glob(templateFS, "pages/*.html")
	if err != nil {
		return nil, fmt.Errorf("glob page templates: %w", err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("page templates not found under %s", label)
	}

	renderer := &Renderer{templates: make(map[string]*template.Template), layouts: make(map[string]string)}
	layoutPattern := regexp.MustCompile(`(?s)\{\{\s*define\s+"layout"\s*\}\}\s*([[:word:]-]+)\s*\{\{\s*end\s*\}\}`)
	for _, file := range pages {
		pageTemplate, cloneErr := base.Clone()
		if cloneErr != nil {
			return nil, fmt.Errorf("clone templates for %s: %w", file, cloneErr)
		}
		content, readErr := fs.ReadFile(templateFS, file)
		if readErr != nil {
			return nil, fmt.Errorf("read page template %s: %w", file, readErr)
		}
		if _, parseErr := pageTemplate.Parse(string(content)); parseErr != nil {
			return nil, fmt.Errorf("parse page template %s: %w", file, parseErr)
		}
		name := path.Base(file)
		layout := "base"
		if match := layoutPattern.FindSubmatch(content); len(match) == 2 {
			layout = string(match[1])
		}
		if pageTemplate.Lookup(layout) == nil {
			return nil, fmt.Errorf("page template %s references unknown layout %q", file, layout)
		}
		renderer.templates[name] = pageTemplate
		renderer.layouts[name] = layout
	}
	return renderer, nil
}

func NewProductionRenderer() (*Renderer, error) {
	return newRenderer(frontend.Templates, "embedded frontend/templates")
}

func (renderer *Renderer) Render(page string, data any) (string, error) {
	tmpl, found := renderer.templates[page]
	if !found {
		return "", fmt.Errorf("template not found: %s", page)
	}
	var output strings.Builder
	if err := tmpl.ExecuteTemplate(&output, renderer.layouts[page], data); err != nil {
		return "", fmt.Errorf("render template %s: %w", page, err)
	}
	return output.String(), nil
}

func templateFiles(templateFS fs.FS, directories ...string) ([]string, error) {
	var files []string
	for _, directory := range directories {
		matches, err := fs.Glob(templateFS, directory+"/*.html")
		if err != nil {
			return nil, fmt.Errorf("glob %s templates: %w", directory, err)
		}
		files = append(files, matches...)
	}
	return files, nil
}
