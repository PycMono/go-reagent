package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"
)

const defaultProcessOutputBytes = 50 * 1024

type ProcessStart struct {
	Command  string
	WorkDir  string
	Env      map[string]string
	Timeout  time.Duration
	OnOutput func(stream string, chunk []byte)
}

type ProcessLog struct {
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	Truncated  bool   `json:"truncated"`
}

type ProcessSnapshot struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Command   string `json:"command"`
	CWD       string `json:"cwd"`
	Output    string `json:"-"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Truncated bool   `json:"truncated"`
}

type ProcessSupervisor struct {
	workspace *Workspace

	mu        sync.RWMutex
	sessions  map[string]*processSession
	nextID    uint64
	closed    bool
	closeDone chan struct{}
}

type processSession struct {
	id      string
	command string
	workDir string
	started time.Time
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	output  *boundedProcessLog
	done    chan struct{}

	mu        sync.Mutex
	stdinMu   sync.Mutex
	status    string
	exitCode  *int
	errorText string
}

type boundedProcessLog struct {
	mu         sync.Mutex
	data       []byte
	baseOffset int64
	endOffset  int64
	truncated  bool
}

type processStreamWriter struct {
	stream   string
	log      *boundedProcessLog
	onOutput func(string, []byte)
}

func NewProcessSupervisor(lifecycle fx.Lifecycle, workspace *Workspace) (*ProcessSupervisor, error) {
	if lifecycle == nil {
		return nil, errors.New("lifecycle 不能为空")
	}
	if workspace == nil {
		return nil, errors.New("workspace 未初始化")
	}
	supervisor := &ProcessSupervisor{
		workspace: workspace,
		sessions:  make(map[string]*processSession),
		closeDone: make(chan struct{}),
	}
	lifecycle.Append(fx.Hook{OnStop: func(context.Context) error {
		return supervisor.Close()
	}})
	return supervisor, nil
}

func (s *ProcessSupervisor) Start(ctx context.Context, start ProcessStart) (*processSession, error) {
	if s == nil || s.workspace == nil {
		return nil, errors.New("process supervisor 未初始化")
	}
	if ctx == nil {
		return nil, errors.New("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("命令执行已取消: %w", err)
	}
	if strings.TrimSpace(start.Command) == "" {
		return nil, errors.New("command 不能为空")
	}
	if start.Timeout < 0 {
		return nil, errors.New("timeout 不能小于 0")
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	workDir, err := s.workspace.ResolveDir(start.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("解析工作区目录失败: %w", err)
	}
	shell, shellArgs := shellInvocation(start.Command)
	cmd := exec.Command(shell, shellArgs...)
	cmd.Dir = workDir
	cmd.Env, err = processEnvironment(start.Env)
	if err != nil {
		return nil, err
	}
	configureProcessGroup(cmd)
	output := &boundedProcessLog{}
	cmd.Stdout = &processStreamWriter{stream: "stdout", log: output, onOutput: start.OnOutput}
	cmd.Stderr = &processStreamWriter{stream: "stderr", log: output, onOutput: start.OnOutput}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建命令 stdin 失败: %w", err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = stdin.Close()
		return nil, errors.New("process supervisor 已关闭")
	}
	s.nextID++
	session := &processSession{
		id:      fmt.Sprintf("proc-%d-%d", time.Now().UnixNano(), s.nextID),
		command: start.Command,
		workDir: workDir,
		started: time.Now(),
		cmd:     cmd,
		stdin:   stdin,
		output:  output,
		done:    make(chan struct{}),
		status:  "running",
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		_ = stdin.Close()
		return nil, fmt.Errorf("启动命令失败: %w", err)
	}
	s.sessions[session.id] = session
	s.mu.Unlock()

	go session.wait()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.terminate("canceled")
		case <-session.done:
		}
	}()
	if start.Timeout > 0 {
		go func() {
			timer := time.NewTimer(start.Timeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				_ = session.terminate("timed_out")
			case <-session.done:
			}
		}()
	}
	return session, nil
}

func (s *ProcessSupervisor) List() []ProcessSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	sessions := make([]*processSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].started.Equal(sessions[j].started) {
			return sessions[i].id < sessions[j].id
		}
		return sessions[i].started.Before(sessions[j].started)
	})
	snapshots := make([]ProcessSnapshot, 0, len(sessions))
	for _, session := range sessions {
		snapshots = append(snapshots, session.snapshot())
	}
	return snapshots
}

func (s *ProcessSupervisor) Poll(ctx context.Context, id string, timeout time.Duration) (ProcessSnapshot, error) {
	if ctx == nil {
		return ProcessSnapshot{}, errors.New("context 不能为空")
	}
	if timeout < 0 {
		return ProcessSnapshot{}, errors.New("poll timeout 不能小于 0")
	}
	session, err := s.session(id)
	if err != nil {
		return ProcessSnapshot{}, err
	}
	if timeout == 0 {
		return session.snapshot(), nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-session.done:
		return session.snapshot(), nil
	case <-timer.C:
		return session.snapshot(), nil
	case <-ctx.Done():
		return ProcessSnapshot{}, fmt.Errorf("process poll 已取消: %w", ctx.Err())
	}
}

func (s *ProcessSupervisor) Log(id string, offset int64, limit int) (ProcessLog, error) {
	if offset < 0 {
		return ProcessLog{}, errors.New("log offset 不能小于 0")
	}
	if limit < 0 {
		return ProcessLog{}, errors.New("log limit 不能小于 0")
	}
	session, err := s.session(id)
	if err != nil {
		return ProcessLog{}, err
	}
	return session.output.page(offset, limit), nil
}

func (s *ProcessSupervisor) Write(ctx context.Context, id string, data *string, eof bool) (ProcessSnapshot, error) {
	if ctx == nil {
		return ProcessSnapshot{}, errors.New("context 不能为空")
	}
	session, err := s.session(id)
	if err != nil {
		return ProcessSnapshot{}, err
	}
	writeDone := make(chan error, 1)
	go func() { writeDone <- session.writeInput(data, eof) }()
	select {
	case err := <-writeDone:
		if err != nil {
			return ProcessSnapshot{}, err
		}
		return session.snapshot(), nil
	case <-ctx.Done():
		_ = session.terminate("canceled")
		return ProcessSnapshot{}, fmt.Errorf("process write 已取消: %w", ctx.Err())
	}
}

func (s *ProcessSupervisor) Kill(ctx context.Context, id string) (ProcessSnapshot, error) {
	if ctx == nil {
		return ProcessSnapshot{}, errors.New("context 不能为空")
	}
	session, err := s.session(id)
	if err != nil {
		return ProcessSnapshot{}, err
	}
	if err := session.terminate("killed"); err != nil {
		return ProcessSnapshot{}, fmt.Errorf("终止 process session 失败: %w", err)
	}
	select {
	case <-session.done:
		return session.snapshot(), nil
	case <-ctx.Done():
		return ProcessSnapshot{}, fmt.Errorf("process kill 已取消: %w", ctx.Err())
	}
}

func (s *ProcessSupervisor) Clear() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	removed := 0
	for id, session := range s.sessions {
		select {
		case <-session.done:
			delete(s.sessions, id)
			removed++
		default:
		}
	}
	s.mu.Unlock()
	return removed
}

func (s *ProcessSupervisor) Remove(ctx context.Context, id string) error {
	if ctx == nil {
		return errors.New("context 不能为空")
	}
	session, err := s.session(id)
	if err != nil {
		return err
	}
	if err := session.terminate("killed"); err != nil {
		return fmt.Errorf("终止 process session 失败: %w", err)
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		return fmt.Errorf("process remove 已取消: %w", ctx.Err())
	}
	s.mu.Lock()
	if s.sessions[id] == session {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	return nil
}

func (s *ProcessSupervisor) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		closeDone := s.closeDone
		s.mu.Unlock()
		<-closeDone
		return nil
	}
	s.closed = true
	sessions := make([]*processSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()

	var closeErrors []error
	for _, session := range sessions {
		if err := session.terminate("killed"); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	for _, session := range sessions {
		<-session.done
	}
	s.mu.Lock()
	clear(s.sessions)
	close(s.closeDone)
	s.mu.Unlock()
	return errors.Join(closeErrors...)
}

func (s *ProcessSupervisor) ensureOpen() error {
	if s == nil {
		return errors.New("process supervisor 未初始化")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errors.New("process supervisor 已关闭")
	}
	return nil
}

func (s *ProcessSupervisor) session(id string) (*processSession, error) {
	if s == nil {
		return nil, errors.New("process supervisor 未初始化")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("sessionId 不能为空")
	}
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("process session 不存在: %s", id)
	}
	return session, nil
}

func (s *processSession) wait() {
	err := s.cmd.Wait()
	exitCode := s.cmd.ProcessState.ExitCode()
	s.mu.Lock()
	if s.status == "running" {
		s.status = "completed"
		if err != nil {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				s.status = "failed"
				s.errorText = err.Error()
			}
		}
	}
	s.exitCode = &exitCode
	s.mu.Unlock()
	s.stdinMu.Lock()
	_ = s.stdin.Close()
	s.stdinMu.Unlock()
	close(s.done)
}

func (s *processSession) terminate(status string) error {
	select {
	case <-s.done:
		return nil
	default:
	}
	s.mu.Lock()
	if s.status != "running" {
		s.mu.Unlock()
		return nil
	}
	s.status = status
	s.mu.Unlock()
	return killProcessGroup(s.cmd.Process)
}

func (s *processSession) snapshot() ProcessSnapshot {
	s.mu.Lock()
	status, exitCode := s.status, s.exitCode
	s.mu.Unlock()
	output, truncated := s.output.snapshot()
	return ProcessSnapshot{
		SessionID: s.id,
		Status:    status,
		Command:   s.command,
		CWD:       s.workDir,
		Output:    output,
		ExitCode:  exitCode,
		Truncated: truncated,
	}
}

func (s *processSession) writeInput(data *string, eof bool) error {
	s.mu.Lock()
	running := s.status == "running"
	s.mu.Unlock()
	if !running {
		return errors.New("process session 已结束")
	}
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if data != nil && *data != "" {
		if _, err := io.WriteString(s.stdin, *data); err != nil {
			return fmt.Errorf("写入 process stdin 失败: %w", err)
		}
	}
	if eof {
		if err := s.stdin.Close(); err != nil {
			return fmt.Errorf("关闭 process stdin 失败: %w", err)
		}
	}
	return nil
}

func (w *processStreamWriter) Write(content []byte) (int, error) {
	written, err := w.log.Write(content)
	if err != nil || w.onOutput == nil || len(content) == 0 {
		return written, err
	}
	chunk := bytes.Clone(content)
	func() {
		defer func() { _ = recover() }()
		w.onOutput(w.stream, chunk)
	}()
	return written, nil
}

func (b *boundedProcessLog) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(content)
	b.endOffset += int64(originalLength)
	if len(content) >= defaultProcessOutputBytes {
		b.data = append(b.data[:0], content[len(content)-defaultProcessOutputBytes:]...)
		b.baseOffset = b.endOffset - int64(len(b.data))
		b.truncated = true
		return originalLength, nil
	}
	b.data = append(b.data, content...)
	if len(b.data) > defaultProcessOutputBytes {
		overflow := len(b.data) - defaultProcessOutputBytes
		copy(b.data, b.data[overflow:])
		b.data = b.data[:defaultProcessOutputBytes]
		b.baseOffset += int64(overflow)
		b.truncated = true
	}
	return originalLength, nil
}

func (b *boundedProcessLog) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.data)), b.truncated
}

func (b *boundedProcessLog) page(offset int64, limit int) ProcessLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	requestedOffset := offset
	if offset < b.baseOffset {
		offset = b.baseOffset
	}
	if offset > b.endOffset {
		offset = b.endOffset
	}
	available := b.endOffset - offset
	if limit == 0 || int64(limit) > available {
		limit = int(available)
	}
	start := int(offset - b.baseOffset)
	nextOffset := offset + int64(limit)
	return ProcessLog{
		Content:    string(bytes.Clone(b.data[start : start+limit])),
		Offset:     offset,
		NextOffset: nextOffset,
		Truncated:  requestedOffset < b.baseOffset || nextOffset < b.endOffset,
	}
}

func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-lc", command}
}

func processEnvironment(overrides map[string]string) ([]string, error) {
	environment := os.Environ()
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("无效环境变量名: %q", key)
		}
		if strings.EqualFold(key, "PATH") {
			return nil, errors.New("禁止通过 exec 覆盖 PATH")
		}
		value := overrides[key]
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("环境变量 %s 包含 NUL", key)
		}
		environment = append(environment, key+"="+value)
	}
	return environment, nil
}
