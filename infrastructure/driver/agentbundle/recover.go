package agentbundle

import (
	"context"
	"os"
	"path/filepath"
)

// RecoverCandidate is an offline takeover operation. The caller must first
// confirm that the previous server and all of its subprocesses have stopped.
// The database checkpoint is authoritative; later Git refs remain diagnostic.
func (s *Store) RecoverCandidate(ctx context.Context, c Candidate) error {
	if err := validateIDs(c.TenantID, c.AgentID, c.TrainingID); err != nil {
		return err
	}
	if !lowerHex(c.Head, 40) {
		return ErrInvalidBundle
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	if _, err := s.readCommit(ctx, repo, c.Head); err != nil {
		return err
	}
	root := s.candidatePath(c)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidBundle
		}
		if err := os.Chmod(filepath.Join(root, ".git"), 0600); err != nil {
			return err
		}
		backup := root + "-interrupted-" + candidateID()
		if _, err := s.run(ctx, "", nil, "--git-dir", repo, "worktree", "move", root, backup); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(backup, ".git"), 0400); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := s.run(ctx, "", nil, "--git-dir", repo, "worktree", "add", "--detach", root, c.Head); err != nil {
		return err
	}
	if err := protectCandidate(root); err != nil {
		return err
	}
	_, err := s.run(ctx, "", nil, "--git-dir", repo, "update-ref", trainingRef(c)+"/head", c.Head)
	return err
}
