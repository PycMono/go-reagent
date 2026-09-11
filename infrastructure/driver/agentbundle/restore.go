package agentbundle

import (
	"context"
	"errors"
	port "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Store) Restore(ctx context.Context, c Candidate, id string) (Checkpoint, error) {
	root, err := s.checkCandidate(ctx, c)
	if err != nil {
		return Checkpoint{}, err
	}
	cp, err := s.checkpoint(ctx, c, id)
	if err != nil {
		return Checkpoint{}, err
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	replacement := root + "-restore-" + candidateID()
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "worktree", "add", "--detach", replacement, cp.Head); err != nil {
		return Checkpoint{}, err
	}
	if err = protectCandidate(replacement); err != nil {
		return Checkpoint{}, err
	}
	if _, err = inspectSourceMode(ctx, replacement, true); err != nil {
		return Checkpoint{}, err
	}
	if err := os.Chmod(filepath.Join(root, ".git"), 0600); err != nil {
		return Checkpoint{}, err
	}
	defer os.Chmod(filepath.Join(root, ".git"), 0400)
	if err := os.Chmod(filepath.Join(replacement, ".git"), 0600); err != nil {
		return Checkpoint{}, err
	}
	backup := root + "-retained-" + candidateID()
	defer os.Chmod(filepath.Join(backup, ".git"), 0400)
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "worktree", "move", root, backup); err != nil {
		final, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, statErr := os.Lstat(root); os.IsNotExist(statErr) {
			if _, rollbackErr := s.run(final, "", nil, "--git-dir", repo, "worktree", "move", backup, root); rollbackErr != nil {
				return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, rollbackErr)
			}
		}
		_ = os.Chmod(filepath.Join(root, ".git"), 0400)
		if _, verifyErr := s.checkCandidate(final, c); verifyErr != nil {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, verifyErr)
		}
		return Checkpoint{}, err
	}
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "worktree", "move", replacement, root); err != nil {
		final, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, rollbackErr := s.run(final, "", nil, "--git-dir", repo, "worktree", "move", backup, root); rollbackErr != nil {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, rollbackErr)
		}
		return Checkpoint{}, err
	}
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "update-ref", trainingRef(c)+"/head", cp.Head, c.Head); err != nil {
		final, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		head, readErr := s.run(final, "", nil, "--git-dir", repo, "rev-parse", "--verify", trainingRef(c)+"/head")
		if readErr != nil {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, readErr)
		}
		if strings.TrimSpace(head) == cp.Head {
			return cp, nil
		}
		if strings.TrimSpace(head) != c.Head {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err)
		}
		if _, rollbackErr := s.run(final, "", nil, "--git-dir", repo, "worktree", "move", root, replacement); rollbackErr != nil {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, rollbackErr)
		}
		if _, rollbackErr := s.run(final, "", nil, "--git-dir", repo, "worktree", "move", backup, root); rollbackErr != nil {
			return Checkpoint{}, errors.Join(port.ErrRecoveryRequired, err, rollbackErr)
		}
		return Checkpoint{}, err
	}
	// Retain the previous directory for diagnosis. Once the HEAD CAS succeeds,
	// optional cleanup must not turn this committed restore into a failed result.
	return cp, nil
}
func (s *Store) CleanupPreparation(ctx context.Context, p PreparationArtifacts) error {
	if err := validateIDs(p.TenantID, p.AgentID, p.VersionID); err != nil {
		return err
	}
	base := s.agentRoot(p.TenantID, p.AgentID)
	paths := []string{filepath.Join(base, "versions", p.VersionID), filepath.Join(base, "runtime-cache", "validation", p.VersionID)}
	for _, path := range paths {
		for curr := path; curr != s.root; curr = filepath.Dir(curr) {
			info, err := os.Lstat(curr)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return ErrInvalidBundle
			}
		}
	}
	if p.Ref.Commit != "" {
		if p.Ref.Tag != "versions/"+p.VersionID {
			return ErrInvalidBundle
		}
		if err := s.Verify(ctx, p.TenantID, p.AgentID, p.Ref); err != nil {
			return err
		}
		if _, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(p.TenantID, p.AgentID), "update-ref", "-d", "refs/tags/"+p.Ref.Tag, p.Ref.Commit); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if err := filepath.WalkDir(path, func(name string, d os.DirEntry, e error) error {
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if d.IsDir() {
				return os.Chmod(name, 0700)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) RecoverInitial(ctx context.Context, tenant, agent, version string) (BundleRef, error) {
	if err := validateIDs(tenant, agent, version); err != nil {
		return BundleRef{}, err
	}
	repo := s.repoPath(tenant, agent)
	commit, err := s.run(ctx, "", nil, "--git-dir", repo, "rev-parse", "--verify", "refs/tags/versions/"+version)
	if err != nil {
		return BundleRef{}, os.ErrNotExist
	}
	files, err := s.readCommit(ctx, repo, strings.TrimSpace(commit))
	if err != nil {
		return BundleRef{}, err
	}
	entries := make([]agentversion.Entry, len(files))
	for i := range files {
		entries[i] = files[i].entry
	}
	digest, err := agentversion.BundleDigest(entries)
	if err != nil {
		return BundleRef{}, err
	}
	ref := BundleRef{Commit: strings.TrimSpace(commit), Tag: "versions/" + version, Digest: digest}
	return ref, s.Verify(ctx, tenant, agent, ref)
}
