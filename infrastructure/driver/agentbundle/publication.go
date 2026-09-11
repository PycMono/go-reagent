package agentbundle

import (
	"context"
	"encoding/json"
	"errors"
	port "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) CloneVersion(ctx context.Context, tenant, agent, version string, base BundleRef) (BundleRef, error) {
	if err := validateIDs(tenant, agent, version); err != nil {
		return BundleRef{}, err
	}
	if err := s.Verify(ctx, tenant, agent, base); err != nil {
		return BundleRef{}, err
	}
	ref := BundleRef{Commit: base.Commit, Tag: "versions/" + version, Digest: base.Digest}
	if _, err := s.run(ctx, "", nil, "--git-dir", s.repoPath(tenant, agent), "update-ref", "refs/tags/"+ref.Tag, ref.Commit, strings.Repeat("0", 40)); err != nil {
		return BundleRef{}, err
	}
	return ref, s.Verify(ctx, tenant, agent, ref)
}
func validateIntent(p port.PublicationIntent) error {
	if err := validateIDs(p.TenantID, p.AgentID, p.VersionID, p.BaseVersionID); err != nil {
		return err
	}
	if p.Tag != "versions/"+p.VersionID || p.Version < 2 || (p.Phase != "reserved" && p.Phase != "validated") || len(p.BundleDigest) != 71 || len(p.SpecDigest) != 71 {
		return errors.New("invalid publication intent")
	}
	return nil
}
func (s *Store) intentPath(p port.PublicationIntent) string {
	return filepath.Join(s.agentRoot(p.TenantID, p.AgentID), "state", "publications", p.VersionID+".json")
}
func (s *Store) Prepare(ctx context.Context, p port.PublicationIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateIntent(p); err != nil {
		return err
	}
	destination := s.intentPath(p)
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".intent-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), destination); err != nil {
		return err
	}
	return syncIntentDirectory(dir)
}
func (s *Store) List(ctx context.Context, tenant, agent string) ([]port.PublicationIntent, error) {
	if err := validateIDs(tenant, agent); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.agentRoot(tenant, agent), "state", "publications")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []port.PublicationIntent{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []port.PublicationIntent{}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(entry.Name(), ".intent-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, errors.New("unexpected publication intent entry")
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var p port.PublicationIntent
		if err = agentversion.DecodeStrict(raw, &p); err != nil {
			return nil, err
		}
		if err = validateIntent(p); err != nil {
			return nil, err
		}
		if p.TenantID != tenant || p.AgentID != agent || entry.Name() != p.VersionID+".json" {
			return nil, errors.New("publication intent scope mismatch")
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Store) Remove(ctx context.Context, p port.PublicationIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateIntent(p); err != nil {
		return err
	}
	path := s.intentPath(p)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncIntentDirectory(filepath.Dir(path))
}
func syncIntentDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
