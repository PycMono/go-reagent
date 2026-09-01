package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const probeTimeout = 10 * time.Second

// NewRunner 按操作系统选择固定后端。macOS/Linux 缺少所需后端时
// 直接返回错误，不降级为 Host。
func NewRunner(workspaceRoot string) (Runner, error) {
	return newRunnerForOS(runtime.GOOS, workspaceRoot, exec.LookPath)
}

func newRunnerForOS(goos, workspaceRoot string, lookPath func(string) (string, error)) (Runner, error) {
	switch goos {
	case "darwin":
		path, err := lookPath("/usr/bin/sandbox-exec")
		if err != nil {
			return nil, fmt.Errorf("macOS Seatbelt 后端不可用: %w", err)
		}
		return NewSeatbeltRunner(path, workspaceRoot)
	case "linux":
		path, err := lookPath("bwrap")
		if err != nil {
			return nil, fmt.Errorf("Linux Bubblewrap 后端不可用: %w", err)
		}
		return NewBubblewrapRunner(path, workspaceRoot)
	case "windows":
		return NewHostRunner(), nil
	default:
		return nil, fmt.Errorf("不支持的操作系统 %q", goos)
	}
}

func runProbe(ctx context.Context, cmd *exec.Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		return buf.String(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return buf.String(), fmt.Errorf("探针超时/取消: %w", ctx.Err())
	}
}

// ProbeBubblewrap 使用完整生产参数探测 Bubblewrap 是否可用。
func ProbeBubblewrap(ctx context.Context, runner *BubblewrapRunner) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := runner.BuildShell("exit 0", CommandSpec{
		WorkDir:    runner.workspaceRoot,
		PayloadEnv: Env(runner.workspaceRoot, "/tmp"),
	})
	if err != nil {
		return err
	}
	out, err := runProbe(ctx, cmd)
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(out, "No permissions to create new namespace"),
		strings.Contains(out, "setting up uid map"):
		return fmt.Errorf("bwrap 不可用（内核/AppArmor 禁止 user namespace）: %s", out)
	case strings.Contains(out, "Can't find source path"):
		return fmt.Errorf("bwrap 必需挂载源缺失: %s", out)
	default:
		return fmt.Errorf("bwrap 探针失败: %w: %s", err, out)
	}
}

// ProbeSeatbelt 使用完整生产参数探测 Seatbelt 是否可用。
func ProbeSeatbelt(ctx context.Context, runner *SeatbeltRunner) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := runner.BuildShell("exit 0", CommandSpec{
		WorkDir:    runner.workspaceRoot,
		PayloadEnv: Env(runner.workspaceRoot, runner.tmpDir),
	})
	if err != nil {
		return err
	}
	if out, err := runProbe(ctx, cmd); err != nil {
		return fmt.Errorf("seatbelt 探针失败: %w: %s", err, out)
	}
	return nil
}
