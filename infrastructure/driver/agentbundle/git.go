package agentbundle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	port "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
)

var ErrInvalidBundle = errors.New("invalid agent bundle")

type BundleRef = port.BundleRef
type Store struct {
	root string
	git  string
}

var _ port.Store = (*Store)(nil)

func New(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	return &Store{root: root, git: git}, nil
}

func (s *Store) CreateInitial(ctx context.Context, tenant, agent, version, source string) (BundleRef, error) {
	if err := validateIDs(tenant, agent, version); err != nil {
		return BundleRef{}, err
	}
	if err := sourceClean(ctx, s.git, source); err != nil {
		return BundleRef{}, err
	}
	files, err := inspectSource(ctx, source)
	if err != nil {
		return BundleRef{}, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	entries := make([]agentversion.Entry, len(files))
	for i := range files {
		entries[i] = files[i].entry
	}
	digest, err := agentversion.BundleDigest(entries)
	if err != nil {
		return BundleRef{}, err
	}
	repo := s.repoPath(tenant, agent)
	if err := os.MkdirAll(filepath.Dir(repo), 0o700); err != nil {
		return BundleRef{}, err
	}
	if _, err := os.Stat(repo); os.IsNotExist(err) {
		if _, err := s.run(ctx, "", nil, "init", "--bare", repo); err != nil {
			return BundleRef{}, err
		}
	} else if err != nil {
		return BundleRef{}, err
	}
	index, err := os.CreateTemp(s.root, "index-")
	if err != nil {
		return BundleRef{}, err
	}
	index.Close()
	defer os.Remove(index.Name())
	env := []string{"GIT_INDEX_FILE=" + index.Name()}
	if _, err := s.run(ctx, source, env, "--git-dir", repo, "read-tree", "--empty"); err != nil {
		return BundleRef{}, err
	}
	addArgs := []string{"--git-dir", repo, "--work-tree", source, "add", "-f", "--"}
	for _, file := range files {
		addArgs = append(addArgs, file.entry.Path)
	}
	if _, err := s.run(ctx, source, env, addArgs...); err != nil {
		return BundleRef{}, err
	}
	tree, err := s.run(ctx, source, env, "--git-dir", repo, "write-tree")
	if err != nil {
		return BundleRef{}, err
	}
	commit, err := s.run(ctx, source, env, "--git-dir", repo, "commit-tree", strings.TrimSpace(tree))
	if err != nil {
		return BundleRef{}, err
	}
	commit = strings.TrimSpace(commit)
	tag := "versions/" + version
	if _, err := s.run(ctx, "", nil, "--git-dir", repo, "update-ref", "refs/tags/"+tag, commit, strings.Repeat("0", 40)); err != nil {
		return BundleRef{}, err
	}
	ref := BundleRef{Commit: commit, Tag: tag, Digest: digest}
	if err := s.Verify(ctx, tenant, agent, ref); err != nil {
		return BundleRef{}, err
	}
	return ref, nil
}

func (s *Store) Verify(ctx context.Context, tenant, agent string, ref BundleRef) error {
	if err := validateIDs(tenant, agent); err != nil {
		return err
	}
	if !validRef(ref) {
		return ErrInvalidBundle
	}
	repo := s.repoPath(tenant, agent)
	resolved, err := s.run(ctx, "", nil, "--git-dir", repo, "rev-parse", "--verify", "refs/tags/"+ref.Tag+"^{commit}")
	if err != nil || strings.TrimSpace(resolved) != ref.Commit {
		return fmt.Errorf("%w: tag or commit mismatch", ErrInvalidBundle)
	}
	files, err := s.readCommit(ctx, repo, ref.Commit)
	if err != nil {
		return err
	}
	if err := verifyTreeFiles(ctx, files); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	entries := make([]agentversion.Entry, len(files))
	for i := range files {
		entries[i] = files[i].entry
	}
	digest, err := agentversion.BundleDigest(entries)
	if err != nil || digest != ref.Digest {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidBundle)
	}
	return nil
}

func (s *Store) repoPath(tenant, agent string) string {
	return filepath.Join(s.agentRoot(tenant, agent), "bundle.git")
}
func (s *Store) agentRoot(tenant, agent string) string {
	return filepath.Join(s.root, "tenants", tenant, "agents", agent)
}
func validateIDs(ids ...string) error {
	for _, id := range ids {
		if !safeID(id) {
			return fmt.Errorf("%w: unsafe id %q", ErrInvalidBundle, id)
		}
	}
	return nil
}
func safeID(id string) bool {
	if id == "" || len(id) > 128 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.ContainsAny(id, "/\\") {
		return false
	}
	switch strings.ToLower(id) {
	case ".", "..", ".git", ".tmp", "scratch":
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validRef(ref BundleRef) bool {
	return lowerHex(ref.Commit, 40) && safeID(strings.TrimPrefix(ref.Tag, "versions/")) && strings.HasPrefix(ref.Tag, "versions/") && strings.HasPrefix(ref.Digest, "sha256:") && lowerHex(strings.TrimPrefix(ref.Digest, "sha256:"), 64)
}
func lowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func sourceClean(ctx context.Context, git, source string) error {
	if _, err := os.Stat(filepath.Join(source, ".git")); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.CommandContext(ctx, git, "-C", source, "status", "--porcelain=v1", "--untracked-files=all")
	cmd.Env = gitEnv(source, nil)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return fmt.Errorf("%w: source git tree is dirty or untracked", ErrInvalidBundle)
	}
	return nil
}

func (s *Store) run(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, s.git, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(s.root, extra)
	cmd.Stdin = strings.NewReader("agent bundle\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func gitEnv(home string, extra []string) []string {
	env := []string{"PATH=/usr/bin:/bin:/usr/local/bin", "HOME=" + home, "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=go-reagent", "GIT_AUTHOR_EMAIL=agent@localhost", "GIT_COMMITTER_NAME=go-reagent", "GIT_COMMITTER_EMAIL=agent@localhost", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	return append(env, extra...)
}
