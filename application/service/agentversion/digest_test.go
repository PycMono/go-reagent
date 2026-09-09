package agentversion

import (
	"slices"
	"testing"
)

func TestCanonicalJSONRFC8785Vectors(t *testing.T) {
	input := []byte(`{"numbers":[333333333.33333329,1E30,4.50,2e-3,0.000000000000000000000000001],"string":"€$\u000f\nA'B\"\\\"/"}`)
	want := `{"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\"/"}`
	got, err := canonicalizeJSON(input)
	if err != nil || string(got) != want {
		t.Fatalf("canonicalizeJSON() = %s, %v; want %s", got, err, want)
	}
	unicodeInput := []byte(`{"\u20ac":"Euro Sign","\r":"Carriage Return","\ufb33":"Hebrew Letter Dalet With Dagesh","1":"One","😀":"Emoji: Grinning Face","\u00f6":"Latin Small Letter O With Diaeresis"}`)
	unicodeWant := `{"\r":"Carriage Return","1":"One","ö":"Latin Small Letter O With Diaeresis","€":"Euro Sign","😀":"Emoji: Grinning Face","דּ":"Hebrew Letter Dalet With Dagesh"}`
	got, err = canonicalizeJSON(unicodeInput)
	if err != nil || string(got) != unicodeWant {
		t.Fatalf("unicode canonicalization = %s, %v", got, err)
	}
}

func TestBundleDigestCoversBytesModeAndUTF8PathOrder(t *testing.T) {
	entries := []Entry{
		{Path: "é.txt", Mode: "100644", Size: 2, ContentSHA256: "sha256:73cb3858a687a8494ca332305b60e4f015397267c5c88ff1cda7f0a9d4d1b34b"},
		{Path: "script.sh", Mode: "100755", Size: 18, ContentSHA256: "sha256:299001868fb8c02fdc6f05bb78f3f3127b1fc1b01c2d30fc134d5f16048c8f8f"},
		{Path: "link", Mode: "120000", Size: 9, ContentSHA256: "sha256:2c61e1128f259387f318a559c3fb5c90317b4a518a95a313bdc3b35e637e7cf4"},
	}
	original := slices.Clone(entries)
	digest, err := BundleDigest(entries)
	if err != nil || digest == "" {
		t.Fatalf("BundleDigest() = %q, %v", digest, err)
	}
	if !slices.Equal(entries, original) {
		t.Fatal("BundleDigest mutated input order")
	}
	changed := slices.Clone(entries)
	changed[1].Mode = "100644"
	other, _ := BundleDigest(changed)
	if digest == other {
		t.Fatal("executable mode did not affect digest")
	}
	for _, invalid := range [][]Entry{{{Path: "../escape", Mode: "100644", Size: 0, ContentSHA256: "sha256:" + string(make([]byte, 64))}}, {{Path: "a", Mode: "100644", Size: -1, ContentSHA256: "bad"}}} {
		if _, err := BundleDigest(invalid); err == nil {
			t.Fatal("invalid entry accepted")
		}
	}
}

func TestSpecDigestIsStableAndArrayOrderMatters(t *testing.T) {
	snapshot, err := ParseSnapshot([]byte(validSnapshotJSON))
	if err != nil {
		t.Fatal(err)
	}
	first, err := SpecDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", snapshot)
	if err != nil || first == "" {
		t.Fatalf("SpecDigest() = %q, %v", first, err)
	}
	snapshot.Model.Capabilities = []string{"vision", "text"}
	second, err := SpecDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", snapshot)
	if err != nil || first == second {
		t.Fatal("array order did not affect spec digest")
	}
}
