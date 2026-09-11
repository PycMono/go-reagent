package agentruntime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	trainingtools "github.com/PycMono/go-reagent/application/tool/agenttraining"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
)

type PIFactory struct {
	Config    *config.Config
	Bundles   agentbundle.Store
	Tools     []ai.Tool
	Notifiers []pi.Notifier
}

// ParseVersion checks saved effective settings without applying current defaults.
// Evidence expiry only gates publication; it does not expire a published version.
func ParseVersion(v agent.Version) (agentversion.Snapshot, error) {
	var protocol struct {
		DigestVersion int `json:"digest_version"`
	}
	if err := json.Unmarshal(v.Validation, &protocol); err != nil || protocol.DigestVersion != 1 {
		return agentversion.Snapshot{}, errors.New("unsupported saved digest protocol")
	}
	wire := struct {
		SchemaVersion int             `json:"schema_version"`
		Model         json.RawMessage `json:"model"`
		Tools         json.RawMessage `json:"tools"`
		Runtime       json.RawMessage `json:"runtime"`
	}{1, v.ModelConfig, v.ToolPolicy, v.RuntimeConfig}
	raw, err := json.Marshal(wire)
	if err != nil {
		return agentversion.Snapshot{}, err
	}
	snapshot, err := agentversion.ParseSnapshot(raw)
	if err != nil {
		return snapshot, err
	}
	digest, err := agentversion.SpecDigest(v.BundleDigest, snapshot)
	if err != nil {
		return snapshot, err
	}
	if digest != v.SpecDigest {
		return snapshot, errors.New("version configuration digest mismatch")
	}
	return snapshot, nil
}

func (f *PIFactory) optionsFor(r Request, workDir string) (pi.Options, error) {
	if !validRequest(r) || f.Config == nil {
		return pi.Options{}, errors.New("invalid runtime request")
	}
	s, err := ParseVersion(r.Version)
	if err != nil {
		return pi.Options{}, err
	}
	if s.Runtime.WritePolicy.WriteMode != "restricted" || !s.Tools.Builtin.Read {
		return pi.Options{}, errors.New("production runtime requires restricted readable workspace")
	}
	opts := pi.Options{WorkDir: workDir, AllowExec: s.Runtime.AllowExec, AllowWrite: false, BuiltinSubagent: s.Tools.Builtin.Subagent,
		WorkspacePolicy: pi.WorkspacePolicy{WriteMode: pi.WorkspaceWriteRestricted, WritablePrefixes: append([]string{}, s.Runtime.WritePolicy.WritablePrefixes...)},
		LoopDetection:   s.Runtime.LoopDetection, Compaction: harness.CompactionConfig{ContextWindowTokens: s.Runtime.Compaction.ContextWindowTokens, EnablePrune: s.Runtime.Compaction.EnablePrune}, Notifiers: f.Notifiers}
	// Server policy may be narrower, but never lets a saved version write behavior.
	for _, prefix := range opts.WorkspacePolicy.WritablePrefixes {
		if !safeRuntimePrefix(prefix) {
			return pi.Options{}, errors.New("invalid production write prefix")
		}
	}
	opts.Platform, err = f.Config.ResolveAgentModel(s.Model)
	if err != nil {
		return pi.Options{}, err
	}
	available := map[string]ai.Tool{}
	for _, tool := range f.Tools {
		available[tool.Definition().Name] = tool
	}
	for _, ref := range s.Tools.Registered {
		tool, ok := available[ref.Name]
		if !ok || ref.ImplementationRef != "application:"+ref.Name+":v1" || len(ref.Arguments) != 0 {
			return pi.Options{}, errors.New("application tool implementation unavailable")
		}
		opts.Tools = append(opts.Tools, tool)
	}
	for _, ref := range s.Tools.MCP {
		server, err := f.Config.ResolveAgentMCP(ref)
		if err != nil {
			return pi.Options{}, err
		}
		opts.MCPServers = append(opts.MCPServers, server)
	}
	frozen := &config.Config{Tools: config.ToolsConfig{TimeoutSeconds: s.Runtime.ToolRuntime.TimeoutSeconds, Retry: config.ToolRetryConfig{Attempts: s.Runtime.ToolRuntime.Retry.Attempts, BackoffMs: s.Runtime.ToolRuntime.Retry.BackoffMS, Tools: append([]string{}, s.Runtime.ToolRuntime.Retry.Tools...)}}}
	for _, rule := range s.Tools.Permissions {
		frozen.Permissions.Rules = append(frozen.Permissions.Rules, config.PermissionRuleConfig{Tool: rule.Tool, Patterns: append([]string{}, rule.Patterns...), Effect: rule.Effect, Reason: rule.Reason})
	}
	opts.ExtraHandlers, err = config.NewExtraToolHandlers(frozen)
	return opts, err
}

func (f *PIFactory) Create(ctx context.Context, r Request) (ManagedRuntime, error) {
	if r.Key.Kind == "training" || r.Key.Kind == "preview" {
		opts, err := f.authorOptions(r)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return pi.New(opts)
	}
	if r.Key.Kind != "chat" || f.Bundles == nil {
		return nil, errors.New("production factory accepts chat only")
	}
	// Validate settings and external refs before expensive materialization.
	if _, err := f.optionsFor(r, ""); err != nil {
		return nil, err
	}
	v := r.Version
	ref := agentbundle.BundleRef{Commit: v.BundleCommit, Tag: v.BundleTag, Digest: v.BundleDigest}
	workDir, err := f.Bundles.MaterializeChat(ctx, v.TenantID, v.AgentID, r.Key.ConversationID, v.ID, ref)
	if err != nil {
		return nil, err
	}
	opts, err := f.optionsFor(r, workDir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pi.New(opts)
}

// Authors edit only candidate assets through a rooted application tool. They
// never inherit executable tools, MCP servers or production business tools.
func (f *PIFactory) authorOptions(r Request) (pi.Options, error) {
	if !validRequest(r) || (r.Key.Kind != "training" && r.Key.Kind != "preview") || f.Config == nil || r.CandidateRoot == "" {
		return pi.Options{}, errors.New("invalid training runtime request")
	}
	snapshot, err := ParseVersion(r.Version)
	if err != nil {
		return pi.Options{}, err
	}
	platform, err := f.Config.ResolveAgentModel(snapshot.Model)
	if err != nil {
		return pi.Options{}, err
	}
	var tools []ai.Tool
	if r.Key.Kind == "training" {
		fileTool, err := trainingtools.NewFileTool(r.CandidateRoot)
		if err != nil {
			return pi.Options{}, err
		}
		tools = []ai.Tool{fileTool}
	}
	return pi.Options{WorkDir: r.CandidateRoot, Platform: platform,
		WorkspacePolicy: pi.WorkspacePolicy{WriteMode: pi.WorkspaceWriteRestricted},
		Tools:           tools, LoopDetection: snapshot.Runtime.LoopDetection,
		Compaction: harness.CompactionConfig{ContextWindowTokens: snapshot.Runtime.Compaction.ContextWindowTokens, EnablePrune: snapshot.Runtime.Compaction.EnablePrune}}, nil
}

func safeRuntimePrefix(s string) bool {
	if s == ".tmp" || s == "scratch" {
		return true
	}
	// Reuse the SDK's lexical policy validation at construction; this check keeps
	// the top-level writable namespace fixed even when the saved prefix is nested.
	for _, root := range []string{".tmp/", "scratch/"} {
		if len(s) > len(root) && s[:len(root)] == root {
			return true
		}
	}
	return false
}
