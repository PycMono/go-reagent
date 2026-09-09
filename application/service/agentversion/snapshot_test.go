package agentversion

import (
	"encoding/json"
	"strings"
	"testing"
)

const validSnapshotJSON = `{
  "schema_version":1,
  "model":{"provider_ref":"platform:primary","protocol":"openai","base_url":"https://api.example.test","model_id":"gpt-test","reasoning":"medium","capabilities":["text","vision"],"secret_ref":"platform:primary"},
  "tools":{"builtin":{"read":true,"write":false,"exec":true,"subagent":true},"registered":[{"name":"clock","implementation_ref":"builtin:current_time","arguments":{"timezone":"UTC"}}],"mcp":[{"name":"search","implementation_ref":false,"config_ref":"mcp:search","allow_tools":["query"],"tool_prefix":"search"}]},
  "runtime":{"limits":{"max_turns":20,"max_cost_usd":1,"max_total_tokens":2000000},"loop_detection":{"disabled":false,"excluded_tools":[]},"compaction":{"context_window_tokens":128000,"enable_prune":true},"history_message_limit":100,"write_policy":{"write_mode":"restricted","writable_prefixes":[".tmp","scratch"]},"allow_exec":true,"allow_write":false}
}`

func TestParseSnapshotStrictSchema(t *testing.T) {
	got, err := ParseSnapshot([]byte(validSnapshotJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Model.SecretRef != "platform:primary" || !got.Tools.Builtin.Subagent || !got.Runtime.AllowExec || got.Runtime.AllowWrite {
		t.Fatalf("snapshot = %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil || strings.Contains(string(encoded), "api_key") {
		t.Fatalf("encoded snapshot leaks credential or failed: %s %v", encoded, err)
	}
}

func TestParseSnapshotRejectsMalformedShapes(t *testing.T) {
	cases := map[string]string{
		"unknown": strings.Replace(validSnapshotJSON, `"schema_version":1`, `"schema_version":1,"extra":true`, 1),
		"duplicate": strings.Replace(validSnapshotJSON, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		"null": strings.Replace(validSnapshotJSON, `"model":{`, `"model":null,"discard":{`, 1),
		"missing": strings.Replace(validSnapshotJSON, `"secret_ref":"platform:primary"`, `"omitted":"platform:primary"`, 1),
		"secret body": strings.Replace(validSnapshotJSON, `"secret_ref":"platform:primary"`, `"secret_ref":"platform:primary","api_key":"secret"`, 1),
		"stdio cwd": strings.Replace(validSnapshotJSON, `"config_ref":"mcp:search"`, `"config_ref":"mcp:search","cwd":"/host"`, 1),
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSnapshot([]byte(document)); err == nil {
				t.Fatal("ParseSnapshot accepted invalid document")
			}
		})
	}
}
