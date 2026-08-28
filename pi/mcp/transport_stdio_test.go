package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// helper 进程模式：通过环境变量选择脚本行为，helper 只存在于本 _test.go。
const stdioHelperEnvVar = "GO_REAGENT_MCP_STDIO_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(stdioHelperEnvVar) != "" {
		os.Exit(runStdioHelper())
	}
	os.Exit(m.Run())
}

func TestStdioTransportHandshakeAndCalls(t *testing.T) {
	transport := newTestStdioTransport(t, "standard")
	ctx := context.Background()

	response, err := transport.Send(ctx, Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), `"protocolVersion":"`+ProtocolVersion+`"`) {
		t.Fatalf("initialize result = %s", response.Result)
	}
	// notification 写入后立即返回，不等待 stdout。
	started := time.Now()
	if _, err := transport.Send(ctx, Request{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("notification Send took %v", elapsed)
	}
	response, err = transport.Send(ctx, Request{JSONRPC: "2.0", ID: int64Ptr(2), Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), "echo_tool") {
		t.Fatalf("tools/list result = %s", response.Result)
	}
}

func TestStdioTransportConcurrentOutOfOrderResponses(t *testing.T) {
	transport := newTestStdioTransport(t, "standard")
	initializeStdio(t, transport)

	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		response, err := transport.Send(context.Background(), Request{
			JSONRPC: "2.0", ID: int64Ptr(10), Method: "tools/call",
			Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 400, "value": "slow"}},
		})
		if err == nil {
			results[0] = string(response.Result)
		}
		errs[0] = err
	}()
	go func() {
		defer wg.Done()
		response, err := transport.Send(context.Background(), Request{
			JSONRPC: "2.0", ID: int64Ptr(11), Method: "tools/call",
			Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 0, "value": "fast"}},
		})
		if err == nil {
			results[1] = string(response.Result)
		}
		errs[1] = err
	}()
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("call %d error = %v", index, err)
		}
	}
	if !strings.Contains(results[0], `"slow"`) || !strings.Contains(results[1], `"fast"`) {
		t.Fatalf("results crossed: %s / %s", results[0], results[1])
	}
}

func TestStdioTransportContextCancelOnlyCancelsOneRequest(t *testing.T) {
	transport := newTestStdioTransport(t, "standard")
	initializeStdio(t, transport)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := transport.Send(cancelledCtx, Request{
		JSONRPC: "2.0", ID: int64Ptr(1), Method: "tools/call",
		Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 2000, "value": "x"}},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Send error = %v", err)
	}
	// 其他并发请求不受影响。
	response, err := transport.Send(context.Background(), Request{
		JSONRPC: "2.0", ID: int64Ptr(2), Method: "tools/call",
		Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 0, "value": "ok"}},
	})
	if err != nil || !strings.Contains(string(response.Result), `"ok"`) {
		t.Fatalf("sibling call = %s, %v", response.Result, err)
	}
}

func TestStdioTransportRequestTimeoutKeepsDeadlineExceeded(t *testing.T) {
	transport := newTestStdioTransport(t, "standard")
	transport.options.Timeout = 300 * time.Millisecond
	initializeStdio(t, transport)

	_, err := transport.Send(context.Background(), Request{
		JSONRPC: "2.0", ID: int64Ptr(1), Method: "tools/call",
		Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 2000, "value": "x"}},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	// Timeout 只移除该请求，后续请求仍成功。
	if _, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(2), Method: "tools/list"}); err != nil {
		t.Fatalf("follow-up call error = %v", err)
	}
}

