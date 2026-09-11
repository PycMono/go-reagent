package agentbundle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (s *Store) inputGit(ctx context.Context, repo string, env []string, input []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, s.git, append([]string{"--git-dir", repo}, args...)...)
	cmd.Env = gitEnv(s.root, env)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git plumbing: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
func (s *Store) candidateTree(ctx context.Context, c Candidate) (string, error) {
	root, err := s.checkCandidate(ctx, c)
	if err != nil {
		return "", err
	}
	files, err := inspectSourceMode(ctx, root, true)
	if err != nil {
		return "", err
	}
	index, err := os.CreateTemp(s.root, "candidate-index-")
	if err != nil {
		return "", err
	}
	index.Close()
	defer os.Remove(index.Name())
	env := []string{"GIT_INDEX_FILE=" + index.Name()}
	repo := s.repoPath(c.TenantID, c.AgentID)
	if _, err = s.run(ctx, "", env, "--git-dir", repo, "read-tree", "--empty"); err != nil {
		return "", err
	}
	var records bytes.Buffer
	for _, f := range files {
		oid, err := s.inputGit(ctx, repo, nil, f.content, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&records, "%s %s\t%s%c", f.entry.Mode, oid, f.entry.Path, 0)
	}
	if _, err = s.inputGit(ctx, repo, env, records.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return "", err
	}
	tree, err := s.run(ctx, "", env, "--git-dir", repo, "write-tree")
	return strings.TrimSpace(tree), err
}
func (s *Store) Checkpoint(ctx context.Context, c Candidate, m CheckpointMetadata) (Checkpoint, error) {
	if err := validateIDs(m.RunID, m.ActorID); err != nil || m.At.IsZero() {
		return Checkpoint{}, ErrInvalidBundle
	}
	tree, err := s.candidateTree(ctx, c)
	if err != nil {
		return Checkpoint{}, err
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	m.At = m.At.UTC()
	payload, _ := json.Marshal(m)
	commit, err := s.inputGit(ctx, repo, nil, payload, "commit-tree", tree, "-p", c.Head)
	if err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{candidateID(), commit, m}
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "update-ref", trainingRef(c)+"/checkpoints/"+cp.ID, commit, strings.Repeat("0", 40)); err != nil {
		return Checkpoint{}, err
	}
	if _, err = s.run(ctx, "", nil, "--git-dir", repo, "update-ref", trainingRef(c)+"/head", commit, c.Head); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}
func (s *Store) checkpoint(ctx context.Context, c Candidate, id string) (Checkpoint, error) {
	if !lowerHex(id, 32) {
		return Checkpoint{}, ErrInvalidBundle
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	commit, err := s.run(ctx, "", nil, "--git-dir", repo, "rev-parse", "--verify", trainingRef(c)+"/checkpoints/"+id)
	if err != nil {
		return Checkpoint{}, ErrInvalidBundle
	}
	commit = strings.TrimSpace(commit)
	body, err := s.run(ctx, "", nil, "--git-dir", repo, "show", "-s", "--format=%B", commit)
	if err != nil {
		return Checkpoint{}, err
	}
	var m CheckpointMetadata
	if err = json.Unmarshal([]byte(body), &m); err != nil {
		return Checkpoint{}, ErrInvalidBundle
	}
	return Checkpoint{id, commit, m}, nil
}
func (s *Store) ListCheckpoints(ctx context.Context, c Candidate, cursor string, limit int) ([]Checkpoint, string, error) {
	if _, err := s.checkCandidate(ctx, c); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	out, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(c.TenantID, c.AgentID), "for-each-ref", "--sort=refname", "--format=%(refname)", trainingRef(c)+"/checkpoints/")
	if err != nil {
		return nil, "", err
	}
	var result []Checkpoint
	for _, ref := range strings.Fields(out) {
		id := strings.TrimPrefix(ref, trainingRef(c)+"/checkpoints/")
		if id <= cursor {
			continue
		}
		if len(result) == limit {
			return result, result[len(result)-1].ID, nil
		}
		cp, err := s.checkpoint(ctx, c, id)
		if err != nil {
			return nil, "", err
		}
		result = append(result, cp)
	}
	return result, "", nil
}
func (s *Store) Diff(ctx context.Context, c Candidate, base string, maxBytes int) (Diff, error) {
	if !lowerHex(base, 40) {
		return Diff{}, ErrInvalidBundle
	}
	tree, err := s.candidateTree(ctx, c)
	if err != nil {
		return Diff{}, err
	}
	repo := s.repoPath(c.TenantID, c.AgentID)
	names, err := s.run(ctx, "", nil, "--git-dir", repo, "diff", "--name-only", "-z", base, tree)
	if err != nil {
		return Diff{}, err
	}
	out, err := s.run(ctx, "", nil, "--git-dir", repo, "diff", "--no-ext-diff", "--no-textconv", base, tree, "--")
	if err != nil {
		return Diff{}, err
	}
	if maxBytes < 1 || maxBytes > 1<<20 {
		maxBytes = 1 << 20
	}
	d := Diff{Text: out, ChangedPaths: strings.Count(names, "\x00")}
	if len(out) > maxBytes {
		d.Text = out[:maxBytes]
		d.Truncated = true
	}
	return d, nil
}
