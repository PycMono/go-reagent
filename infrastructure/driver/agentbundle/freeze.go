package agentbundle

import (
	"context"
	"strings"

	"github.com/PycMono/go-reagent/application/service/agentversion"
)

// FreezeCandidate creates an immutable reference to the exact clean checkpoint.
// Callers hold the durable operation slot and have stopped every candidate writer.
func (s *Store) FreezeCandidate(ctx context.Context, c Candidate, id string) (BundleRef, error) {
	if err := validateIDs(id); err != nil {
		return BundleRef{}, err
	}
	root, err := s.checkCandidate(ctx, c)
	if err != nil {
		return BundleRef{}, err
	}
	if err := inspectWorkspaceStrict(ctx, root); err != nil {
		return BundleRef{}, err
	}
	actual, err := inspectSourceMode(ctx, root, true)
	if err != nil {
		return BundleRef{}, err
	}
	saved, err := s.readCommit(ctx, s.repoPath(c.TenantID, c.AgentID), c.Head)
	if err != nil {
		return BundleRef{}, err
	}
	digest := func(files []treeFile) (string, error) {
		entries := make([]agentversion.Entry, len(files))
		for i, f := range files {
			entries[i] = f.entry
		}
		return agentversion.BundleDigest(entries)
	}
	liveDigest, err := digest(actual)
	if err != nil {
		return BundleRef{}, err
	}
	savedDigest, err := digest(saved)
	if err != nil {
		return BundleRef{}, err
	}
	if liveDigest != savedDigest {
		return BundleRef{}, ErrInvalidBundle
	}
	ref := BundleRef{Commit: c.Head, Tag: "versions/" + id, Digest: liveDigest}
	// A lost response may retry the same immutable tag, but cannot move it.
	existing, e := s.RecoverInitial(ctx, c.TenantID, c.AgentID, id)
	if e == nil {
		if existing != ref {
			return BundleRef{}, ErrInvalidBundle
		}
		return existing, nil
	}
	if _, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(c.TenantID, c.AgentID), "update-ref", "refs/tags/"+ref.Tag, c.Head, strings.Repeat("0", 40)); err != nil {
		return BundleRef{}, err
	}
	return ref, s.Verify(ctx, c.TenantID, c.AgentID, ref)
}

func (s *Store) CandidatePath(ctx context.Context, c Candidate) (string, error) {
	return s.checkCandidate(ctx, c)
}
