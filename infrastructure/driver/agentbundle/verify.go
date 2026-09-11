package agentbundle

import (
	"context"
	"strings"
)

// VerifyRepository checks all durable Git refs and reachable objects without
// repairing or deleting anything. Used during an offline backup restore drill.
func (s *Store) VerifyRepository(ctx context.Context, tenant, agent string) error {
	if err := validateIDs(tenant, agent); err != nil {
		return err
	}
	_, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(tenant, agent), "fsck", "--full", "--no-reflogs")
	return err
}

func (s *Store) VerifyCandidateHead(ctx context.Context, c Candidate) error {
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
	head, err := s.run(ctx, "", nil, "--git-dir", repo, "rev-parse", "--verify", trainingRef(c)+"/head")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != c.Head {
		return ErrInvalidBundle
	}
	return nil
}
