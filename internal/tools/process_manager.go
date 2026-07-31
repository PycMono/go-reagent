package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultProcessOutputBytes = 50 * 1024

type ProcessManager struct {
	workDir string

	mu        sync.Mutex
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
	output  *tailBuffer
	done    chan struct{}

	mu        sync.Mutex
	stdinMu   sync.Mutex
	status    string
	exitCode  *int
	errorText string
}

type processSnapshot struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Command   string `json:"command"`
	WorkDir   string `json:"workdir"`
	Output    string `json:"output"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

type tailBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func NewProcessManager(workDir string) (*ProcessManager, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil, errors.New("workDir 不能为空")
	}
	absolute, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("解析工作区失败: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("解析工作区真实路径失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("检查工作区失败: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("workDir 必须是目录")
	}
	return &ProcessManager{
		workDir:   resolved,
		sessions:  make(map[string]*processSession),
		closeDone: make(chan struct{}),
	}, nil
}

func (m *ProcessManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		closeDone := m.closeDone
		m.mu.Unlock()
		<-closeDone
		return nil
	}
	m.closed = true
	sessions := make([]*processSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	for _, session := range sessions {
		session.terminate("killed")
	}
	for _, session := range sessions {
		select {
		case <-session.done:
		case <-time.After(time.Second):
		}
	}
	m.mu.Lock()
	clear(m.sessions)
	close(m.closeDone)
	m.mu.Unlock()
	return nil
}

func (m *ProcessManager) start(ctx context.Context, command, relativeWorkDir string, env map[string]string, timeout time.Duration) (*processSession, error) {
	if m == nil {
		return nil, errors.New("process manager 未初始化")
	}
	if ctx == nil {
		return nil, errors.New("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("命令执行已取消: %w", err)
	}
	workDir, err := m.resolveWorkDir(relativeWorkDir)
	if err != nil {
		return nil, err
	}
	shell, shellArgs := shellInvocation(command)
	cmd := exec.Command(shell, shellArgs...)
	cmd.Dir = workDir
	cmd.Env, err = processEnvironment(env)
	if err != nil {
		return nil, err
	}
	configureProcessGroup(cmd)
	output := &tailBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建命令 stdin 失败: %w", err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = stdin.Close()
		return nil, errors.New("process manager 已关闭")
	}
	m.nextID++
	id := fmt.Sprintf("proc-%d-%d", time.Now().UnixNano(), m.nextID)
	session := &processSession{
		id: id, command: command, workDir: workDir, started: time.Now(), cmd: cmd,
		stdin: stdin, output: output, done: make(chan struct{}), status: "running",
	}
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		_ = stdin.Close()
		return nil, fmt.Errorf("启动命令失败: %w", err)
	}
	m.sessions[id] = session
	m.mu.Unlock()

	go session.wait()
	go func() {
		select {
		case <-ctx.Done():
			session.terminate("canceled")
		case <-session.done:
		}
	}()
	if timeout > 0 {
		go func() {
			timer := time.NewTimer(timeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				session.terminate("timed_out")
			case <-session.done:
			}
		}()
	}
	return session, nil
}

func (m *ProcessManager) resolveWorkDir(relative string) (string, error) {
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return m.workDir, nil
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", errors.New("workdir 必须是工作区相对路径")
	}
	cleaned := filepath.Clean(relative)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("workdir 不能逃逸工作区")
	}
	target := filepath.Join(m.workDir, cleaned)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("解析 workdir 失败: %w", err)
	}
	rel, err := filepath.Rel(m.workDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("workdir 不能逃逸工作区")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("检查 workdir 失败: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workdir 必须是目录")
	}
	return resolved, nil
}

func (m *ProcessManager) ensureOpen() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("process manager 已关闭")
	}
	return nil
}

func (m *ProcessManager) snapshot(id string) (processSnapshot, error) {
	session, err := m.session(id)
	if err != nil {
		return processSnapshot{}, err
	}
	return session.snapshot(), nil
}

func (m *ProcessManager) list() []processSnapshot {
	m.mu.Lock()
	sessions := make([]*processSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].started.Before(sessions[j].started) })
	result := make([]processSnapshot, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, session.snapshot())
	}
	return result
}

func (m *ProcessManager) session(id string) (*processSession, error) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	m.mu.Unlock()
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

func (s *processSession) terminate(status string) {
	select {
	case <-s.done:
		return
	default:
	}
	s.mu.Lock()
	if s.status != "running" {
		s.mu.Unlock()
		return
	}
	s.status = status
	s.mu.Unlock()
	_ = killProcessGroup(s.cmd.Process)
}

func (s *processSession) snapshot() processSnapshot {
	s.mu.Lock()
	status, exitCode, errorText := s.status, s.exitCode, s.errorText
	s.mu.Unlock()
	output, truncated := s.output.snapshot()
	return processSnapshot{
		SessionID: s.id, Status: status, Command: s.command, WorkDir: s.workDir,
		Output: output, ExitCode: exitCode, Truncated: truncated, Error: errorText,
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

func (b *tailBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(content)
	if len(content) >= defaultProcessOutputBytes {
		b.data = append(b.data[:0], content[len(content)-defaultProcessOutputBytes:]...)
		b.truncated = true
		return originalLength, nil
	}
	b.data = append(b.data, content...)
	if len(b.data) > defaultProcessOutputBytes {
		overflow := len(b.data) - defaultProcessOutputBytes
		copy(b.data, b.data[overflow:])
		b.data = b.data[:defaultProcessOutputBytes]
		b.truncated = true
	}
	return originalLength, nil
}

func (b *tailBuffer) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.data)), b.truncated
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
