package agentversion

import (
	"encoding/json"
	"slices"
	"strings"
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
	ordinary, err := json.Marshal(map[string]string{"😀": "emoji", "דּ": "ligature"})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalizeJSON(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(ordinary) == string(canonical) {
		t.Fatalf("ordinary JSON unexpectedly matched JCS order: %s", ordinary)
	}
}

func TestBundleDigestCoversBytesModeAndUTF8PathOrder(t *testing.T) {
	entries := []Entry{
		{Path: "é.txt", Mode: "100644", Size: 2, ContentSHA256: "73cb3858a687a8494ca332305b60e4f015397267c5c88ff1cda7f0a9d4d1b34b"},
		{Path: "script.sh", Mode: "100755", Size: 18, ContentSHA256: "299001868fb8c02fdc6f05bb78f3f3127b1fc1b01c2d30fc134d5f16048c8f8f"},
		{Path: "link", Mode: "120000", Size: 9, ContentSHA256: "2c61e1128f259387f318a559c3fb5c90317b4a518a95a313bdc3b35e637e7cf4"},
	}
	original := slices.Clone(entries)
	digest, err := BundleDigest(entries)
	if err != nil || digest == "" {
		t.Fatalf("BundleDigest() = %q, %v", digest, err)
	}
	if digest != "sha256:e159b3c1fa68d9942d8aff1778399c9cce45aad72e44ce6e2a15c85f424b6425" {
		t.Fatalf("BundleDigest() = %q", digest)
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

func TestSpecDigestUsesExactProtocolFieldNames(t *testing.T) {
	snapshot, err := ParseSnapshot([]byte(validSnapshotJSON))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := specDigestCanonical("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"bundle_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","digest_version":1,"model_config":{"base_url":"https://api.example.test","capabilities":["text","vision"],"context_window_tokens":128000,"model_id":"gpt-test","pricing":{"cache_read_usd_per_million_tokens":0.1,"cache_write_usd_per_million_tokens":0.2,"input_usd_per_million_tokens":1,"output_usd_per_million_tokens":2},"protocol":"openai","provider_ref":"platform:primary","reasoning":"","secret_ref":"platform:primary","vision":true},"runtime_config":{"allow_exec":true,"allow_write":false,"compaction":{"context_window_tokens":128000,"enable_prune":true},"history_message_limit":100,"limits":{"max_cost_usd":1,"max_total_tokens":2000000,"max_turns":20},"loop_detection":{"disabled":false,"excluded_tools":[]},"tool_runtime":{"retry":{"attempts":1,"backoff_ms":0,"tools":[]},"timeout_seconds":30},"write_policy":{"writable_prefixes":[".tmp","scratch"],"write_mode":"restricted"}},"tool_policy":{"builtin":{"exec":true,"read":true,"subagent":true,"write":false},"mcp":[{"allow_tools":["query"],"config_ref":"mcp:search","implementation_ref":"mcp:http:v1","name":"search","tool_prefix":"search"}],"permissions":[{"effect":"deny","patterns":["rm"],"reason":"protected","tool":"exec"}],"registered":[{"arguments":{"timezone":"UTC"},"implementation_ref":"builtin:current_time","name":"clock"}]}}`
	if string(canonical) != expected {
		t.Fatalf("canonical=%s", canonical)
	}
	for _, field := range []string{`"model_config":`, `"tool_policy":`, `"runtime_config":`} {
		if !strings.Contains(string(canonical), field) {
			t.Fatalf("canonical payload missing %s: %s", field, canonical)
		}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &top); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{"model", "tools", "runtime"} {
		if _, exists := top[wrong]; exists {
			t.Fatalf("canonical payload contains legacy top-level field %s", wrong)
		}
	}
	if got := digestBytes(canonical); got != "sha256:1f7129dbf7fc286b5aec648c210c14cf39c323948ea12a2941206fdb6b587dba" {
		t.Fatalf("canonical=%s\ndigest=%s", canonical, got)
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
