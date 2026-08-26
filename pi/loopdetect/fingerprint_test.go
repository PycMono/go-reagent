package loopdetect

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

func makeCall(id, name, args string) ai.ToolCall {
	return ai.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func makeResult(call ai.ToolCall, text string) toolexec.Result {
	return toolexec.Result{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock(text)},
	}
}

// 1. JSON object key 顺序和无意义空白不同，Call signature 相同。
func TestCallSignatureKeyOrderAndWhitespace(t *testing.T) {
	a := callSignature(makeCall("1", "read_file", `{"path":"/tmp/x","offset":1}`))
	b := callSignature(makeCall("2", "read_file", "{\n  \"offset\": 1, \"path\": \"/tmp/x\"\n}"))
	if a != b {
		t.Fatal("key order/whitespace must not change call signature")
	}
}

// 2. ToolCallID 不同，Call signature 相同。
func TestCallSignatureIgnoresID(t *testing.T) {
	a := callSignature(makeCall("call-1", "read_file", `{"path":"/tmp/x"}`))
	b := callSignature(makeCall("call-2", "read_file", `{"path":"/tmp/x"}`))
	if a != b {
		t.Fatal("ToolCallID must not affect call signature")
	}
}

// 3. tool name、array 顺序、string、boolean、null 或 number 文本不同，Call
// signature 不同。
func TestCallSignatureSensitivity(t *testing.T) {
	base := makeCall("1", "tool", `{"a":[1,2],"s":"x","b":true,"n":null,"num":1}`)
	variants := []ai.ToolCall{
		makeCall("1", "other", `{"a":[1,2],"s":"x","b":true,"n":null,"num":1}`),  // tool name
		makeCall("1", "tool", `{"a":[2,1],"s":"x","b":true,"n":null,"num":1}`),   // array order
		makeCall("1", "tool", `{"a":[1,2],"s":"y","b":true,"n":null,"num":1}`),   // string
		makeCall("1", "tool", `{"a":[1,2],"s":"x","b":false,"n":null,"num":1}`),  // boolean
		makeCall("1", "tool", `{"a":[1,2],"s":"x","b":true,"n":0,"num":1}`),      // null vs 0
		makeCall("1", "tool", `{"a":[1,2],"s":"x","b":true,"n":null,"num":1.0}`), // number text
		makeCall("1", "tool", `{"a":[1,2],"s":"x","b":true,"n":null,"num":2}`),   // number value
	}
	baseSig := callSignature(base)
	for i, variant := range variants {
		if callSignature(variant) == baseSig {
			t.Fatalf("variant %d must change call signature", i)
		}
	}
}

// 4. 超过 JavaScript 安全整数范围的大整数不发生 float64 精度折叠。
func TestCallSignatureBigIntegerPrecision(t *testing.T) {
	a := callSignature(makeCall("1", "tool", `{"id":9007199254740993}`))
	b := callSignature(makeCall("1", "tool", `{"id":9007199254740992}`))
	if a == b {
		t.Fatal("big integers must not collapse through float64")
	}
}

// 5. Outcome 的 IsError、ErrorCode、Content 顺序/Type/Text 任一变化都会改变
// 签名。
func TestOutcomeSignatureSensitivity(t *testing.T) {
	call := makeCall("1", "tool", `{"a":1}`)
	sig := callSignature(call)
	base := makeResult(call, "ok")

	changed := []toolexec.Result{
		{ToolCallID: "1", ToolName: "tool", Content: []ai.ContentBlock{ai.TextBlock("ok")}, IsError: true},
		{ToolCallID: "1", ToolName: "tool", Content: []ai.ContentBlock{ai.TextBlock("ok")}, ErrorCode: "tool_timeout"},
		{ToolCallID: "1", ToolName: "tool", Content: []ai.ContentBlock{ai.TextBlock("different")}},
		{ToolCallID: "1", ToolName: "tool", Content: []ai.ContentBlock{ai.TextBlock("a"), ai.TextBlock("b")}},
		{ToolCallID: "1", ToolName: "tool", Content: []ai.ContentBlock{{Type: "image", Text: "ok"}}},
	}
	baseOutcome := outcomeSignature(sig, base)
	for i, result := range changed {
		if outcomeSignature(sig, result) == baseOutcome {
			t.Fatalf("variant %d must change outcome signature", i)
		}
	}
}

// 6. Details、ToolCallID 等易变元数据变化不改变 Outcome signature。
func TestOutcomeSignatureIgnoresVolatileMetadata(t *testing.T) {
	call := makeCall("1", "tool", `{"a":1}`)
	sig := callSignature(call)
	a := makeResult(call, "ok")
	b := makeResult(call, "ok")
	b.ToolCallID = "another-id"
	b.Details = map[string]any{"pid": 12345, "duration_ms": 42, "ts": "2026-08-25T00:00:00Z"}
	if outcomeSignature(sig, a) != outcomeSignature(sig, b) {
		t.Fatal("volatile metadata must not change outcome signature")
	}
}

// 7. [Text("a"), Text("b")] 与 [Text("a\x00b")] 的 Outcome signature 不同；
// 含 NUL、空 block、多 block 的编码无歧义。
func TestOutcomeSignatureNoDelimiterCollision(t *testing.T) {
	call := makeCall("1", "tool", `{"a":1}`)
	sig := callSignature(call)
	two := toolexec.Result{ToolCallID: "1", ToolName: "tool",
		Content: []ai.ContentBlock{ai.TextBlock("a"), ai.TextBlock("b")}}
	one := toolexec.Result{ToolCallID: "1", ToolName: "tool",
		Content: []ai.ContentBlock{ai.TextBlock("a\x00b")}}
	if outcomeSignature(sig, two) == outcomeSignature(sig, one) {
		t.Fatal("encoding must be unambiguous against NUL injection")
	}
	empty := toolexec.Result{ToolCallID: "1", ToolName: "tool", Content: nil}
	emptyText := toolexec.Result{ToolCallID: "1", ToolName: "tool",
		Content: []ai.ContentBlock{ai.TextBlock("")}}
	if outcomeSignature(sig, empty) == outcomeSignature(sig, emptyText) {
		t.Fatal("nil content and one empty block must differ")
	}
}

// 8. Content.Text 中时间戳变化会改变 Outcome signature——固定这一有意漏报
// 边界：输出含易变文本的工具循环只警告，由预算兜底。
func TestOutcomeSignatureVolatileTextChanges(t *testing.T) {
	call := makeCall("1", "tool", `{"a":1}`)
	sig := callSignature(call)
	a := makeResult(call, "done at 2026-08-25T00:00:00Z")
	b := makeResult(call, "done at 2026-08-25T00:00:01Z")
	if outcomeSignature(sig, a) == outcomeSignature(sig, b) {
		t.Fatal("volatile text must change outcome signature (documented miss boundary)")
	}
}
