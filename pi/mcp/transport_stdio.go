package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// stdioCloseGracePeriod 是正常关闭时等待子进程自然退出的最长时间；
// 超时后强制终止完整进程组。变量以便测试缩短等待。
var stdioCloseGracePeriod = 5 * time.Second

// StdioTransportOptions 是 stdio Transport 的启动选项。NewStdioTransport
// 只校验并保存选项，不立即启动进程；第一次 Send（必然是 initialize）
// 时同步完成一次性启动。
type StdioTransportOptions struct {
	Command string
	Args    []string
	Env     []string
	WorkDir string
	Timeout time.Duration

	// BuildCommand 与旧字段严格互斥（设计 §7）：非 nil 时 Command/Args/Env/WorkDir
	// 必须全为零值，否则构造报错（防止两半配置静默拼接）；回调返回 (nil, nil)
	// 同样报错。设置后进程组设置收口到 Runner，transport 不再调用
	// configureProcessGroup（重复赋值 SysProcAttr 会互相覆盖）。
	BuildCommand func() (*exec.Cmd, error)
}

type stdioState int

const (
	stdioStateNew stdioState = iota
	stdioStateStarting
	stdioStateRunning
	stdioStateFailed
	stdioStateClosing
	stdioStateClosed
)

// stdioResult 是 pending 请求的结果载荷；err 非 nil 时 response 为零值。
type stdioResult struct {
	response Response
	err      error
}

// StdioTransport 通过本地子进程的 stdin/stdout 交换单行分隔的 JSON-RPC
// 消息。并发路由、进程状态和关闭全部封装在本结构内，上层 Client 不感知
// 连接类型。
type StdioTransport struct {
	options StdioTransportOptions

	stateMu sync.Mutex
	state   stdioState
	err     error
	cmd     *exec.Cmd

	// started 在启动流程完成（成功或失败）时关闭；启动结果保存在
	// state/err 中。并发首次 Send 与 Close 都等待同一个 started。
	started chan struct{}

	// terminated 在 Transport 进入 failed/closing 终态时关闭一次，
	// 用于唤醒所有等待响应的 Send。
	terminated chan struct{}
	closeOnce  sync.Once

	// closedCh 在 Close 流程完成时关闭，保证 Close 幂等且并发调用
	// 等待同一次回收。
	closedCh chan struct{}

	// done 在 wait goroutine 完成（进程已被回收）时关闭。任何关闭路径
	// 都等待同一个 done；强制终止前必须确认它尚未关闭。
	done chan struct{}

	// stdin 写入受 writeMu 串行化：保证一条消息及其换行写完后，下一条
	// 消息才能开始。stdin 写入是 writeMu 内唯一允许的预期短暂阻塞操作；
	// Close 不获取 writeMu，直接关闭 stdin 并终止进程，从而解除正在
	// 进行的阻塞写入（Go 管道 fd 注册 netpoller，Close 可唤醒阻塞写）。
	writeMu sync.Mutex
	stdin   io.WriteCloser

	pendingMu sync.Mutex
	pending   map[int64]chan stdioResult

	// dropped 统计被安全丢弃的无法路由消息（未知/无法解析的响应 ID、
	// server-to-client 请求与通知）。计数不含任何消息内容。
	dropped atomic.Int64
}