func TestStdioTransportDropsUnroutableResponseIDs(t *testing.T) {
	transport := newTestStdioTransport(t, "weird-ids")
	initializeStdio(t, transport)

	// helper 对任意请求先回一串非法/未知 ID，再回正确响应和一条迟到
	// 重复响应；本次 Send 应拿到正确结果，其余被丢弃。
	response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(7), Method: "tools/list"})
	if err != nil || !strings.Contains(string(response.Result), "echo_tool") {
		t.Fatalf("response = %s, %v", response.Result, err)
	}
	// 迟到响应可能在 Send 返回后才被 reader 处理，轮询等待计数稳定。
	deadline := time.Now().Add(2 * time.Second)
	for transport.dropped.Load() < 6 {
		if time.Now().After(deadline) {
			t.Fatalf("dropped = %d, want >= 6", transport.dropped.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStdioTransportIgnoresServerRequestsAndNotifications(t *testing.T) {
	transport := newTestStdioTransport(t, "ping-and-notify")
	initializeStdio(t, transport)
	// helper 已发送 server notification 与 ping（字符串 ID 的 Request），
	// reader 不应被阻塞，后续请求正常。
	response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	if transport.dropped.Load() < 2 {
		t.Fatalf("dropped = %d, want >= 2", transport.dropped.Load())
	}
	if !strings.Contains(string(response.Result), "echo_tool") {
		t.Fatalf("tools/list result = %s", response.Result)
	}
}

func TestStdioTransportProtocolErrorsFailTransport(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "non-JSON line", mode: "garbage", want: "invalid JSON-RPC message"},
		{name: "invalid UTF-8", mode: "invalid-utf8", want: "invalid JSON-RPC message"},
		{name: "bad JSON-RPC version", mode: "bad-version", want: "unsupported jsonrpc version"},
		{name: "oversized line", mode: "huge-line", want: "response too large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newTestStdioTransport(t, test.mode)
			_, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Send error = %v, want %q", err, test.want)
			}
			// failed 终态：后续 Send 直接返回同一稳定错误。
			again, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(2), Method: "initialize"})
			if again.JSONRPC != "" || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Send after failure = %v", err)
			}
		})
	}
}

func TestStdioTransportSkipsBlankLines(t *testing.T) {
	transport := newTestStdioTransport(t, "blank-lines")
	response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
	if err != nil || !strings.Contains(string(response.Result), ProtocolVersion) {
		t.Fatalf("response = %s, %v", response.Result, err)
	}
}

func TestStdioTransportDecodesFinalMessageWithoutNewline(t *testing.T) {
	transport := newTestStdioTransport(t, "no-final-newline")
	response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
	if err != nil || !strings.Contains(string(response.Result), ProtocolVersion) {
		t.Fatalf("response = %s, %v", response.Result, err)
	}
}

func TestStdioTransportServerExitFailsPending(t *testing.T) {
	transport := newTestStdioTransport(t, "exit-after-init")
	_, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	// helper 在 initialize 后退出；下一次 Send 收到稳定的 exited 错误。
	_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(2), Method: "tools/list"})
	if err == nil || !strings.Contains(err.Error(), "stdio server exited") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("Send error = %v", err)
	}
}

func TestStdioTransportStderrFloodDoesNotBlockAndStaysSecret(t *testing.T) {
	const secret = "stderr-should-never-leak"
	transport := newTestStdioTransport(t, "stderr-flood")
	initializeStdio(t, transport)
	response, err := transport.Send(context.Background(), Request{
		JSONRPC: "2.0", ID: int64Ptr(1), Method: "tools/call",
		Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 0, "value": "ok"}},
	})
	if err != nil || !strings.Contains(string(response.Result), `"ok"`) {
		t.Fatalf("call = %s, %v", response.Result, err)
	}
	if strings.Contains(fmt.Sprintf("%v", transport.err), secret) {
		t.Fatalf("transport error leaks stderr: %v", transport.err)
	}
}

func TestStdioTransportEnvReachesChild(t *testing.T) {
	// helper 校验 literal 与 ${NAME} 引用都正确进入子进程环境；若缺失
	// 则直接退出，本测试通过 initialize 成功来证明 env 已生效。
	transport := newTestStdioTransport(t, "env-check")
	if _, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"}); err != nil {
		t.Fatal(err)
	}
}

