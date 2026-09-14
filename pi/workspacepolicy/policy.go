// Package workspacepolicy defines the workspace write boundary shared by the
// SDK's file and process isolation layers.
package workspacepolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Mode string

const (
	Restricted Mode = "restricted"
	All        Mode = "all"
)

type Policy struct {
	WriteMode        Mode
	WritablePrefixes []string
}

type Normalized struct {
	root     string
	mode     Mode
	prefixes []string
}

func Normalize(root string, policy Policy) (*Normalized, error) {
	if policy.WriteMode != Restricted && policy.WriteMode != All {
		return nil, fmt.Errorf("workspace policy: unsupported write mode %q", policy.WriteMode)
	}
	if policy.WriteMode == All && len(policy.WritablePrefixes) != 0 {
		return nil, errors.New("workspace policy: all mode cannot have writable prefixes")
	}

	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("workspace policy: root: %w", err)
	}

	prefixSet := make(map[string]struct{}, len(policy.WritablePrefixes))
	for _, prefix := range policy.WritablePrefixes {
		clean, err := cleanRelativePath(prefix)
		if err != nil {
			return nil, fmt.Errorf("workspace policy: writable prefix %q: %w", prefix, err)
		}
		if err := validatePrefixAncestors(canonicalRoot, clean); err != nil {
			return nil, fmt.Errorf("workspace policy: writable prefix %q: %w", prefix, err)
		}
		prefixSet[clean] = struct{}{}
	}

	prefixes := make([]string, 0, len(prefixSet))
	for prefix := range prefixSet {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	prefixes = removeCoveredPrefixes(prefixes)

	return &Normalized{root: canonicalRoot, mode: policy.WriteMode, prefixes: prefixes}, nil
}

func (n *Normalized) Root() string {
	return n.root
}

func (n *Normalized) Mode() Mode {
	return n.mode
}

func (n *Normalized) Prefixes() []string {
	return append([]string(nil), n.prefixes...)
}

func (n *Normalized) MatchWrite(target string) (prefix, relative string, err error) {
	clean, err := cleanRelativePath(target)
	if err != nil {
		return "", "", fmt.Errorf("workspace write %q: %w", target, fs.ErrPermission)
	}
	if n.mode == All {
		return "", clean, nil
	}
	for _, allowed := range n.prefixes {
		if strings.HasPrefix(clean, allowed+"/") {
			return allowed, strings.TrimPrefix(clean, allowed+"/"), nil
		}
	}
	return "", "", fmt.Errorf("workspace write %q: %w", target, fs.ErrPermission)
}

func canonicalDirectory(root string) (string, error) {
	if root == "" || strings.IndexByte(root, 0) >= 0 {
		return "", errors.New("path is empty or contains NUL")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(canonical), nil
}

func cleanRelativePath(name string) (string, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return "", errors.New("path is empty or contains NUL")
	}
	if strings.ContainsRune(name, '\\') || filepath.IsAbs(name) || path.IsAbs(name) || hasVolume(name) {
		return "", errors.New("path must be relative and use slash separators")
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." {
			return "", errors.New("path contains an unsafe component")
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", errors.New("path cannot name the workspace root")
	}
	return clean, nil
}

func hasVolume(name string) bool {
	if filepath.VolumeName(name) != "" {
		return true
	}
	return len(name) >= 2 && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':'
}

func validatePrefixAncestors(root, prefix string) error {
	current := root
	for _, component := range strings.Split(prefix, "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("path contains a symbolic link")
		}
		if !info.IsDir() {
			return errors.New("path contains a non-directory")
		}
	}
	return nil
}

func removeCoveredPrefixes(sorted []string) []string {
	result := make([]string, 0, len(sorted))
	for _, candidate := range sorted {
		covered := false
		for _, parent := range result {
			if strings.HasPrefix(candidate, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, candidate)
		}
	}
	return result
}
