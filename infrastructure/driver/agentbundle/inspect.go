package agentbundle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

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
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if err := inspectWorkspaceStrict(ctx, canonical); err != nil {
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
			return nil
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
			content, err = os.ReadFile(name)
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
			content, mode = []byte(target), "120000"
		default:
			return fmt.Errorf("bundle path %q has unsupported file type", rel)
		}
		total += int64(len(content))
		if total > maxBundleBytes {
			return fmt.Errorf("bundle exceeds 32 MiB")
		}
		hash := sha256.Sum256(content)
		files = append(files, treeFile{agentversion.Entry{Path: rel, Mode: mode, Size: int64(len(content)), ContentSHA256: fmt.Sprintf("sha256:%x", hash)}, content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
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
