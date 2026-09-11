package agentbundle

import (
	"context"
	"crypto/rand"
	"fmt"
	port "github.com/PycMono/go-reagent/application/port/agentbundle"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Candidate = port.Candidate
type CheckpointMetadata = port.CheckpointMetadata
type Checkpoint = port.Checkpoint
type Diff = port.Diff
type PreparationArtifacts = port.PreparationArtifacts

var _ port.CandidateStore = (*Store)(nil)

func trainingRef(c Candidate) string { return "refs/training/" + c.TrainingID }
func (s *Store) candidatePath(c Candidate) string {
	return filepath.Join(s.agentRoot(c.TenantID, c.AgentID), "training", c.TrainingID, "candidate")
}
func (s *Store) checkCandidate(ctx context.Context, c Candidate) (string, error) {
	if err := validateIDs(c.TenantID, c.AgentID, c.TrainingID); err != nil {
		return "", err
	}
	if !lowerHex(c.Head, 40) {
		return "", ErrInvalidBundle
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	if _, err := s.run(ctx, "", nil, "check-ref-format", trainingRef(c)+"/head"); err != nil {
		return "", ErrInvalidBundle
	}
	head, err := s.run(ctx, "", nil, "--git-dir", repo, "rev-parse", "--verify", trainingRef(c)+"/head")
	if err != nil || strings.TrimSpace(head) != c.Head {
		return "", fmt.Errorf("%w: candidate head conflict", ErrInvalidBundle)
	}
	root := s.candidatePath(c)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return "", ErrInvalidBundle
	}
	control := filepath.Join(root, ".git")
	info, err = os.Lstat(control)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0400 || linkCount(info) > 1 {
		return "", ErrInvalidBundle
	}
	gitdir, err := s.run(ctx, root, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	backlink, err := os.ReadFile(filepath.Join(strings.TrimSpace(gitdir), "gitdir"))
	canonicalControl, e := filepath.EvalSymlinks(control)
	canonicalBacklink, e2 := filepath.EvalSymlinks(strings.TrimSpace(string(backlink)))
	if err != nil || e != nil || e2 != nil || canonicalControl != canonicalBacklink {
		return "", ErrInvalidBundle
	}
	actual, err := s.run(ctx, root, nil, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(strings.TrimSpace(actual))
	expected, _ := filepath.EvalSymlinks(repo)
	if err != nil || resolved != expected {
		return "", ErrInvalidBundle
	}
	return root, nil
}
func (s *Store) CreateCandidate(ctx context.Context, tenant, agent, training string, base BundleRef) (string, error) {
	c := Candidate{tenant, agent, training, base.Commit}
	if err := validateIDs(tenant, agent, training); err != nil {
		return "", err
	}
	if err := s.Verify(ctx, tenant, agent, base); err != nil {
		return "", err
	}
	if _, err := s.run(ctx, "", nil, "check-ref-format", trainingRef(c)+"/head"); err != nil {
		return "", ErrInvalidBundle
	}
	root := s.candidatePath(c)
	if _, err := os.Lstat(root); err == nil {
		return s.checkCandidate(ctx, c)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return "", err
	}
	if _, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(tenant, agent), "worktree", "add", "--detach", root, base.Commit); err != nil {
		return "", err
	}
	if err := protectCandidate(root); err != nil {
		return "", err
	}
	if _, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(tenant, agent), "update-ref", trainingRef(c)+"/head", base.Commit, strings.Repeat("0", 40)); err != nil {
		return "", err
	}
	return root, nil
}
func protectCandidate(root string) error {
	for _, d := range []string{"skills", "documents", "assets", ".tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0700); err != nil {
			return err
		}
	}
	return os.Chmod(filepath.Join(root, ".git"), 0400)
}
func candidateID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", b)
}

var credentialPattern = regexp.MustCompile(`(?i)(-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|\bAKIA[A-Z0-9]{16}\b|\bsk-[a-zA-Z0-9_-]{20,}|(?:api[_-]?key|password|access[_-]?token|secret)\s*[=:]\s*["']?[^\s"']{8,})`)

func candidateSecret(path string, b []byte) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == ".env" || strings.HasPrefix(base, ".env.") || base == "id_rsa" || base == "id_ed25519" || credentialPattern.Match(b)
}
