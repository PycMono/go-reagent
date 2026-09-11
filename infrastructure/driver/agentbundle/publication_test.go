package agentbundle

import (
	"context"
	port "github.com/PycMono/go-reagent/application/port/agentbundle"
	"testing"
)

func TestPublicationIntentRoundtripAndClone(t *testing.T) {
	s := mustStore(t)
	ctx := context.Background()
	base, err := s.CreateInitial(ctx, "t", "a", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.CloneVersion(ctx, "t", "a", "v2", base)
	if err != nil || ref.Commit != base.Commit || ref.Digest != base.Digest || ref.Tag == base.Tag {
		t.Fatalf("clone: %+v %v", ref, err)
	}
	intent := port.PublicationIntent{TenantID: "t", AgentID: "a", VersionID: "v2", BaseVersionID: "v1", Tag: ref.Tag, BundleDigest: ref.Digest, SpecDigest: ref.Digest, Phase: "reserved", Version: 2}
	if err = s.Prepare(ctx, intent); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx, "t", "a")
	if err != nil || len(items) != 1 || items[0] != intent {
		t.Fatalf("%+v %v", items, err)
	}
	if err = s.Remove(ctx, intent); err != nil {
		t.Fatal(err)
	}
}
