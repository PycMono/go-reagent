package agenttemplate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	agentprofile "github.com/PycMono/go-reagent/domain/repository/agentprofile"
	"github.com/PycMono/go-reagent/pi/harness"
)

const (
	maxFileSize = 1 << 20
	maxBytes    = 32 << 20
	maxPaths    = 2000
)

type Assembler struct {
	workspace string
	catalog   agentprofile.Catalog
}

type plannedFile struct {
	path string
	data []byte
	mode fs.FileMode
}

func New(workspace string, catalog agentprofile.Catalog) (*Assembler, error) {
	if catalog == nil {
		return nil, errors.New("agent template catalog is required")
	}
	absolute, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("agent template workspace must be a real directory")
	}
	return &Assembler{workspace: filepath.Clean(absolute), catalog: catalog}, nil
}

func (a *Assembler) Assemble(ctx context.Context, templateCode, destination string) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	profile, ok := a.catalog.Find(strings.TrimSpace(templateCode))
	if !ok || !profile.Selectable {
		return errors.New("agent template is unknown or unavailable")
	}
	destination, err := emptyDestination(destination)
	if err != nil {
		return err
	}
	plan, err := a.plan(ctx, profile.Code)
	if err != nil {
		return err
	}
	written := false
	defer func() {
		if resultErr != nil && written {
			entries, _ := os.ReadDir(destination)
			for _, entry := range entries {
				_ = os.RemoveAll(filepath.Join(destination, entry.Name()))
			}
		}
	}()
	for _, file := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		written = true
		if err := os.WriteFile(target, file.data, file.mode); err != nil {
			return err
		}
	}
	report, err := harness.InspectWorkspace(ctx, destination)
	if err != nil {
		return err
	}
	if len(report.Diagnostics) != 0 {
		return fmt.Errorf("assembled template has %d workspace diagnostics", len(report.Diagnostics))
	}
	return nil
}

func (a *Assembler) plan(ctx context.Context, code string) ([]plannedFile, error) {
	files := map[string]plannedFile{}
	paths, total := 2, 0
	add := func(path string, data []byte, mode fs.FileMode) error {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
			return fmt.Errorf("template target path escapes: %q", path)
		}
		if _, exists := files[path]; exists {
			return fmt.Errorf("template asset collision at %q", path)
		}
		for existing := range files {
			if strings.HasPrefix(path, existing+"/") || strings.HasPrefix(existing, path+"/") {
				return fmt.Errorf("template file/directory collision between %q and %q", existing, path)
			}
		}
		total += len(data)
		if paths > maxPaths || total > maxBytes {
			return errors.New("agent template exceeds bundle limits")
		}
		files[path] = plannedFile{path: path, data: append([]byte(nil), data...), mode: mode.Perm()}
		return nil
	}
	rootInstructions, err := readSourceFile(filepath.Join(a.workspace, "AGENTS.md"))
	if err != nil {
		return nil, err
	}
	profileRoot := filepath.Join(a.workspace, "profiles", code)
	profileInstructions, err := readSourceFile(filepath.Join(profileRoot, "AGENTS.md"))
	if err != nil {
		return nil, err
	}
	combined := append(append([]byte{}, rootInstructions...), []byte("\n\n")...)
	combined = append(combined, profileInstructions...)
	if err := add("AGENTS.md", rewriteReferences(combined, code), 0o644); err != nil {
		return nil, err
	}
	if err := walkSource(ctx, filepath.Join(a.workspace, "skills"), &paths, func(relative string, data []byte, mode fs.FileMode) error {
		return add(filepath.Join("skills", relative), rewriteReferences(data, code), mode)
	}); err != nil {
		return nil, err
	}
	profileSkills := filepath.Join(profileRoot, "skills")
	if err := walkSource(ctx, profileSkills, &paths, func(relative string, data []byte, mode fs.FileMode) error {
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) >= 3 && parts[1] == "templates" {
			return add(filepath.Join("assets", parts[0], filepath.FromSlash(strings.Join(parts[2:], "/"))), rewriteReferences(data, code), mode)
		}
		return add(filepath.Join("skills", relative), rewriteReferences(data, code), mode)
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := walkSource(ctx, filepath.Join(profileRoot, "references"), &paths, func(relative string, data []byte, mode fs.FileMode) error {
		return add(filepath.Join("documents", relative), rewriteReferences(data, code), mode)
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	plan := make([]plannedFile, 0, len(files))
	for _, file := range files {
		if strings.Contains(string(file.data), "profiles/"+code+"/") {
			return nil, fmt.Errorf("unresolved local template reference in %q", file.path)
		}
		plan = append(plan, file)
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].path < plan[j].path })
	return plan, nil
}

func rewriteReferences(data []byte, code string) []byte {
	text := string(data)
	text = strings.ReplaceAll(text, "profiles/"+code+"/references/", "documents/")
	templateReference := regexp.MustCompile(regexp.QuoteMeta("profiles/"+code+"/skills/") + `([a-z][a-z0-9-]*)/templates/`)
	text = templateReference.ReplaceAllString(text, "assets/$1/")
	return []byte(text)
}

func walkSource(ctx context.Context, root string, paths *int, visit func(string, []byte, fs.FileMode) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("template source must be a real directory")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		*paths++
		if *paths > maxPaths {
			return errors.New("agent template exceeds path limit")
		}
		fileInfo, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 || (!fileInfo.IsDir() && !fileInfo.Mode().IsRegular()) {
			return fmt.Errorf("unsafe template source path %q", path)
		}
		if fileInfo.IsDir() {
			return nil
		}
		if fileInfo.Size() > maxFileSize || linkCount(fileInfo) > 1 {
			return fmt.Errorf("unsafe template source file %q", path)
		}
		data, err := readSourceFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return visit(relative, data, fileInfo.Mode())
	})
}

func readSourceFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize || linkCount(info) > 1 {
		return nil, fmt.Errorf("unsafe template source file %q", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil || len(data) > maxFileSize || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, fmt.Errorf("invalid template text file %q", path)
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != int64(len(data)) {
		return nil, fmt.Errorf("template source changed while reading %q", path)
	}
	return data, nil
}

func emptyDestination(path string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("assembly destination must be a real directory")
	}
	entries, err := os.ReadDir(absolute)
	if err != nil || len(entries) != 0 {
		return "", errors.New("assembly destination must be empty")
	}
	return filepath.Clean(absolute), nil
}

func linkCount(info os.FileInfo) uint64 {
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 1
	}
	field := value.FieldByName("Nlink")
	if !field.IsValid() {
		return 1
	}
	return field.Uint()
}