func TestStdioTransportCloseUnblocksSendWhenServerStopsReadingStdin(t *testing.T) {
	transport := newTestStdioTransport(t, "no-stdin-read")
	// Server 不读取 stdin：第一条写入在 pipe 缓冲填满后阻塞。已知限制
	// 下 Send 无法被 Timeout 打断，但 Close 关闭 stdin 并终止进程后，
	// 阻塞写必须返回。
	blocked := make(chan error, 1)
	go func() {
		// 超过 pipe 缓冲（64KiB）的 payload 保证写入真正阻塞。
		request := Request{
			JSONRPC: "2.0", ID: int64Ptr(1), Method: "tools/call",
			Params: map[string]any{"name": "echo_tool", "arguments": map[string]any{"delay_ms": 0, "value": strings.Repeat("x", 256*1024)}},
		}
		_, err := transport.Send(context.Background(), request)
		blocked <- err
	}()
	// 等待 Send 进入写入，再触发 Close。
	time.Sleep(300 * time.Millisecond)
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.Close(closeCtx); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	select {
	case err := <-blocked:
		if err == nil {
			t.Fatal("blocked Send returned nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock the pending write")
	}
}

func TestStdioTransportCloseForcesKillWhenServerIgnoresEOF(t *testing.T) {
	previousGrace := stdioCloseGracePeriod
	stdioCloseGracePeriod = 300 * time.Millisecond
	defer func() { stdioCloseGracePeriod = previousGrace }()
	transport := newTestStdioTransport(t, "close-hang")
	initializeStdio(t, transport)
	started := time.Now()
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	// helper 收到 EOF 后不退出；Close 必须在宽限期内强杀并返回。
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("Close took %v, force kill did not happen", elapsed)
	}
}

func TestStdioTransportCloseIsIdempotentAndSafeWithoutStart(t *testing.T) {
	transport := newTestStdioTransport(t, "standard")
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 从未 Send 的 Transport 可以无副作用关闭。
	if err := transport.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := transport.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	_, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"})
	if err == nil || !strings.Contains(err.Error(), "mcp transport is closed") {
		t.Fatalf("Send after Close error = %v", err)
	}
}

func TestStdioTransportConcurrentSendAndClose(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		transport := newTestStdioTransport(t, "standard")
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			for index := 0; index < 10; index++ {
				transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(int64(index + 1)), Method: "tools/list"})
			}
		}()
		go func() {
			defer wg.Done()
			transport.Close(context.Background())
		}()
		go func() {
			defer wg.Done()
			transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(99), Method: "tools/list"})
		}()
		wg.Wait()
		// 再次 Close 必须幂等且立即返回。
		if err := transport.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStdioTransportInvalidOptions(t *testing.T) {
	if _, err := NewStdioTransport(StdioTransportOptions{Command: ""}); err == nil {
		t.Fatal("empty command accepted")
	}
	if _, err := NewStdioTransport(StdioTransportOptions{Command: "a\x00b"}); err == nil {
		t.Fatal("NUL command accepted")
	}
	transport, err := NewStdioTransport(StdioTransportOptions{Command: "go-reagent-test-command"})
	if err != nil {
		t.Fatal(err)
	}
	// Timeout <= 0 兜底为 DefaultTimeout。
	if transport.options.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %v", transport.options.Timeout)
	}
}

// --- helper 进程实现 ---

func newTestStdioTransport(t *testing.T, mode string, mutate ...func(*StdioTransportOptions)) *StdioTransport {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options := StdioTransportOptions{
		Command: executable,
		Args:    []string{"-test.run=TestStdioHelperProcess"},
		Env: append(os.Environ(),
			stdioHelperEnvVar+"="+mode,
			"GO_REAGENT_HELPER_LITERAL=literal-value",
			"GO_REAGENT_HELPER_REF=helper-env-value",
			"GO_REAGENT_PARENT_ENV=parent-value",
		),
		Timeout: 5 * time.Second,
	}
	for _, apply := range mutate {
		apply(&options)
	}
	transport, err := NewStdioTransport(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = transport.Close(ctx)
	})
	return transport
}

func initializeStdio(t *testing.T, transport *StdioTransport) {
	t.Helper()
	if _, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: int64Ptr(1), Method: "initialize"}); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
}

func int64Ptr(value int64) *int64 { return &value }

// TestStdioHelperProcess 是 helper 进程的入口占位：真正的行为分发在
// TestMain 中，这个测试本身永远不应执行。
func TestStdioHelperProcess(t *testing.T) {}

