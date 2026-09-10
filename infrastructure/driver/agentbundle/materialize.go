package agentbundle

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/service/agentversion"
)

func (s *Store) MaterializeVersion(ctx context.Context, tenant, agent, version string, ref BundleRef) (string, error) {
	return s.materialize(ctx, tenant, agent, version, "", ref, false)
}
func (s *Store) MaterializeChat(ctx context.Context, tenant, agent, conversation, version string, ref BundleRef) (string, error) {
	if err := validateIDs(conversation); err != nil {
		return "", err
	}
	return s.materialize(ctx, tenant, agent, version, conversation, ref, true)
}

func (s *Store) materialize(ctx context.Context, tenant, agent, version, conversation string, ref BundleRef, chat bool) (string, error) {
	if err := validateIDs(tenant, agent, version); err != nil {
		return "", err
	}
	if err := s.Verify(ctx, tenant, agent, ref); err != nil {
		return "", err
	}
	base := filepath.Join(s.agentRoot(tenant, agent), "versions")
	if chat {
		base = filepath.Join(s.agentRoot(tenant, agent), "runtime-cache", "chat", conversation)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	destination := filepath.Join(base, version)
	if _, err := os.Stat(destination); err == nil {
		if err := s.verifyMaterialized(ctx, destination, tenant, agent, ref, chat); err != nil {
			return "", err
		}
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp, err := os.MkdirTemp(base, ".materialize-")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(tmp)
		}
	}()
	files, err := s.readCommit(ctx, s.repoPath(tenant, agent), ref.Commit)
	if err != nil {
		return "", err
	}
	if err := writeTreeFiles(tmp, files); err != nil {
		return "", err
	}
	if err := inspectWorkspaceStrict(ctx, tmp); err != nil {
		return "", err
	}
	if chat {
		for _, name := range []string{"scratch", ".tmp"} {
			if err := os.Mkdir(filepath.Join(tmp, name), 0o700); err != nil {
				return "", err
			}
		}
	}
	if err := makeDirsReadOnly(tmp, chat); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, destination); err != nil {
		return "", err
	}
	keep = true
	return destination, nil
}

func (s *Store) verifyMaterialized(ctx context.Context, root, tenant, agent string, ref BundleRef, chat bool) error {
	files, err := s.readCommit(ctx, s.repoPath(tenant, agent), ref.Commit)
	if err != nil {
		return err
	}
	expected := make(map[string]treeFile, len(files))
	for _, file := range files {
		expected[file.entry.Path] = file
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(name string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		first := strings.Split(rel, "/")[0]
		if chat && (first == "scratch" || first == ".tmp") {
			if rel == first && !d.IsDir() {
				return ErrInvalidBundle
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return nil
		}
		want, ok := expected[rel]
		if !ok {
			return ErrInvalidBundle
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		var content []byte
		if want.entry.Mode == "120000" {
			if info.Mode()&os.ModeSymlink == 0 {
				return ErrInvalidBundle
			}
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			content = []byte(target)
		} else {
			if !info.Mode().IsRegular() || (want.entry.Mode == "100755") != (info.Mode()&0o111 != 0) || info.Mode().Perm()&0o222 != 0 {
				return ErrInvalidBundle
			}
			content, err = os.ReadFile(name)
			if err != nil {
				return err
			}
		}
		if !bytes.Equal(content, want.content) {
			return ErrInvalidBundle
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return ErrInvalidBundle
	}
	return inspectWorkspaceStrict(ctx, root)
}

func makeDirsReadOnly(root string, chat bool) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if chat && (path == filepath.Join(root, "scratch") || path == filepath.Join(root, ".tmp")) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			mode := os.FileMode(0o444)
			if info.Mode()&0o111 != 0 {
				mode = 0o555
			}
			return os.Chmod(path, mode)
		}
		return os.Chmod(path, 0o555)
	})
}

func (s *Store) readCommit(ctx context.Context, repo, commit string) ([]treeFile, error) {
	out, err := s.run(ctx, "", nil, "--git-dir", repo, "ls-tree", "-rz", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Split(splitNUL)
	var files []treeFile
	var total int64
	for scanner.Scan() {
		line := scanner.Text()
		meta, name, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, ErrInvalidBundle
		}
		parts := strings.Fields(meta)
		if len(parts) != 3 || parts[1] != "blob" || (parts[0] != "100644" && parts[0] != "100755" && parts[0] != "120000") {
			return nil, ErrInvalidBundle
		}
		if !agentPathAllowed(name) {
			return nil, ErrInvalidBundle
		}
		blob, err := s.runBytes(ctx, repo, "cat-file", "blob", parts[2])
		if err != nil {
			return nil, err
		}
		if len(blob) > maxBundleFile {
			return nil, ErrInvalidBundle
		}
		total += int64(len(blob))
		if total > maxBundleBytes || len(files) >= maxBundlePaths {
			return nil, ErrInvalidBundle
		}
		if parts[0] == "120000" && !safeLink(name, string(blob)) {
			return nil, ErrInvalidBundle
		}
		sum := sha256.Sum256(blob)
		files = append(files, treeFile{agentversion.Entry{Path: name, Mode: parts[0], Size: int64(len(blob)), ContentSHA256: fmt.Sprintf("sha256:%x", sum)}, blob})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func verifyTreeFiles(ctx context.Context, files []treeFile) error {
	root, err := os.MkdirTemp("", "go-reagent-bundle-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := writeTreeFiles(root, files); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.entry.Mode != "120000" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(file.entry.Path)))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(canonical, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ErrInvalidBundle
		}
	}
	return inspectWorkspaceStrict(ctx, root)
}

func writeTreeFiles(root string, files []treeFile) error {
	for _, file := range files {
		target := filepath.Join(root, filepath.FromSlash(file.entry.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		switch file.entry.Mode {
		case "120000":
			if err := os.Symlink(string(file.content), target); err != nil {
				return err
			}
		case "100755":
			if err := os.WriteFile(target, file.content, 0o755); err != nil {
				return err
			}
		default:
			if err := os.WriteFile(target, file.content, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) runBytes(ctx context.Context, repo string, args ...string) ([]byte, error) {
	all := append([]string{"--git-dir", repo}, args...)
	cmd := exec.CommandContext(ctx, s.git, all...)
	cmd.Env = gitEnv(s.root, nil)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}
func splitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := strings.IndexByte(string(data), 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
func agentPathAllowed(name string) bool {
	if !agentversionPath(name) {
		return false
	}
	first := strings.Split(name, "/")[0]
	return first != ".git" && first != ".tmp" && first != "scratch"
}
func safeLink(name, target string) bool {
	if filepath.IsAbs(target) {
		return false
	}
	clean := filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(name)), target))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
func agentversionPath(name string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	return name != "" && utf8.ValidString(name) && clean == name && name != "." && !strings.HasPrefix(name, "../") && !strings.ContainsRune(name, 0)
}
