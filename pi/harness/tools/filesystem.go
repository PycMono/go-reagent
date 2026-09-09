package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
)

// Root is the filesystem root shared by workspace-aware tools.
type Root string

// Workspace owns the guarded root shared by workspace-aware tools.
type Workspace struct {
	path       string
	root       *os.Root
	policy     *workspacepolicy.Normalized
	writeRoots map[string]*os.Root
	writeOrder []string
	closeOnce  sync.Once
	closeErr   error
}

// NewWorkspace opens workDir once and closes it with the application lifecycle.
func NewWorkspace(workDir Root) (*Workspace, error) {
	workspace, err := openWorkspace(workDir)
	if err != nil {
		return nil, err
	}
	policy, err := workspacepolicy.Normalize(workspace.path, workspacepolicy.Policy{WriteMode: workspacepolicy.All})
	if err != nil {
		_ = workspace.root.Close()
		return nil, fmt.Errorf("%w: 创建工作区策略失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}
	workspace.policy = policy
	return workspace, nil
}

// NewWorkspaceWithPolicy opens a workspace with mutations confined to the
// policy's independently rooted writable subtrees.
func NewWorkspaceWithPolicy(workDir Root, policy *workspacepolicy.Normalized) (*Workspace, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: workspace policy 不能为空", pierrors.ErrWorkspaceInvalid)
	}
	workspace, err := openWorkspace(workDir)
	if err != nil {
		return nil, err
	}
	if workspace.path != policy.Root() {
		_ = workspace.root.Close()
		return nil, fmt.Errorf("%w: workspace policy root 不匹配", pierrors.ErrWorkspaceInvalid)
	}
	workspace.policy = policy
	if policy.Mode() == workspacepolicy.All {
		return workspace, nil
	}

	workspace.writeRoots = make(map[string]*os.Root, len(policy.Prefixes()))
	for _, prefix := range policy.Prefixes() {
		root, err := openTrustedPrefix(workspace.root, prefix)
		if err != nil {
			_ = workspace.closeRoots()
			return nil, fmt.Errorf("打开可写目录 %q 失败: %w", prefix, err)
		}
		workspace.writeRoots[prefix] = root
		workspace.writeOrder = append(workspace.writeOrder, prefix)
	}
	return workspace, nil
}

func openTrustedPrefix(workspaceRoot *os.Root, prefix string) (*os.Root, error) {
	parent := workspaceRoot
	var opened []*os.Root
	closeOpened := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}

	for _, component := range strings.Split(prefix, "/") {
		before, err := parent.Lstat(component)
		if errors.Is(err, fs.ErrNotExist) {
			if err := parent.Mkdir(component, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				closeOpened()
				return nil, err
			}
			before, err = parent.Lstat(component)
		}
		if err != nil {
			closeOpened()
			return nil, err
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			closeOpened()
			return nil, fmt.Errorf("component %q is not a trusted directory: %w", component, fs.ErrPermission)
		}

		child, err := parent.OpenRoot(component)
		if err != nil {
			closeOpened()
			return nil, err
		}
		after, afterErr := parent.Lstat(component)
		bound, boundErr := child.Stat(".")
		if afterErr != nil || boundErr != nil || after.Mode()&os.ModeSymlink != 0 ||
			!after.IsDir() || !os.SameFile(before, after) || !os.SameFile(after, bound) {
			_ = child.Close()
			closeOpened()
			if afterErr != nil {
				return nil, afterErr
			}
			if boundErr != nil {
				return nil, boundErr
			}
			return nil, fmt.Errorf("component %q changed while opening: %w", component, fs.ErrPermission)
		}
		opened = append(opened, child)
		parent = child
	}

	result := opened[len(opened)-1]
	for i := len(opened) - 2; i >= 0; i-- {
		_ = opened[i].Close()
	}
	return result, nil
}

func openWorkspace(workDir Root) (*Workspace, error) {
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
	mutating := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
	if mutating {
		if w.policy.Mode() == workspacepolicy.All {
			if err := w.guard(path); err != nil {
				return nil, err
			}
			return w.root.OpenFile(path, flag, perm)
		}
		target, relative, err := w.writeTarget(path)
		if err != nil {
			return nil, err
		}
		if err := rejectExistingNonRegular(target, relative); err != nil {
			return nil, err
		}
		return target.OpenFile(relative, flag, perm)
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
	if w.policy.Mode() == workspacepolicy.All {
		if err := w.guard(path); err != nil {
			return err
		}
		return w.root.MkdirAll(path, perm)
	}
	for _, prefix := range w.writeOrder {
		if path == prefix {
			return nil
		}
	}
	target, relative, err := w.writeTarget(path)
	if err != nil {
		return err
	}
	return target.MkdirAll(relative, perm)
}

func (w *Workspace) Remove(path string) error {
	path, err := cleanRelativePath(path, true)
	if err != nil {
		return err
	}
	if w.policy.Mode() == workspacepolicy.All {
		if err := w.guard(path); err != nil {
			return err
		}
		return w.root.Remove(path)
	}
	target, relative, err := w.writeTarget(path)
	if err != nil {
		return err
	}
	if err := rejectExistingNonRegular(target, relative); err != nil {
		return err
	}
	return target.Remove(relative)
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
		w.closeErr = w.closeRoots()
	})
	return w.closeErr
}

func (w *Workspace) closeRoots() error {
	var errs []error
	for i := len(w.writeOrder) - 1; i >= 0; i-- {
		if err := w.writeRoots[w.writeOrder[i]].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if w.root != nil {
		if err := w.root.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *Workspace) writeTarget(path string) (*os.Root, string, error) {
	prefix, relative, err := w.policy.MatchWrite(filepath.ToSlash(path))
	if err != nil {
		return nil, "", err
	}
	if w.policy.Mode() == workspacepolicy.All {
		return w.root, filepath.FromSlash(relative), nil
	}
	root := w.writeRoots[prefix]
	if root == nil || relative == "" {
		return nil, "", fmt.Errorf("workspace write %q: %w", path, fs.ErrPermission)
	}
	return root, filepath.FromSlash(relative), nil
}

func rejectExistingNonRegular(root *os.Root, path string) error {
	info, err := root.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("workspace mutation %q: non-regular file: %w", path, fs.ErrPermission)
	}
	return nil
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
