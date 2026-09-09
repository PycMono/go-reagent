package sandbox

import (
	"fmt"
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

type disabledRunner struct {
	policy Policy
}

func (r *disabledRunner) Policy() Policy {
	return clonePolicy(r.policy)
}

func (r *disabledRunner) BuildShell(string, CommandSpec) (*exec.Cmd, error) {
	return nil, ErrProcessDisabled
}

func (r *disabledRunner) BuildArgv([]string, CommandSpec) (*exec.Cmd, error) {
	return nil, ErrProcessDisabled
}

// prepareWritableDirectories traverses anchored directories one component at a
// time; it never follows a pre-existing symlink or unchecked ancestor.
func prepareWritableDirectories(n *workspacepolicy.Normalized) error {
	prefixes := n.Prefixes()
	if n.Mode() == workspacepolicy.All {
		prefixes = []string{".tmp"}
	}
	for _, prefix := range prefixes {
		dir, err := os.OpenRoot(n.Root())
		if err != nil {
			return err
		}
		for _, component := range strings.Split(prefix, "/") {
			info, e := dir.Lstat(component)
			if os.IsNotExist(e) {
				e = dir.Mkdir(component, 0700)
				if e == nil {
					info, e = dir.Lstat(component)
				}
			}
			if e != nil {
				dir.Close()
				return e
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				dir.Close()
				return fmt.Errorf("unsafe writable directory %s", prefix)
			}
			next, e := dir.OpenRoot(component)
			dir.Close()
			if e != nil {
				return e
			}
			opened, e := next.Stat(".")
			if e != nil {
				next.Close()
				return e
			}
			if !os.SameFile(info, opened) {
				next.Close()
				return fmt.Errorf("writable directory changed during preparation: %s", prefix)
			}
			dir = next
		}
		if prefix == ".tmp" {
			if err := dir.Chmod(".", 0700); err != nil {
				dir.Close()
				return err
			}
		}
		dir.Close()
	}
	return validateWritableLinks(n)
}

// Writable aliases to shared inodes cannot be isolated by path based native
// sandboxes. Fail closed before launch instead of allowing write-through.
func validateWritableLinks(n *workspacepolicy.Normalized) error {
	if n.Mode() != workspacepolicy.Restricted {
		return nil
	}
	for _, prefix := range n.Prefixes() {
		err := filepath.WalkDir(filepath.Join(n.Root(), prefix), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			v := reflect.Indirect(reflect.ValueOf(info.Sys()))
			if v.IsValid() && v.Kind() == reflect.Struct {
				links := v.FieldByName("Nlink")
				if links.IsValid() && links.CanUint() && links.Uint() > 1 {
					return fmt.Errorf("writable hardlink rejected: %s", path)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func validateNativePolicy(root string, n *workspacepolicy.Normalized) error {
	if n == nil {
		return fmt.Errorf("%w: nil workspace policy", ErrWritePolicyUnsupported)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	if canonical != n.Root() {
		return fmt.Errorf("workspace policy root mismatch")
	}
	if n.Mode() == workspacepolicy.Restricted && !hasTmpPrefix(n.Prefixes()) {
		return fmt.Errorf("%w: explicit .tmp required", ErrWritePolicyUnsupported)
	}
	return prepareWritableDirectories(n)
}
