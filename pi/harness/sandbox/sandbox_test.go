package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

var (
	tempRootOnce sync.Once
	tempRootPath string
)

func tempDir() string {
	tempRootOnce.Do(func() {
		real, err := os.MkdirTemp("", "sandbox-test-real-*")
		if err != nil {
			panic(err)
		}
		canonical, err := filepath.EvalSymlinks(real)
		if err != nil {
			panic(err)
		}
		tempRootPath = canonical
	})
	return tempRootPath
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// 契约断言基线：构造函数只收真实存在的路径（EvalSymlinks 需要目录存在）。
var testRoot = mustTempRoot()

func mustTempRoot() string {
	t := tempDir()
	return t
}

func sandboxSpec(workDir, tmpDir string) CommandSpec {
	return CommandSpec{
		WorkDir:    workDir,
		PayloadEnv: Env(workDir, tmpDir),
	}
}

func TestBubblewrapUsesFixedAllowPolicy(t *testing.T) {
	runner, err := NewBubblewrapRunner("/usr/bin/bwrap", testRoot)
	if err != nil {
		t.Fatalf("NewBubblewrapRunner() error = %v", err)
	}
	if got := runner.Policy(); got.Backend != "bubblewrap" || got.Network != "allow" {
		t.Fatalf("Policy() = %+v", got)
	}
	spec := sandboxSpec(testRoot, "/tmp")
	argv := runner.baseArgv(spec, testRoot)
	for _, want := range []string{
		"--die-with-parent", "--unshare-user", "--unshare-pid",
		"--clearenv", "--tmpfs", "/tmp", "--proc", "--dev",
		"--bind", testRoot, "--chdir", testRoot,
		"/etc/hosts", "/etc/resolv.conf", "/etc/ssl",
	} {
		if !slices.Contains(argv, want) {
			t.Fatalf("固定基线缺少 %q: %v", want, argv)
		}
	}
	// payload 注入在沙箱边界内侧：--setenv KEY VALUE
	if !slices.Contains(argv, "--setenv") {
		t.Fatalf("缺少 --setenv 注入: %v", argv)
	}
	if slices.Contains(argv, "--unshare-net") {
		t.Fatalf("固定 allow 策略不应 unshare-net: %v", argv)
	}
}

func TestBubblewrapRejectsContractViolation(t *testing.T) {
	runner, err := NewBubblewrapRunner("/usr/bin/bwrap", testRoot)
	if err != nil {
		t.Fatalf("NewBubblewrapRunner() error = %v", err)
	}
	if _, err := runner.BuildShell("echo", CommandSpec{
		WorkDir:    testRoot,
		PayloadEnv: Env(testRoot, "/tmp"),
	}); err != nil {
		t.Fatalf("合法 spec 不应被拒: %v", err)
	}
	spec := sandboxSpec(testRoot, "/tmp")
	spec.WorkDir = "/etc" // 工作区外
	if _, err := runner.BuildShell("echo", spec); err == nil {
		t.Fatal("WorkDir 越界应被拒")
	}
}

func TestSeatbeltArgvOrder(t *testing.T) {
	runner, err := NewSeatbeltRunner("/usr/bin/sandbox-exec", testRoot)
	if err != nil {
		t.Fatalf("NewSeatbeltRunner() error = %v", err)
	}
	cmd, err := runner.BuildShell("exit 0", sandboxSpec(testRoot, runner.tmpDir))
	if err != nil {
		t.Fatalf("BuildShell() error = %v", err)
	}
	argv := cmd.Args
	pIdx := slices.Index(argv, "-p")
	if pIdx < 0 || !strings.HasPrefix(argv[pIdx+1], "(version 1)") {
		t.Fatalf("profile 应内联在 -p 之后: %v", argv)
	}
	if !strings.Contains(argv[pIdx+1], `(subpath (param "WORKSPACE_ROOT"))`) {
		t.Fatalf("profile 缺少 WORKSPACE_ROOT param: %s", argv[pIdx+1])
	}
	envIdx := slices.Index(argv, "env")
	if envIdx < 0 || argv[envIdx+1] != "-i" {
		t.Fatalf("payload 应经 env -i 注入: %v", argv)
	}
	if !slices.Contains(argv[envIdx:], "HOME="+testRoot) {
		t.Fatalf("payload 缺少 HOME: %v", argv[envIdx:])
	}
	if cmd.Env == nil || !slices.Equal(cmd.Env, []string{"PATH=/usr/bin:/bin"}) {
		t.Fatalf("WrapperEnv 应为最小集: %v", cmd.Env)
	}
	if cmd.WaitDelay == 0 {
		t.Fatal("WaitDelay 未设置")
	}
	if got := runner.Policy(); got.Backend != "seatbelt" || got.Network != "allow" {
		t.Fatalf("Policy() = %+v", got)
	}
}

func TestSeatbeltEnforcesPrivateTmpPermissions(t *testing.T) {
	root := t.TempDir()
	tmpDir := root + "/.tmp"
	if err := os.Mkdir(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSeatbeltRunner("/usr/bin/sandbox-exec", root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf(".tmp mode = %o, want 700", got)
	}
}

func TestSeatbeltProfileInjectionGuard(t *testing.T) {
	// 目录名含引号：构造期直接拒绝（§6.2 纵深防御）。
	evil := t.TempDir() + "/ev\"il"
	if err := mkdirAll(evil); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := NewSeatbeltRunner("/usr/bin/sandbox-exec", evil); err == nil {
		t.Fatal("含注入字符的路径应被拒绝")
	}
}

func TestSeatbeltProfileIsFixedAllow(t *testing.T) {
	profile := seatbeltProfile()
	if !strings.Contains(profile, "network-outbound") {
		t.Fatal("固定 profile 应允许网络")
	}
	if strings.Contains(profile, `param "RO_`) {
		t.Fatal("固定 profile 不应包含扩展只读挂载参数")
	}
}

func TestNewRunnerSelectsFixedBackend(t *testing.T) {
	root := t.TempDir()
	lookPath := func(name string) (string, error) { return "/resolved/" + name, nil }
	tests := []struct {
		goos    string
		backend string
	}{
		{goos: "darwin", backend: "seatbelt"},
		{goos: "linux", backend: "bubblewrap"},
		{goos: "windows", backend: "host"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			runner, err := newRunnerForOS(tt.goos, root, lookPath)
			if err != nil {
				t.Fatalf("newRunnerForOS() error = %v", err)
			}
			if got := runner.Policy(); got.Backend != tt.backend || got.Network != "allow" {
				t.Fatalf("Policy() = %+v", got)
			}
		})
	}
}

func TestNewRunnerDoesNotFallbackToHost(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("missing") }
	for _, goos := range []string{"darwin", "linux"} {
		if _, err := newRunnerForOS(goos, t.TempDir(), missing); err == nil {
			t.Fatalf("%s 缺少沙箱后端时不应降级 Host", goos)
		}
	}
}

func TestNewRunnerRejectsUnknownOS(t *testing.T) {
	if _, err := newRunnerForOS("plan9", t.TempDir(), nil); err == nil {
		t.Fatal("未知操作系统应返回错误")
	}
}
