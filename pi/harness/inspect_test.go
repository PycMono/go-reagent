package harness

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/harness/skills"
)

func TestInspectWorkspaceMatchesContext(t *testing.T) {
	root := t.TempDir()
	agents := []byte("You are a test Agent.\n")
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), agents, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "skills", "example")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	skill := []byte("---\nname: example\ndescription: Example workflow\n---\nUse this workflow.\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), skill, 0o600); err != nil {
		t.Fatal(err)
	}

	before := workspaceContents(t, root)
	report, err := InspectWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(agents))
	if report.AgentInstructionsDigest != wantDigest || len(report.Skills) != 1 {
		t.Fatalf("report: %+v", report)
	}
	builder := NewContextBuilder(NewPromptComposer(root), root)
	built, err := builder.Build(context.Background(), ContextRequest{
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}},
	}, ai.ToolDefinitions{{Name: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := built.Messages[0].Content.Text()
	if err != nil || !strings.Contains(text, report.Skills[0].Location) || !strings.Contains(text, string(agents)) {
		t.Fatalf("context differs: %s %v", text, err)
	}
	if after := workspaceContents(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("inspection changed workspace:\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("inspection wrote files: %v", err)
	}
}

func TestInspectWorkspaceAndContextRejectInvalidAgents(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "missing", setup: func(*testing.T, string) {}},
		{name: "blank", setup: writeAgentsFixture([]byte(" \n\t"))},
		{name: "nul", setup: writeAgentsFixture([]byte("valid\x00invalid"))},
		{name: "invalid UTF-8", setup: writeAgentsFixture([]byte{0xff, 0xfe})},
		{name: "directory", setup: func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", setup: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "actual"), []byte("valid"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("actual", filepath.Join(root, "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "over 1 MiB", setup: writeAgentsFixture([]byte(strings.Repeat("a", 1024*1024+1)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)
			_, inspectErr := InspectWorkspace(context.Background(), root)
			if !errors.Is(inspectErr, pierrors.ErrWorkspaceInvalid) {
				t.Fatalf("InspectWorkspace() error = %v, want ErrWorkspaceInvalid", inspectErr)
			}
			builder := NewContextBuilder(NewPromptComposer(root), root)
			_, buildErr := builder.Build(context.Background(), ContextRequest{Input: contextTestUserMessage("hello")}, nil)
			if !errors.Is(buildErr, pierrors.ErrWorkspaceInvalid) {
				t.Fatalf("Build() error = %v, want ErrWorkspaceInvalid", buildErr)
			}
		})
	}
}

func TestInspectWorkspaceAndContextAcceptOneMiBAgents(t *testing.T) {
	root := t.TempDir()
	agents := []byte(strings.Repeat("a", 1024*1024))
	writeAgentsFixture(agents)(t, root)

	report, err := InspectWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("InspectWorkspace() error = %v", err)
	}
	wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(agents))
	if report.AgentInstructionsDigest != wantDigest {
		t.Fatalf("digest = %q, want %q", report.AgentInstructionsDigest, wantDigest)
	}
	builder := NewContextBuilder(NewPromptComposer(root), root)
	built, err := builder.Build(context.Background(), ContextRequest{Input: contextTestUserMessage("hello")}, nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	text, err := built.Messages[0].Content.Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, string(agents)) {
		t.Fatal("system prompt does not contain the complete 1 MiB AGENTS.md")
	}
}

func TestWorkspaceInspectionAndContextHonorCanceledContext(t *testing.T) {
	root := t.TempDir()
	writeAgentsFixture([]byte("valid\n"))(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := InspectWorkspace(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("InspectWorkspace() error = %v, want context.Canceled", err)
	}
	builder := NewContextBuilder(NewPromptComposer(root), root)
	_, err = builder.Build(ctx, ContextRequest{Input: contextTestUserMessage("hello")}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Build() error = %v, want context.Canceled", err)
	}
}

func TestInspectWorkspaceMatchesSkillDiscoveryDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeAgentsFixture([]byte("valid\n"))(t, root)
	writeSkillFixture(t, root, "skills/duplicate-a/SKILL.md", "duplicate", "first", "")
	writeSkillFixture(t, root, "skills/duplicate-b/SKILL.md", "duplicate", "second", "")
	writeSkillFixture(t, root, "skills/missing-dependency/SKILL.md", "missing-dependency", "needs binary", "metadata:\n  openclaw:\n    requires:\n      bins: [go-reagent-definitely-missing-binary]")
	largeDir := filepath.Join(root, "skills", "large")
	if err := os.MkdirAll(largeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(largeDir, "SKILL.md"), []byte(strings.Repeat("x", 256*1024+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	want, err := skills.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Skills, want.Skills()) || !reflect.DeepEqual(report.Diagnostics, want.Diagnostics()) {
		t.Fatalf("inspection differs from Discover:\nreport=%+v\ndiscover skills=%+v diagnostics=%+v", report, want.Skills(), want.Diagnostics())
	}

	builder := NewContextBuilder(NewPromptComposer(root), root)
	got, err := builder.Build(context.Background(), ContextRequest{Input: contextTestUserMessage("hello")}, ai.ToolDefinitions{{Name: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := got.Messages[0].Content.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "missing-dependency") || strings.Contains(text, "skills/large/SKILL.md") {
		t.Fatalf("context included skipped Skills: %s", text)
	}
}

func writeAgentsFixture(content []byte) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeSkillFixture(t *testing.T, root, relativePath, name, description, extra string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n%s\n---\nbody\n", name, description, extra)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func workspaceContents(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := filepath.ToSlash(relative) + "|" + info.Mode().String()
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item += fmt.Sprintf("|sha256:%x", sha256.Sum256(content))
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			item += "|" + target
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return entries
}
