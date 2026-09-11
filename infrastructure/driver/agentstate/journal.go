package agentstate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	stateport "github.com/PycMono/go-reagent/application/port/agentstate"
)

type Phase = stateport.Phase
type Preparation = stateport.Preparation

const (
	PhaseReserved     = stateport.PhaseReserved
	PhaseBundled      = stateport.PhaseBundled
	PhaseMaterialized = stateport.PhaseMaterialized
	PhaseValidated    = stateport.PhaseValidated
)

type Journal struct{ root string }

func New(root string) (*Journal, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Journal{root: root}, nil
}

func (j *Journal) Begin(ctx context.Context, tenant, agent, version string, expected uint64) (Preparation, error) {
	record := Preparation{SchemaVersion: 1, TenantID: tenant, AgentID: agent, VersionID: version, ExpectedRowVersion: expected, Phase: PhaseReserved, Tag: "versions/" + version}
	if err := j.validate(record); err != nil {
		return Preparation{}, err
	}
	record.TempSource = filepath.Join(j.root, "tenants", tenant, "agents", agent, "state", "preparations", version, "source")
	if err := os.MkdirAll(record.TempSource, 0o700); err != nil {
		return Preparation{}, err
	}
	if err := syncDirectory(filepath.Dir(record.TempSource)); err != nil {
		return Preparation{}, err
	}
	if err := j.Save(ctx, record); err != nil {
		return Preparation{}, err
	}
	return record, nil
}

func (j *Journal) Save(ctx context.Context, record Preparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = 1
	}
	if err := j.validate(record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	destination := j.path(record.TenantID, record.AgentID, record.VersionID)
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".preparation-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	remove := true
	defer func() {
		_ = tmp.Close()
		if remove {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return err
	}
	remove = false
	return syncDirectory(directory)
}

func (j *Journal) Load(ctx context.Context, tenant, agent, version string) (Preparation, error) {
	if err := ctx.Err(); err != nil {
		return Preparation{}, err
	}
	if !validID(tenant) || !validID(agent) || !validID(version) {
		return Preparation{}, errors.New("invalid preparation identity")
	}
	data, err := os.ReadFile(j.path(tenant, agent, version))
	if err != nil {
		return Preparation{}, err
	}
	var record Preparation
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Preparation{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Preparation{}, errors.New("trailing preparation data")
	}
	if record.TenantID != tenant || record.AgentID != agent || record.VersionID != version {
		return Preparation{}, errors.New("preparation identity mismatch")
	}
	if err := j.validate(record); err != nil {
		return Preparation{}, err
	}
	return record, nil
}

func (j *Journal) List(ctx context.Context) ([]Preparation, error) {
	var records []Preparation
	err := filepath.WalkDir(filepath.Join(j.root, "tenants"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(j.root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 7 || parts[0] != "tenants" || parts[2] != "agents" || parts[4] != "state" || parts[5] != "preparations" {
			return fmt.Errorf("unexpected preparation path %q", rel)
		}
		version := strings.TrimSuffix(parts[6], ".json")
		record, err := j.Load(ctx, parts[1], parts[3], version)
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Slice(records, func(i, k int) bool {
		a, b := records[i], records[k]
		if a.TenantID != b.TenantID {
			return a.TenantID < b.TenantID
		}
		if a.AgentID != b.AgentID {
			return a.AgentID < b.AgentID
		}
		return a.VersionID < b.VersionID
	})
	return records, err
}

func (j *Journal) Remove(ctx context.Context, tenant, agent, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validID(tenant) || !validID(agent) || !validID(version) {
		return errors.New("invalid preparation identity")
	}
	path := j.path(tenant, agent, version)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (j *Journal) Complete(ctx context.Context, record Preparation) error {
	return j.finish(ctx, record)
}

func (j *Journal) Discard(ctx context.Context, record Preparation) error {
	return j.finish(ctx, record)
}

func (j *Journal) finish(ctx context.Context, record Preparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := j.validate(record); err != nil {
		return err
	}
	if record.TempSource != "" {
		if err := os.RemoveAll(record.TempSource); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(record.TempSource)); err != nil {
			return err
		}
	}
	return j.Remove(ctx, record.TenantID, record.AgentID, record.VersionID)
}

func (j *Journal) path(tenant, agent, version string) string {
	return filepath.Join(j.root, "tenants", tenant, "agents", agent, "state", "preparations", version+".json")
}

func (j *Journal) validate(record Preparation) error {
	if record.SchemaVersion != 1 || !validID(record.TenantID) || !validID(record.AgentID) || !validID(record.VersionID) || record.Tag != "versions/"+record.VersionID {
		return errors.New("invalid preparation record")
	}
	switch record.Phase {
	case PhaseReserved, PhaseBundled, PhaseMaterialized, PhaseValidated:
	default:
		return errors.New("invalid preparation phase")
	}
	agentRoot := filepath.Join(j.root, "tenants", record.TenantID, "agents", record.AgentID)
	for _, owned := range []string{record.TempSource, record.Materialized} {
		if owned == "" {
			continue
		}
		absolute, err := filepath.Abs(owned)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(agentRoot, absolute)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("preparation reference is not owned")
		}
	}
	return nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\") || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

var _ stateport.Journal = (*Journal)(nil)
