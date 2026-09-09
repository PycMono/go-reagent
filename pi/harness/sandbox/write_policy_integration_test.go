package sandbox

import (
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func restrictedPolicy(t *testing.T) *workspacepolicy.Normalized {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	n, e := workspacepolicy.Normalize(root, workspacepolicy.Policy{WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{"scratch space", ".tmp"}})
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func TestRestrictedSeatbelt(t *testing.T) {
	n := restrictedPolicy(t)
	r, e := NewSeatbeltRunnerWithPolicy("sandbox-exec", n.Root(), n)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.profile, `(allow file-write* (subpath (param "WORKSPACE_ROOT")))`) {
		t.Fatal("root writable")
	}
	if !strings.Contains(strings.Join(r.dArgs, " "), "WRITE_ROOT_1="+filepath.Join(n.Root(), "scratch space")) {
		t.Fatal(r.dArgs)
	}
	if r.Policy().TmpDir != filepath.Join(n.Root(), ".tmp") {
		t.Fatal(r.Policy())
	}
}
func TestRestrictedBubblewrap(t *testing.T) {
	n := restrictedPolicy(t)
	r, e := NewBubblewrapRunnerWithPolicy("bwrap", n.Root(), n)
	if e != nil {
		t.Fatal(e)
	}
	a := strings.Join(r.baseArgv(CommandSpec{}, n.Root()), "|")
	root := "--ro-bind|" + n.Root() + "|" + n.Root()
	prefix := "--bind|" + filepath.Join(n.Root(), "scratch space")
	if strings.Index(a, root) < 0 || strings.Index(a, prefix) < strings.Index(a, root) {
		t.Fatal(a)
	}
	if r.Policy().TmpDir != filepath.Join(n.Root(), ".tmp") {
		t.Fatal(r.Policy())
	}
}
func TestPrepareRejectsChangedPrefix(t *testing.T) {
	n := restrictedPolicy(t)
	if e := os.Mkdir(filepath.Join(n.Root(), "other"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("other", filepath.Join(n.Root(), "scratch space")); e != nil {
		t.Fatal(e)
	}
	if e := prepareWritableDirectories(n); e == nil {
		t.Fatal("accepted symlink")
	}
}

func TestNativeRestrictedWrites(t *testing.T) {
	if os.Getenv("RUN_WORKSPACE_SANDBOX_INTEGRATION") != "1" {
		t.Skip("native suite not requested")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("requires native OS sandbox")
	}
	n := restrictedPolicy(t)
	bundle := filepath.Join(n.Root(), "AGENTS.md")
	if e := os.WriteFile(bundle, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	r, e := NewRunnerWithPolicy(n.Root(), n, true)
	if e != nil {
		t.Fatal(e)
	}
	spec := CommandSpec{WorkDir: n.Root(), PayloadEnv: Env(n.Root(), r.Policy().TmpDir)}
	run := func(command string, wantSuccess bool) {
		t.Helper()
		cmd, e := r.BuildShell(command, spec)
		if e != nil {
			t.Fatal(e)
		}
		out, e := cmd.CombinedOutput()
		if (e == nil) != wantSuccess {
			t.Fatalf("command %q: %v %s", command, e, out)
		}
	}
	run(`printf ok > 'scratch space/result'; printf tmp > "$TMPDIR/result"`, true)
	for p, want := range map[string]string{"scratch space/result": "ok", ".tmp/result": "tmp"} {
		b, e := os.ReadFile(filepath.Join(n.Root(), p))
		if e != nil || string(b) != want {
			t.Fatalf("%s = %q: %v", p, b, e)
		}
	}
	if runtime.GOOS == "linux" {
		run(`printf alias > /tmp/alias; test "$(cat "$TMPDIR/alias")" = alias`, true)
	}
	if e := os.Symlink("../AGENTS.md", filepath.Join(n.Root(), "scratch space/link")); e != nil {
		t.Fatal(e)
	}
	other := t.TempDir()
	if e := os.WriteFile(filepath.Join(other, "secret"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, attack := range []string{`printf changed > AGENTS.md`, `rm AGENTS.md`, `mv AGENTS.md 'scratch space/moved'`, `touch new-bundle`, `mkdir scratch-evil`, `printf changed > 'scratch space/link'`, `mv 'scratch space' renamed`, `mv .tmp renamed`, `ln AGENTS.md 'scratch space/hardlink'`} {
		run(attack, false)
		b, e := os.ReadFile(bundle)
		if e != nil || string(b) != "original" {
			t.Fatalf("Bundle changed: %q %v", b, e)
		}
	}
	cmd, e := r.BuildArgv([]string{"/bin/cat", filepath.Join(other, "secret")}, spec)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := cmd.CombinedOutput(); e == nil {
		t.Fatalf("other workspace readable: %s", out)
	}
	cmd, e = r.BuildArgv([]string{"/bin/rm", bundle}, spec)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := cmd.CombinedOutput(); e == nil {
		t.Fatalf("direct argv write accepted: %s", out)
	}
	if e := os.Link(bundle, filepath.Join(n.Root(), "scratch space/preexisting-hardlink")); e != nil {
		t.Fatal(e)
	}
	if _, e := r.BuildShell(`printf changed > 'scratch space/preexisting-hardlink'`, spec); e == nil {
		t.Fatal("preexisting hardlink accepted")
	}
}

func TestRestrictedPreparationAndAllCompatibility(t *testing.T) {
	root := t.TempDir()
	n, e := workspacepolicy.Normalize(root, workspacepolicy.Policy{WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{"nested/scratch", ".tmp"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = prepareWritableDirectories(n); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(root, ".tmp"))
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("tmp mode: %v %v", info, e)
	}
	if _, e = os.Stat(filepath.Join(root, "scratch")); !os.IsNotExist(e) {
		t.Fatal("created unapproved directory")
	}
	all, e := workspacepolicy.Normalize(root, workspacepolicy.Policy{WriteMode: workspacepolicy.All})
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewSeatbeltRunnerWithPolicy("sandbox-exec", root, all)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(s.profile, `(allow file-write* (subpath (param "WORKSPACE_ROOT")))`) {
		t.Fatal("all is not writable")
	}
	b, e := NewBubblewrapRunnerWithPolicy("bwrap", root, all)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(b.baseArgv(CommandSpec{}, root), "|"), "--bind|"+all.Root()+"|"+all.Root()) {
		t.Fatal("all root bind missing")
	}
	legacy, e := NewBubblewrapRunner("bwrap", root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = legacy.BuildShell("true", CommandSpec{WorkDir: root, PayloadEnv: Env(all.Root(), "/tmp")}); e != nil {
		t.Fatalf("legacy env: %v", e)
	}
}
