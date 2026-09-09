package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/harness/sandbox"
)

func TestProcessSupervisorSeparatesStreamsAndKeepsBoundedAbsoluteLog(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	var outputMu sync.Mutex
	streams := make(map[string]string)
	session, err := supervisor.Start(context.Background(), ProcessStart{
		Command: toolHelperCommand("output-exit"),
		OnOutput: func(stream string, chunk []byte) {
			outputMu.Lock()
			streams[stream] += string(chunk)
			outputMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := supervisor.Poll(context.Background(), session.id, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	outputMu.Lock()
	stdout, stderr := streams["stdout"], streams["stderr"]
	outputMu.Unlock()
	if stdout != "stdout" || stderr != "stderr" {
		t.Fatalf("stream output = stdout %q, stderr %q", stdout, stderr)
	}
	if snapshot.ExitCode == nil || *snapshot.ExitCode != 7 || !strings.Contains(snapshot.Output, "stdout") || !strings.Contains(snapshot.Output, "stderr") {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	large, err := supervisor.Start(context.Background(), ProcessStart{Command: toolHelperCommand("large-output", "60000")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Poll(context.Background(), large.id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	page, err := supervisor.Log(large.id, 0, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 60_000-defaultProcessOutputBytes || page.NextOffset != 60_000 || len(page.Content) != defaultProcessOutputBytes || !page.Truncated {
		t.Fatalf("log page = offset %d, next %d, bytes %d, truncated %v", page.Offset, page.NextOffset, len(page.Content), page.Truncated)
	}
	tail, err := supervisor.Log(large.id, page.NextOffset, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if tail.Offset != 60_000 || tail.NextOffset != 60_000 || tail.Content != "" || tail.Truncated {
		t.Fatalf("tail page = %#v", tail)
	}
}

type policyRunner struct{ policy sandbox.Policy }

func (r policyRunner) Policy() sandbox.Policy { return r.policy }
func (r policyRunner) BuildShell(string, sandbox.CommandSpec) (*exec.Cmd, error) {
	return nil, errors.New("not used")
}
func (r policyRunner) BuildArgv([]string, sandbox.CommandSpec) (*exec.Cmd, error) {
	return nil, errors.New("not used")
}

func TestProcessSupervisorSandboxPayloadDoesNotInheritHostEnvironment(t *testing.T) {
	t.Setenv("REAGENT_HOST_SECRET", "must-not-leak")
	root := t.TempDir()
	for _, tt := range []struct {
		backend string
	}{
		{backend: "seatbelt"},
		{backend: "bubblewrap"},
	} {
		tmpDir := filepath.Join(root, ".tmp")
		supervisor := &ProcessSupervisor{
			workspace: &Workspace{path: root},
			runner: policyRunner{policy: sandbox.Policy{
				Backend:   tt.backend,
				Network:   "allow",
				WriteMode: "restricted",
				TmpDir:    tmpDir,
			}},
		}
		env, err := supervisor.payloadEnv(map[string]string{"EXPLICIT": "ok"})
		if err != nil {
			t.Fatalf("%s payloadEnv() error = %v", tt.backend, err)
		}
		joined := strings.Join(env, "\n")
		if strings.Contains(joined, "REAGENT_HOST_SECRET=") {
			t.Fatalf("%s 泄露宿主环境: %v", tt.backend, env)
		}
		if !strings.Contains(joined, "EXPLICIT=ok") || !strings.Contains(joined, "TMPDIR="+tmpDir) {
			t.Fatalf("%s payload 不完整: %v", tt.backend, env)
		}
	}
}

func TestProcessSupervisorSandboxPayloadRejectsContractOverrides(t *testing.T) {
	root := t.TempDir()
	supervisor := &ProcessSupervisor{workspace: &Workspace{path: root}, runner: policyRunner{policy: sandbox.Policy{
		Backend: "seatbelt", WriteMode: "restricted", TmpDir: filepath.Join(root, ".tmp"),
	}}}
	for _, key := range []string{"HOME", "PATH", "TMPDIR"} {
		if _, err := supervisor.payloadEnv(map[string]string{key: "/override"}); err == nil {
			t.Fatalf("contract variable %s override accepted", key)
		}
	}
}

func TestProcessSupervisorSandboxPayloadRejectsInvalidEnvironmentName(t *testing.T) {
	supervisor := &ProcessSupervisor{
		workspace: &Workspace{path: t.TempDir()},
		runner: policyRunner{policy: sandbox.Policy{
			Backend: "bubblewrap",
			Network: "allow",
		}},
	}
	if _, err := supervisor.payloadEnv(map[string]string{"BAD=NAME": "value"}); err == nil {
		t.Fatal("含等号的环境变量名应被拒绝")
	}
}

func TestProcessSupervisorConcurrentSessionActionsAreRaceSafe(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	writer := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("copy-stdin")})
	killed := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")})
	removed := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")})

	var wg sync.WaitGroup
	errorsSeen := make(chan error, 64)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = supervisor.List()
				if _, err := supervisor.Poll(context.Background(), writer.id, 0); err != nil {
					errorsSeen <- err
				}
				if _, err := supervisor.Log(writer.id, 0, 256); err != nil {
					errorsSeen <- err
				}
			}
		}()
	}
	clearDone := make(chan int, 1)
	go func() { clearDone <- supervisor.Clear() }()
	if removedCount := <-clearDone; removedCount != 0 {
		t.Fatalf("Clear() while all sessions are running = %d, want 0", removedCount)
	}
	wg.Add(3)
	go func() {
		defer wg.Done()
		data := "concurrent input"
		if _, err := supervisor.Write(context.Background(), writer.id, &data, true); err != nil {
			errorsSeen <- err
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := supervisor.Kill(context.Background(), killed.id); err != nil {
			errorsSeen <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := supervisor.Remove(context.Background(), removed.id); err != nil {
			errorsSeen <- err
		}
	}()
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent action: %v", err)
	}

	writerSnapshot, err := supervisor.Poll(context.Background(), writer.id, 5*time.Second)
	if err != nil || writerSnapshot.Output != "concurrent input" {
		t.Fatalf("writer snapshot = %#v, error = %v", writerSnapshot, err)
	}
	killedSnapshot, err := supervisor.Poll(context.Background(), killed.id, 5*time.Second)
	if err != nil || killedSnapshot.Status != "killed" {
		t.Fatalf("killed snapshot = %#v, error = %v", killedSnapshot, err)
	}
	if _, err := supervisor.Poll(context.Background(), removed.id, 0); err == nil {
		t.Fatal("removed session was retained")
	}
	if removedCount := supervisor.Clear(); removedCount != 2 {
		t.Fatalf("Clear() = %d, want 2", removedCount)
	}
	if sessions := supervisor.List(); len(sessions) != 0 {
		t.Fatalf("sessions after Clear = %#v", sessions)
	}
}

func TestProcessSupervisorTimeoutCancellationAndProcessGroupTermination(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	timedOut := mustStartProcess(t, supervisor, ProcessStart{
		Command: toolHelperCommand("sleep", "5000"),
		Timeout: 50 * time.Millisecond,
	})
	timedOutSnapshot, err := supervisor.Poll(context.Background(), timedOut.id, 5*time.Second)
	if err != nil || timedOutSnapshot.Status != "timed_out" {
		t.Fatalf("timeout snapshot = %#v, error = %v", timedOutSnapshot, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceled := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")}, ctx)
	cancel()
	canceledSnapshot, err := supervisor.Poll(context.Background(), canceled.id, 5*time.Second)
	if err != nil || canceledSnapshot.Status != "canceled" {
		t.Fatalf("canceled snapshot = %#v, error = %v", canceledSnapshot, err)
	}

	marker := filepath.Join(t.TempDir(), "grandchild-survived")
	group := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("spawn-child", marker)})
	waitForFile(t, marker+".ready", 5*time.Second)
	if _, err := supervisor.Kill(context.Background(), group.id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("grandchild survived process-group kill: %v", err)
	}
}

func TestProcessSupervisorUsesWorkspaceAndLifecycleCloseIsIdempotent(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(Root(workDir))
	if err != nil {
		t.Fatal(err)
	}
	supervisor := NewProcessSupervisor(workspace, sandbox.NewHostRunner())

	session := mustStartProcess(t, supervisor, ProcessStart{
		Command: toolHelperCommand("cwd-env"),
		WorkDir: "nested",
		Env:     map[string]string{"REAGENT_TEST_VALUE": "workspace"},
	})
	snapshot, err := supervisor.Poll(context.Background(), session.id, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD := filepath.Join(resolvedWorkDir, "nested")
	if snapshot.CWD != wantCWD || snapshot.Output != wantCWD+"|workspace" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")})
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	if sessions := supervisor.List(); len(sessions) != 0 {
		t.Fatalf("sessions after Close = %#v", sessions)
	}
	var closeWG sync.WaitGroup
	closeErrors := make(chan error, 8)
	for range 8 {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			closeErrors <- supervisor.Close()
		}()
	}
	closeWG.Wait()
	close(closeErrors)
	for err := range closeErrors {
		if err != nil {
			t.Fatalf("idempotent Close() error = %v", err)
		}
	}
	if _, err := supervisor.Start(context.Background(), ProcessStart{Command: "true"}); err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatalf("Start() after Close error = %v", err)
	}
}

func newProcessSupervisorForTest(t *testing.T, workDir string) *ProcessSupervisor {
	t.Helper()
	workspace, err := NewWorkspace(Root(workDir))
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	supervisor := NewProcessSupervisor(workspace, sandbox.NewHostRunner())
	t.Cleanup(func() { _ = workspace.Close(); _ = supervisor.Close() })
	return supervisor
}

func mustStartProcess(t *testing.T, supervisor *ProcessSupervisor, start ProcessStart, contexts ...context.Context) *processSession {
	t.Helper()
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	session, err := supervisor.Start(ctx, start)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return session
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