func NewStdioTransport(options StdioTransportOptions) (*StdioTransport, error) {
	if options.BuildCommand != nil {
		// 回调模式：旧字段必须全为零值（设计 §7 互斥规则）。
		if options.Command != "" || len(options.Args) > 0 ||
			len(options.Env) > 0 || options.WorkDir != "" {
			return nil, errors.New("mcp stdio: BuildCommand 与 Command/Args/Env/WorkDir 互斥")
		}
	} else if options.Command == "" {
		return nil, errors.New("mcp stdio command is required")
	}
	for _, field := range append([]string{options.Command, options.WorkDir}, options.Args...) {
		if containsNUL(field) {
			return nil, errors.New("mcp stdio command/args/cwd must not contain NUL")
		}
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	return &StdioTransport{
		options:    options,
		started:    make(chan struct{}),
		terminated: make(chan struct{}),
		closedCh:   make(chan struct{}),
		done:       make(chan struct{}),
		pending:    make(map[int64]chan stdioResult),
	}, nil
}

func containsNUL(value string) bool {
	return bytes.IndexByte([]byte(value), 0) >= 0
}

// ensureStarted 保证进程已启动。只允许一个 goroutine 执行启动；其他调用
// 等待同一个启动结果。进入 failed/closing/closed 终态时返回稳定终端错误。
func (t *StdioTransport) ensureStarted() error {
	t.stateMu.Lock()
	switch t.state {
	case stdioStateRunning:
		t.stateMu.Unlock()
		return nil
	case stdioStateNew:
		t.state = stdioStateStarting
		t.stateMu.Unlock()
		t.start()
		t.stateMu.Lock()
		state, err := t.state, t.err
		t.stateMu.Unlock()
		if state == stdioStateRunning {
			return nil
		}
		return err
	case stdioStateStarting:
		started := t.started
		t.stateMu.Unlock()
		<-started
		t.stateMu.Lock()
		state, err := t.state, t.err
		t.stateMu.Unlock()
		if state == stdioStateRunning {
			return nil
		}
		return err
	default:
		err := t.err
		t.stateMu.Unlock()
		if err == nil {
			err = errors.New("mcp transport is closed")
		}
		return err
	}
}

// start 创建 pipe、启动命令和 reader/wait goroutine。调用方已持有启动
// 所有权（状态已切到 starting）。
func (t *StdioTransport) start() {
	defer close(t.started)

	var command *exec.Cmd
	if t.options.BuildCommand != nil {
		// 回调路径：进程构造（含沙箱包装、进程组设置）收口到 Runner（设计 §7），
		// transport 不再调用 configureProcessGroup——两者都会给 SysProcAttr
		// 赋值，重复设置会互相覆盖。
		cmd, err := t.options.BuildCommand()
		if err != nil {
			t.failStarting("build command", err)
			return
		}
		if cmd == nil {
			t.failStarting("build command", errors.New("mcp stdio: BuildCommand 返回 nil 命令"))
			return
		}
		command = cmd
	} else {
		command = exec.Command(t.options.Command, t.options.Args...)
		command.Dir = t.options.WorkDir
		command.Env = t.options.Env
		configureProcessGroup(command)
	}

	stdin, err := command.StdinPipe()
	if err != nil {
		t.failStarting("start process", err)
		return
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		t.failStarting("start process", err)
		return
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		t.failStarting("start process", err)
		return
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		t.failStarting("start process", err)
		return
	}

	t.stateMu.Lock()
	t.cmd = command
	t.stdin = stdin
	t.stateMu.Unlock()

	// cmd.Start 成功后必须恰好调用一次 cmd.Wait；wait goroutine 是它
	// 的唯一所有者，完成后关闭 done 并回收 pipe。
	go t.waitLoop(command)
	go t.readLoop(stdout)
	go t.drainStderr(stderr)

	t.stateMu.Lock()
	// 启动期间可能已发生协议失败或 Close，只有状态仍为 starting 时才
	// 进入 running。
	if t.state == stdioStateStarting {
		t.state = stdioStateRunning
	}
	t.stateMu.Unlock()
}

func (t *StdioTransport) failStarting(kind string, cause error) {
	t.stateMu.Lock()
	t.state = stdioStateFailed
	t.err = &transportError{op: "start", kind: kind, cause: cause}
	t.stateMu.Unlock()
	t.closeTerminated()
}

// waitLoop 等待子进程退出。无论退出原因如何，都让 Transport 进入 failed
// （除非关闭流程已取得状态所有权），使所有 pending 请求收到稳定的
// "stdio server exited" 终端错误。
func (t *StdioTransport) waitLoop(command *exec.Cmd) {
	defer close(t.done)
	waitErr := command.Wait()
	t.stateMu.Lock()
	state := t.state
	t.stateMu.Unlock()
	if state == stdioStateRunning {
		cause := errors.New("stdio server exited")
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			cause = fmt.Errorf("stdio server exited: %s", exit.String())
		} else if waitErr != nil {
			cause = fmt.Errorf("stdio server exited: %v", waitErr)
		}
		t.fail("stdio", "server exited", cause)
	}
}

