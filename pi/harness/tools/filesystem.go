package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"go.uber.org/fx"
)

// Root is the filesystem root shared by workspace-aware tools.
type Root string

// Workspace owns the guarded root shared by workspace-aware tools.
type Workspace struct {
	path      string
	root      *os.Root
	closeOnce sync.Once
	closeErr  error
}

// NewWorkspace opens workDir once and closes it with the application lifecycle.
func NewWorkspace(lifecycle fx.Lifecycle, workDir Root) (*Workspace, error) {
	path := strings.TrimSpace(string(workDir))
	if path == "" {
		return nil, fmt.Errorf("%w: workDir 不能为空", pierrors.ErrWorkspaceInvalid)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: 解析工作区失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, fmt.Errorf("%w: 解析工作区真实路径失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("%w: 检查工作区失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: workDir 必须是目录", pierrors.ErrWorkspaceInvalid)
	}

	root, err := os.OpenRoot(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("%w: 打开工作区失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}

	workspace := &Workspace{path: resolvedPath, root: root}
	lifecycle.Append(fx.Hook{OnStop: func(_ context.Context) error { return workspace.Close() }})
	return workspace, nil
}

func (w *Workspace) Open(path string) (*os.File, error) {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return nil, err
	}
	if err := w.guard(path); err != nil {
		return nil, err
	}
	return w.root.Open(path)
}

func (w *Workspace) OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return nil, err
	}
	if err := w.guard(path); err != nil {
		return nil, err
	}
	return w.root.OpenFile(path, flag, perm)
}

func (w *Workspace) ReadFile(path string) ([]byte, error) {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return nil, err
	}
	if err := w.guard(path); err != nil {
		return nil, err
	}
	return w.root.ReadFile(path)
}

// Stat follows only in-workspace links through the guarded root.
func (w *Workspace) Stat(path string) (fs.FileInfo, error) {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return nil, err
	}
	if err := w.guard(path); err != nil {
		return nil, err
	}
	return w.root.Stat(path)
}

func (w *Workspace) MkdirAll(path string, perm fs.FileMode) error {
	path, err := cleanRelativePath(path, false)
	if err != nil {
		return err
	}
	if err := w.guard(path); err != nil {
		return err
	}
	return w.root.MkdirAll(path, perm)
}

func (w *Workspace) Remove(path string) error {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return err
	}
	if err := w.guard(path); err != nil {
		return err
	}
	return w.root.Remove(path)
}

func (w *Workspace) Rename(oldPath, newPath string) error {
	oldPath, err := cleanRelativePath(oldPath, true)
	if err != nil {
		return err
	}
	newPath, err = cleanRelativePath(newPath, true)
	if err != nil {
		return err
	}
	if err := w.guard(oldPath); err != nil {
		return err
	}
	if err := w.guard(newPath); err != nil {
		return err
	}
	return w.root.Rename(oldPath, newPath)
}

func (w *Workspace) ResolveDir(path string) (string, error) {
	path, err := cleanRelativePath(path, false)
	if err != nil {
		return "", err
	}
	if err := w.guard(path); err != nil {
		return "", err
	}
	file, err := w.root.Open(path)
	if err != nil {
		return "", err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return "", statErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if !info.IsDir() {
		return "", errors.New("path 必须是已有目录")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(w.path, path))
	if err != nil {
		return "", fmt.Errorf("解析目录失败: %w", err)
	}
	relative, err := filepath.Rel(w.path, resolved)
	if err != nil {
		return "", fmt.Errorf("检查目录边界失败: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("path 不能逃逸工作区")
	}
	return resolved, nil
}

func (w *Workspace) Close() error {
	w.closeOnce.Do(func() {
		w.closeErr = w.root.Close()
	})
	return w.closeErr
}

func (w *Workspace) guard(path string) error {
	current := w.path
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("检查工作区路径失败: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			return fmt.Errorf("解析路径链接失败: %w", err)
		}
		relative, err := filepath.Rel(w.path, resolved)
		if err != nil {
			return fmt.Errorf("检查路径边界失败: %w", err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return errors.New("path 不能通过工作区外的符号链接")
		}
		current = resolved
	}
	return nil
}
