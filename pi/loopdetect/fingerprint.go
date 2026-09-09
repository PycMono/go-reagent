package loopdetect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// signature 是固定长度的 Call/Outcome 指纹；Detector 只保存 hash，不保存
// 原始参数、结果或 canonical JSON，避免成为第二份敏感数据账本。
type signature [sha256.Size]byte

// canonicalJSON 把一段 JSON 文本规范化为确定性字节：
// UseNumber 保留数字的合法十进制文本（1 与 1.0 视为不同参数，大整数不
// 发生 float64 精度折叠）；object key 由 encoding/json 按 UTF-8 字典序稳定
// 输出；array 顺序保留；字符串转义后不含原始 NUL 等控制字符。
// 输入必须是单个 JSON value，禁止尾随 token。
func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("loopdetect: arguments must contain exactly one JSON value")
	}
	return json.Marshal(value)
}

// callSignature 计算 SHA256(toolName + NUL + canonicalJSON(arguments))。
// 工具名来自注册表、不含 NUL；canonical JSON 输出不含原始 NUL，因此分隔
// 符无歧义。ToolCall.ID 被忽略。
//
// 调用前 Tool Calls 已通过 ai.ToolCalls.Validate()（参数为合法 JSON），
// 非法 JSON 属于 Loop 的 Action 契约错误；这里出错时降级为原始字节参与
// hash（仍确定性），不修改检测语义。
func callSignature(call ai.ToolCall) signature {
	canonical, err := canonicalJSON(call.Arguments)
	if err != nil {
		canonical = call.Arguments
	}
	sum := sha256.New()
	sum.Write([]byte(call.Name))
	sum.Write([]byte{0})
	sum.Write(canonical)
	var sig signature
	copy(sig[:], sum.Sum(nil))
	return sig
}

// outcomeSignature 计算一次工具结果的指纹：
//
//	SHA256(canonicalJSON([callSignatureHex, IsError, ErrorCode,
//	    [[Content.Type, Content.Text], ...]]))
//
// 结构化 canonical JSON 编码使字符串转义、数组长度与顺序全部显式，
// [Text("a"), Text("b")] 与 [Text("a\x00b")] 不会碰撞；禁止用分隔符直接
// 拼接任意文本。Details、Usage、duration、PID、timestamp、ToolCallID 等
// 易变元数据不纳入。
func outcomeSignature(callSig signature, event toolexec.Event) signature {
	content := make([][]string, 0, len(event.Content))
	for _, block := range event.Content {
		content = append(content, []string{string(block.Type), block.Text})
	}
	canonical, err := json.Marshal([]any{
		hex.EncodeToString(callSig[:]),
		event.IsError,
		string(event.ErrorCode),
		content,
	})
	if err != nil {
		// []any 中只含 string/bool/数组，Marshal 不会失败；防御性兜底。
		canonical = nil
	}
	return sha256.Sum256(canonical)
}