// fail 原子进入 failed：使所有 pending 请求收到同一个终端错误，关闭
// stdin 并回收完整子进程树。已是终态时为 no-op。
func (t *StdioTransport) fail(op string, kind string, cause error) {
	t.stateMu.Lock()
	if t.state != stdioStateRunning && t.state != stdioStateStarting {
		t.stateMu.Unlock()
		return
	}
	t.state = stdioStateFailed
	t.err = &transportError{op: op, kind: kind, cause: cause}
	t.stateMu.Unlock()

	t.closeTerminated()
	t.failPending(&transportError{op: op, kind: kind, cause: cause})
	t.closeStdin()
	t.killProcessTree()
}

// failPending 把终端错误发给所有已注册的 pending 请求（锁外发送）。
func (t *StdioTransport) failPending(err error) {
	t.pendingMu.Lock()
	channels := make([]chan stdioResult, 0, len(t.pending))
	for id, channel := range t.pending {
		delete(t.pending, id)
		channels = append(channels, channel)
	}
	t.pendingMu.Unlock()
	for _, channel := range channels {
		channel <- stdioResult{err: err}
	}
}

func (t *StdioTransport) closeTerminated() {
	t.closeOnce.Do(func() { close(t.terminated) })
}

func (t *StdioTransport) closeStdin() {
	t.stateMu.Lock()
	stdin := t.stdin
	t.stateMu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
}

// killProcessGroup 强制终止完整进程树。调用前确认 done 尚未关闭（进程
// 尚未被 wait goroutine 回收），并立即使用启动时保存的 PID。
func (t *StdioTransport) killProcessTree() {
	select {
	case <-t.done:
		return
	default:
	}
	t.stateMu.Lock()
	command := t.cmd
	t.stateMu.Unlock()
	if command == nil || command.Process == nil {
		return
	}
	_ = killProcessTree(command.Process)
}

func (t *StdioTransport) Send(ctx context.Context, request Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, &transportError{op: request.Method, kind: "request timeout", cause: err}
	}
	if err := t.ensureStarted(); err != nil {
		return Response{}, err
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return Response{}, &transportError{op: request.Method, kind: "write request", cause: err}
	}
	payload = append(payload, '\n')

	if request.ID == nil {
		// notification：完成原子写入后立即返回，不等待 stdout。
		if err := t.writeMessage(payload); err != nil {
			return Response{}, err
		}
		return Response{}, nil
	}

	// 先注册 pending 再写入，防止 Server 极快响应时丢失消息。
	resultCh := make(chan stdioResult, 1)
	t.pendingMu.Lock()
	t.pending[*request.ID] = resultCh
	t.pendingMu.Unlock()
	unregister := func() {
		t.pendingMu.Lock()
		if channel, exists := t.pending[*request.ID]; exists && channel == resultCh {
			delete(t.pending, *request.ID)
		}
		t.pendingMu.Unlock()
	}

	if err := t.writeMessage(payload); err != nil {
		unregister()
		return Response{}, err
	}

	// Timeout 是本次 Send 等待响应的期限；调用方更早的 Deadline 通过
	// ctx 层级自然生效。
	waitCtx, cancel := context.WithTimeout(ctx, t.options.Timeout)
	defer cancel()
	select {
	case result := <-resultCh:
		if result.err != nil {
			return Response{}, result.err
		}
		return result.response, nil
	case <-waitCtx.Done():
		unregister()
		return Response{}, &transportError{op: request.Method, kind: "request timeout", cause: waitCtx.Err()}
	case <-t.terminated:
		unregister()
		t.stateMu.Lock()
		err := t.err
		t.stateMu.Unlock()
		if err == nil {
			err = errors.New("mcp transport is closed")
		}
		return Response{}, err
	}
}

