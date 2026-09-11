// Package agentsmoke checks candidate script syntax without executing business
// code. Native sandboxes deny networking and writes outside a disposable tmp.
package agentsmoke

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
)

type Checker struct{ config config.AgentTrainingConfig }

func New(c config.AgentTrainingConfig) (*Checker, error) {
	if c.SmokeTimeoutSeconds <= 0 {
		return nil, errors.New("script timeout must be positive")
	}
	return &Checker{c}, nil
}

func pinned(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("interpreter dependency is not a regular file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return fmt.Errorf("interpreter dependency pin mismatch: %s", path)
	}
	return nil
}

func (c *Checker) Check(ctx context.Context, root string, _ agentversion.Snapshot) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".tmp" || d.Name() == "scratch" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".py" && ext != ".sh" {
			return nil
		}
		var approved *config.ApprovedInterpreter
		for i := range c.config.ApprovedInterpreters {
			if c.config.ApprovedInterpreters[i].Extension == ext {
				approved = &c.config.ApprovedInterpreters[i]
				break
			}
		}
		if approved == nil {
			return fmt.Errorf("script %s requires an approved isolated interpreter", filepath.Base(path))
		}
		if err := pinned(approved.Executable, approved.SHA256); err != nil {
			return err
		}
		for _, dep := range approved.Dependencies {
			if err := pinned(dep.Path, dep.SHA256); err != nil {
				return err
			}
		}
		argv := []string{approved.Executable, "-n", path}
		if ext == ".py" {
			argv = []string{approved.Executable, "-I", "-S", "-c", "import ast,sys; ast.parse(open(sys.argv[1], encoding='utf-8').read(), filename=sys.argv[1])", path}
		}
		checkCtx, cancel := context.WithTimeout(ctx, time.Duration(c.config.SmokeTimeoutSeconds)*time.Second)
		defer cancel()
		if err := c.run(checkCtx, root, argv, *approved); err != nil {
			return fmt.Errorf("script syntax/startup check %s: %w", filepath.Base(path), err)
		}
		return pinned(approved.Executable, approved.SHA256)
	})
}

func (c *Checker) run(ctx context.Context, root string, argv []string, approved config.ApprovedInterpreter) error {
	tmp, err := os.MkdirTemp("", "agent-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	tmp, err = filepath.EvalSymlinks(tmp)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		binary, err := exec.LookPath("sandbox-exec")
		if err != nil {
			return err
		}
		profile := `(version 1)(deny default)(allow process-exec)(allow process-fork)(allow signal (target same-sandbox))(allow sysctl-read)(allow mach-lookup)(allow file-read-metadata)(allow file-read* (literal "/") (subpath "/usr") (subpath "/System") (subpath "/bin") (literal "/dev/null") (subpath ` + strconv.Quote(root) + `))(allow file-write* (literal "/dev/null"))` +
			`(allow file-read* file-write* (subpath ` + strconv.Quote(tmp) + `))(allow file-read* (literal ` + strconv.Quote(approved.Executable) + `))`
		for _, dep := range approved.Dependencies {
			profile += `(allow file-read* (literal ` + strconv.Quote(dep.Path) + `))`
		}
		cmd = sandbox.BuildCommand(binary, append([]string{"-p", profile}, argv...), root)
	case "linux":
		binary, err := exec.LookPath("bwrap")
		if err != nil {
			return err
		}
		args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--clearenv", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"}
		for _, system := range []string{"/usr", "/bin", "/lib", "/lib64"} {
			if _, err := os.Stat(system); err == nil {
				args = append(args, "--ro-bind", system, system)
			}
		}
		args = append(args, "--ro-bind", root, root, "--ro-bind", approved.Executable, approved.Executable)
		for _, dep := range approved.Dependencies {
			args = append(args, "--ro-bind", dep.Path, dep.Path)
		}
		args = append(args, "--chdir", root, "--setenv", "HOME", "/tmp", "--setenv", "TMPDIR", "/tmp", "--")
		cmd = sandbox.BuildCommand(binary, append(args, argv...), root)
	default:
		return errors.New("isolated script checking unsupported on this OS")
	}
	cmd.Env = []string{"HOME=" + tmp, "TMPDIR=" + tmp, "LANG=C", "LC_ALL=C"}
	var diagnostic bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &diagnostic
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		stopErr := sandbox.KillProcessGroup(cmd.Process)
		if err != nil {
			message := diagnostic.String()
			if len(message) > 2048 {
				message = message[:2048]
			}
			err = fmt.Errorf("%w: %s", err, message)
		}
		return errors.Join(err, stopErr)
	case <-ctx.Done():
		stopErr := sandbox.KillProcessGroup(cmd.Process)
		return errors.Join(ctx.Err(), stopErr, <-done)
	}
}
