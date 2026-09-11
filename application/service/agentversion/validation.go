package agentversion

import (
	"context"
	"errors"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"time"
)

// ValidationPolicy identifies the effective sandbox and pinned interpreter policy.
// Environment must change whenever sandbox, interpreter or dependency pins change.
type ValidationPolicy struct {
	Version     string        `json:"version"`
	Environment string        `json:"environment"`
	Timeout     time.Duration `json:"timeout"`
	TTL         time.Duration `json:"ttl"`
}
type ReferenceCheck func(context.Context, Snapshot) error
type SmokeCheck func(context.Context, string, Snapshot) error
type Clock func(context.Context) (time.Time, error)
type ValidationAdmission func(context.Context, string, string) (func(), error)
type workspaceVerifier interface {
	VerifyValidationWorkspace(context.Context, string, string, string, bundle.BundleRef) error
}

type Validator struct {
	bundles    bundle.Store
	references ReferenceCheck
	smoke      SmokeCheck
	clock      Clock
	policy     ValidationPolicy
	digest     string
	admission  ValidationAdmission
}

func NewValidator(b bundle.Store, r ReferenceCheck, s SmokeCheck, c Clock, p ValidationPolicy, admission ...ValidationAdmission) (*Validator, error) {
	if b == nil || r == nil || s == nil || c == nil || p.Version == "" || p.Environment == "" || p.Timeout <= 0 || p.TTL <= 0 {
		return nil, errors.New("complete validation dependencies required")
	}
	raw, err := canonicalMarshal(p)
	if err != nil {
		return nil, err
	}
	v := &Validator{bundles: b, references: r, smoke: s, clock: c, policy: p, digest: digestBytes(raw)}
	if len(admission) > 1 {
		return nil, errors.New("one validation admission required")
	}
	if len(admission) == 1 {
		v.admission = admission[0]
	}
	return v, nil
}
func (v *Validator) Validate(ctx context.Context, tenant, agent string, ref bundle.BundleRef, workspace string, s Snapshot) (ValidationReport, error) {
	report := ValidationReport{Head: ref.Commit, DigestVersion: 1, BundleDigest: ref.Digest, ValidatorVersion: v.policy.Version, ValidationPolicyDigest: v.digest, ReviewMode: "manual", Diagnostics: []Diagnostic{}}
	if v.admission != nil {
		release, err := v.admission(ctx, tenant, agent)
		if err != nil {
			return report, err
		}
		defer release()
	}
	spec, err := SpecDigest(ref.Digest, s)
	if err != nil {
		return report, err
	}
	report.SpecDigest = spec
	if err = v.bundles.Verify(ctx, tenant, agent, ref); err != nil {
		return report, err
	}
	verifier, ok := v.bundles.(workspaceVerifier)
	if !ok {
		return report, errors.New("materialized validation verification unavailable")
	}
	if err = verifier.VerifyValidationWorkspace(ctx, tenant, agent, workspace, ref); err != nil {
		return report, err
	}
	if err = v.references(ctx, s); err != nil {
		return report, err
	}
	smokeCtx, cancel := context.WithTimeout(ctx, v.policy.Timeout)
	defer cancel()
	if err = v.smoke(smokeCtx, workspace, s); err != nil {
		return report, err
	}
	if err = smokeCtx.Err(); err != nil {
		return report, err
	}
	// Recheck immutable content after the sandbox execution before issuing evidence.
	if err = v.bundles.Verify(ctx, tenant, agent, ref); err != nil {
		return report, err
	}
	if err = verifier.VerifyValidationWorkspace(ctx, tenant, agent, workspace, ref); err != nil {
		return report, err
	}
	now, err := v.clock(ctx)
	if err != nil {
		return report, err
	}
	report.ValidatedAt = now.UTC()
	report.ValidUntil = report.ValidatedAt.Add(v.policy.TTL)
	report.Passed = true
	return report, nil
}