// writeMessage 在 writeMu 保护下完整写入一条消息及其换行。
func (t *StdioTransport) writeMessage(payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.stateMu.Lock()
	stdin := t.stdin
	t.stateMu.Unlock()
	if stdin == nil {
		return errors.New("mcp transport is closed")
	}
	if err := writeFull(stdin, payload); err != nil {
		return &transportError{op: "stdio", kind: "write request", cause: err}
	}
	return nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// stdioWireMessage 是 stdio wire 层的消息 envelope：入站 ID 使用
// json.RawMessage，以区分“缺失”与各种非法形态（字符串/浮点/null）。
type stdioWireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// readLoop 是 stdout 的唯一读取者：按换行切分，每条消息分类处理。
// 协议错误（超过上限、非法 UTF-8、非 JSON、非法 JSON-RPC 版本）使
// Transport 进入 failed。空行与纯空白行跳过。
func (t *StdioTransport) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), int(maxMessageBytes))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := t.handleLine(line); err != nil {
			t.fail("stdio", "read response", err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		kind := "read response"
		if errors.Is(err, bufio.ErrTooLong) {
			kind = "response too large"
		}
		t.fail("stdio", kind, err)
	}
}

func (t *StdioTransport) handleLine(line []byte) error {
	if !utf8.Valid(line) {
		return errors.New("invalid JSON-RPC message: not valid UTF-8")
	}
	var message stdioWireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Errorf("invalid JSON-RPC message: %w", err)
	}
	if message.JSONRPC != "2.0" {
		return errors.New("invalid JSON-RPC message: unsupported jsonrpc version")
	}
	if len(message.ID) > 0 {
		t.routeResponse(message)
		return nil
	}
	// Server notification/request：验证为合法 JSON-RPC 后忽略，不阻塞
	// reader。本期不实现 server-to-client responder，也不回应 ping。
	t.dropped.Add(1)
	return nil
}

// routeResponse 按 ID 路由响应；无法解析为 int64、越界或未命中 pending
// 的 ID 统一按未知响应丢弃，不使 Transport 失败。
func (t *StdioTransport) routeResponse(message stdioWireMessage) {
	var id int64
	if err := json.Unmarshal(message.ID, &id); err != nil {
		t.dropped.Add(1)
		return
	}
	t.pendingMu.Lock()
	channel, exists := t.pending[id]
	if exists {
		delete(t.pending, id)
	}
	t.pendingMu.Unlock()
	if !exists {
		t.dropped.Add(1)
		return
	}
	result := stdioResult{response: Response{JSONRPC: message.JSONRPC, ID: &id, Result: message.Result}}
	if message.Error != nil {
		result.err = &transportError{op: "stdio", code: message.Error.Code}
	}
	channel <- result
}

// drainStderr 用固定大小读缓冲持续排空并丢弃 stderr，避免 pipe 填满
// 导致子进程阻塞。stderr 内容不进入日志、Metrics 或 Span。
func (t *StdioTransport) drainStderr(reader io.Reader) {
	buffer := make([]byte, 4096)
	for {
		if _, err := reader.Read(buffer); err != nil {
			return
		}
	}
}

// Close 关闭 Transport：先 EOF 再有界回收。幂等；并发调用等待同一次
// 关闭流程完成。
func (t *StdioTransport) Close(ctx context.Context) error {
	t.stateMu.Lock()
	switch t.state {
	case stdioStateClosed:
		t.stateMu.Unlock()
		<-t.closedCh
		return nil
	case stdioStateNew:
		t.state = stdioStateClosed
		close(t.closedCh)
		t.stateMu.Unlock()
		t.closeTerminated()
		return nil
	case stdioStateStarting:
		started := t.started
		t.stateMu.Unlock()
		// 等待启动完成，最多启动一个进程后再关闭。
		<-started
		return t.Close(ctx)
	case stdioStateClosing:
		t.stateMu.Unlock()
		<-t.closedCh
		return nil
	default:
		t.state = stdioStateClosing
		t.stateMu.Unlock()
	}

	t.closeTerminated()
	t.failPending(&transportError{op: "close", kind: "transport closed"})
	t.closeStdin()

	// 等待子进程自然退出（EOF），最长 min(5 秒, Close Context 剩余时间)。
	grace := stdioCloseGracePeriod
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < grace {
			grace = remaining
		}
	}
	if grace > 0 {
		select {
		case <-t.done:
		case <-time.After(grace):
			t.killProcessTree()
		}
	} else {
		t.killProcessTree()
	}

	// 由 wait goroutine 完成 cmd.Wait 与 pipe 回收；尊重调用方 Deadline。
	select {
	case <-t.done:
	case <-ctx.Done():
	}

	t.stateMu.Lock()
	t.state = stdioStateClosed
	close(t.closedCh)
	t.stateMu.Unlock()
	return nil
}
