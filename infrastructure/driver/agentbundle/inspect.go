package agentbundle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/pi/harness"
)

const (
	maxBundleFile  = 1 << 20
	maxBundleBytes = 32 << 20
	maxBundlePaths = 2000
)

type treeFile struct {
	entry   agentversion.Entry
	content []byte
}

func inspectSource(ctx context.Context, root string) ([]treeFile, error) {
	return inspectSourceMode(ctx, root, false)
}

func inspectSourceMode(ctx context.Context, root string, draft bool) ([]treeFile, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if err := inspectWorkspaceStrict(ctx, canonical); !draft && err != nil {
		return nil, err
	}
	var files []treeFile
	var total int64
	paths := 0
	err = filepath.WalkDir(canonical, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(canonical, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if draft && rel == ".tmp" {
			if !d.IsDir() {
				return ErrInvalidBundle
			}
			return filepath.SkipDir
		}
		paths++
		if paths > maxBundlePaths {
			return fmt.Errorf("bundle exceeds %d paths", maxBundlePaths)
		}
		first := strings.Split(rel, "/")[0]
		if first == ".tmp" || first == "scratch" {
			return fmt.Errorf("bundle contains reserved path %q", rel)
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return validateBundleAsset(rel, true, "", nil)
		}
		if linkCount(info) > 1 {
			return fmt.Errorf("bundle path %q has multiple hard links", rel)
		}
		var content []byte
		mode := ""
		switch {
		case info.Mode().IsRegular():
			if info.Size() > maxBundleFile {
				return fmt.Errorf("bundle file %q exceeds 1 MiB", rel)
			}
			if linkCount(info) > 1 {
				return fmt.Errorf("bundle file %q has multiple hard links", rel)
			}
			content, err = readRegularBounded(name, info, maxBundleFile)
			if err != nil {
				return err
			}
			mode = "100644"
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, e := os.Readlink(name)
			if e != nil {
				return e
			}
			if filepath.IsAbs(target) {
				return fmt.Errorf("absolute symlink %q", rel)
			}
			resolved, e := filepath.EvalSymlinks(filepath.Join(filepath.Dir(name), target))
			if e != nil {
				return fmt.Errorf("symlink %q target: %w", rel, e)
			}
			within, e := filepath.Rel(canonical, resolved)
			if e != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
				return fmt.Errorf("symlink %q escapes bundle", rel)
			}
			if draft && (within == ".git" || strings.HasPrefix(within, ".git/") || within == ".tmp" || strings.HasPrefix(within, ".tmp/")) {
				return ErrInvalidBundle
			}
			content, mode = []byte(target), "120000"
		default:
			return fmt.Errorf("bundle path %q has unsupported file type", rel)
		}
		if draft && (strings.HasSuffix(rel, "/SKILL.md") && len(content) > 256<<10 || candidateSecret(rel, content)) {
			return fmt.Errorf("unsafe candidate path %q", rel)
		}
		if err := validateBundleAsset(rel, false, mode, content); err != nil {
			return err
		}
		total += int64(len(content))
		if total > maxBundleBytes {
			return fmt.Errorf("bundle exceeds 32 MiB")
		}
		hash := sha256.Sum256(content)
		files = append(files, treeFile{agentversion.Entry{Path: rel, Mode: mode, Size: int64(len(content)), ContentSHA256: fmt.Sprintf("%x", hash)}, content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func readRegularBounded(name string, info os.FileInfo, limit int64) ([]byte, error) {
	if !info.Mode().IsRegular() || info.Size() > limit || linkCount(info) > 1 {
		return nil, ErrInvalidBundle
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrInvalidBundle
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != int64(len(data)) || linkCount(after) > 1 {
		return nil, ErrInvalidBundle
	}
	return data, nil
}

func validateBundleAsset(name string, directory bool, mode string, content []byte) error {
	parts := strings.Split(name, "/")
	for _, part := range parts {
		lower := strings.ToLower(part)
		if lower == ".git" || lower == ".gitmodules" || lower == ".gitattributes" || lower == "agent.yaml" || lower == "agent.yml" || lower == "agent.json" {
			return fmt.Errorf("forbidden bundle path %q", name)
		}
	}
	if directory {
		if len(parts) == 1 && (parts[0] == "skills" || parts[0] == "documents" || parts[0] == "assets") {
			return nil
		}
		if parts[0] == "documents" || parts[0] == "assets" {
			return nil
		}
		if parts[0] == "skills" && len(parts) >= 2 && safeID(parts[1]) && (len(parts) == 2 || parts[2] == "scripts" || parts[2] == "references" || parts[2] == "assets") {
			return nil
		}
		return fmt.Errorf("path outside bundle schema %q", name)
	}
	if mode != "120000" && nativeExecutable(content) {
		return fmt.Errorf("native executable %q", name)
	}
	if name == "AGENTS.md" {
		if mode != "100644" {
			return fmt.Errorf("AGENTS.md mode")
		}
		return nil
	}
	if len(parts) < 2 {
		return fmt.Errorf("path outside bundle schema %q", name)
	}
	valid := false
	script := false
	switch parts[0] {
	case "documents", "assets":
		valid = true
	case "skills":
		if len(parts) >= 3 && safeID(parts[1]) {
			if len(parts) == 3 && parts[2] == "SKILL.md" {
				valid = true
			} else if len(parts) >= 4 && (parts[2] == "scripts" || parts[2] == "references" || parts[2] == "assets") {
				valid = true
				script = parts[2] == "scripts"
			}
		}
	}
	if !valid {
		return fmt.Errorf("path outside bundle schema %q", name)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if (ext == ".sh" || ext == ".py") != script {
		return fmt.Errorf("script path invalid %q", name)
	}
	if mode == "100755" && !script {
		return fmt.Errorf("executable outside scripts %q", name)
	}
	if script && (mode == "120000" || !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 || (ext != ".sh" && ext != ".py")) {
		return fmt.Errorf("invalid script %q", name)
	}
	return nil
}

func nativeExecutable(data []byte) bool {
	if len(data) >= 4 {
		if string(data[:4]) == "\x7fELF" {
			return true
		}
		magic := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
		switch magic {
		case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe, 0xbebafeca:
			return true
		}
	}
	return len(data) >= 2 && data[0] == 'M' && data[1] == 'Z'
}

func inspectWorkspaceStrict(ctx context.Context, root string) error {
	report, err := harness.InspectWorkspace(ctx, root)
	if err != nil {
		return err
	}
	if len(report.Diagnostics) != 0 {
		return fmt.Errorf("workspace has %d skill diagnostics", len(report.Diagnostics))
	}
	return nil
}

func linkCount(info os.FileInfo) uint64 {
	v := reflect.ValueOf(info.Sys())
	if !v.IsValid() {
		return 1
	}
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 1
	}
	f := v.FieldByName("Nlink")
	if !f.IsValid() {
		return 1
	}
	return f.Uint()
}