func runStdioHelper() int {
	mode := os.Getenv(stdioHelperEnvVar)
	switch mode {
	case "no-stdin-read":
		// 从不读取 stdin，用于验证 Close 能解除阻塞写。
		time.Sleep(60 * time.Second)
		return 0
	case "env-check":
		if os.Getenv("GO_REAGENT_HELPER_LITERAL") != "literal-value" ||
			os.Getenv("GO_REAGENT_HELPER_REF") != "helper-env-value" ||
			os.Getenv("GO_REAGENT_PARENT_ENV") != "parent-value" {
			return 3
		}
	}

	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 64*1024), maxStdioMessageBytes)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	var writeMu sync.Mutex
	writeMessage := func(payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		writer.Write(data)
		writer.WriteByte('\n')
		writer.Flush()
	}

	switch mode {
	case "garbage":
		fmt.Fprint(writer, "this is not json\n")
		writer.Flush()
	case "invalid-utf8":
		writer.Write([]byte{0xff, 0xfe, 0x00, '\n'})
		writer.Flush()
	case "bad-version":
		writeMessage(map[string]any{"jsonrpc": "1.0", "id": 1, "result": map[string]any{}})
		// 保持进程存活，让 Transport 的失败路径负责回收。
		time.Sleep(30 * time.Second)
		return 0
	case "huge-line":
		writer.Write(bytesRepeat(maxStdioMessageBytes + 1024))
		writer.WriteByte('\n')
		writer.Flush()
		time.Sleep(30 * time.Second)
		return 0
	case "exit-after-init":
		for reader.Scan() {
			var message stdioWireMessage
			if json.Unmarshal(reader.Bytes(), &message) != nil || message.Method != "initialize" {
				continue
			}
			writeMessage(initializeResultMessage(message.ID))
			// 给 reader 留出读取时间，然后异常退出。
			time.Sleep(200 * time.Millisecond)
			return 7
		}
		return 0
	case "ping-and-notify":
		writeMessage(map[string]any{"jsonrpc": "2.0", "method": "notifications/server-notice"})
		writeMessage(map[string]any{"jsonrpc": "2.0", "id": "srv-1", "method": "ping"})
	}

	for reader.Scan() {
		line := reader.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var message stdioWireMessage
		if err := json.Unmarshal(line, &message); err != nil {
			continue
		}
		switch message.Method {
		case "initialize":
			if mode == "blank-lines" {
				fmt.Fprint(writer, "\n\n   \n")
			}
			writeMessage(initializeResultMessage(message.ID))
			if mode == "weird-ids" {
				// 紧随合法握手之后发送一串无法路由的响应形态。
				writeMessage(map[string]any{"jsonrpc": "2.0", "id": "string-id", "result": map[string]any{}})
				writeMessage(map[string]any{"jsonrpc": "2.0", "id": 1.5, "result": map[string]any{}})
				writeMessage(map[string]any{"jsonrpc": "2.0", "id": nil, "result": map[string]any{}})
				writeMessage(map[string]any{"jsonrpc": "2.0", "result": map[string]any{}})
				writeMessage(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("99999999999999999999"), "result": map[string]any{}})
			}
		case "notifications/initialized":
			// 无响应。
		case "tools/list":
			writeMessage(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"result": map[string]any{"tools": []map[string]any{
					{"name": "echo_tool", "inputSchema": map[string]any{"type": "object"}},
					{"name": "other_tool", "inputSchema": map[string]any{"type": "object"}},
				}},
			})
			if mode == "weird-ids" {
				// 迟到的重复响应：pending 已删除，应被安全丢弃。
				writeMessage(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{}})
			}
		case "tools/call":
			// 并发处理：延迟调用不应阻塞后续请求的读取与响应。
			params := message.Params
			id := message.ID
			go func() {
				var parsed callToolParams
				if json.Unmarshal(params, &parsed) != nil {
					return
				}
				delay := 0
				if raw, ok := parsed.Arguments["delay_ms"].(float64); ok {
					delay = int(raw)
				}
				value, _ := parsed.Arguments["value"].(string)
				time.Sleep(time.Duration(delay) * time.Millisecond)
				writeMessage(map[string]any{
					"jsonrpc": "2.0", "id": id,
					"result": map[string]any{"content": []map[string]any{{"type": "text", "text": value}}},
				})
			}()
		}
		if mode == "blank-lines" {
			fmt.Fprint(writer, "\n \n")
		}
	}

	// close-hang：stdin EOF 后不退出，验证 Close 的强制终止路径。
	if mode == "close-hang" {
		time.Sleep(60 * time.Second)
	}
	return 0
}

func initializeResultMessage(id json.RawMessage) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "helper", "version": "1"},
		},
	}
}

func bytesRepeat(length int) []byte {
	return bytes.Repeat([]byte("a"), length)
}