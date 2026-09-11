package agentruntime

import (
	"testing"
)

func TestPreviewCannotModifyCandidate(t *testing.T) {
	cfg, v := versionFixture(t)
	f := &PIFactory{Config: cfg}
	r := Request{Key: Key{Kind: "preview", TenantID: "t", AgentID: "a", ConversationID: "c", VersionID: "v", OperationID: "op", SpecDigest: v.SpecDigest}, Version: v, CandidateRoot: t.TempDir()}
	opts, err := f.authorOptions(r)
	if err != nil {
		t.Fatal(err)
	}
	if opts.AllowExec || opts.AllowWrite || opts.BuiltinSubagent || len(opts.Tools) != 0 || len(opts.MCPServers) != 0 {
		t.Fatal("preview can change assets")
	}
}
func TestAuthorCannotInheritProductionProcessesOrTools(t *testing.T) {
	cfg, v := versionFixture(t)
	f := &PIFactory{Config: cfg}
	r := Request{Key: Key{Kind: "training", TenantID: "t", AgentID: "a", ConversationID: "c", VersionID: "v", OperationID: "op", SpecDigest: v.SpecDigest}, Version: v, CandidateRoot: t.TempDir()}
	opts, err := f.authorOptions(r)
	if err != nil {
		t.Fatal(err)
	}
	if opts.AllowExec || opts.AllowWrite || opts.BuiltinSubagent || len(opts.MCPServers) != 0 || len(opts.ExtraHandlers) != 0 || len(opts.Tools) != 1 || opts.Tools[0].Definition().Name != "training_file" {
		t.Fatal("author inherited production capabilities")
	}
}
