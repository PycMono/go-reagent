package agentprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
)

func TestNewCatalogLoadsValidatedImmutableProfiles(t *testing.T) {
	workspace := writeCatalogFixture(t, `version: 1
default_profile: general
profiles:
  - code: writing
    name: 写作助手
    description: 文案、改写、总结和平台内容
    icon: pen-line
    order: 20
    selectable: false
    welcome: 想写点什么？
    starters:
      - title: 写一篇小红书
        prompt: 帮我写一篇小红书：
  - code: general
    name: 通用助手
    description: 日常问答、分析与决策
    icon: message-circle
    order: 10
    selectable: true
    welcome: 今天想一起完成什么？
    starters: []
`)
	writeProfile(t, workspace, "general", "# 通用助手\n\n处理通用问题。", "", "")
	writeProfile(t, workspace, "writing", "# 写作助手\n\n你擅长写作。", "content-writing", "Use when the user asks for content writing.")

	catalog, err := NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultCode() != "general" {
		t.Fatalf("DefaultCode() = %q", catalog.DefaultCode())
	}
	got := catalog.List()
	if len(got) != 2 || got[0].Code != "general" || got[1].Code != "writing" {
		t.Fatalf("profiles = %#v", got)
	}
	profile, ok := catalog.Find(" writing ")
	if !ok || profile.Selectable || !strings.Contains(profile.Instructions, "擅长写作") ||
		len(profile.Skills) != 1 || profile.Skills[0].Name != "content-writing" ||
		profile.Skills[0].Location != "profiles/writing/skills/content-writing/SKILL.md" {
		t.Fatalf("writing profile = %#v, found=%v", profile, ok)
	}

	got[0].Name = "mutated"
	profile.Starters[0].Title = "mutated"
	profile.Skills[0].Name = "mutated"
	again, _ := catalog.Find("writing")
	general, _ := catalog.Find("general")
	if general.Name == "mutated" || again.Starters[0].Title == "mutated" || again.Skills[0].Name == "mutated" {
		t.Fatal("catalog exposed mutable snapshot")
	}
}

func TestNewCatalogRejectsInvalidMetadataAndFiles(t *testing.T) {
	tests := []struct {
		name       string
		catalogYML string
		prepare    func(*testing.T, string)
		want       string
	}{
		{
			name: "unsupported version",
			catalogYML: `version: 2
default_profile: general
profiles: []
`,
			want: "version",
		},
		{
			name: "duplicate code",
			catalogYML: oneProfileCatalog("general") + `  - code: general
    name: 重复
    description: 重复项
    icon: message-circle
    order: 20
    selectable: true
    welcome: 重复
    starters: []
`,
			prepare: func(t *testing.T, workspace string) {
				writeProfile(t, workspace, "general", "general", "", "")
			},
			want: "duplicate",
		},
		{
			name:       "invalid code",
			catalogYML: oneProfileCatalog("Bad_Code"),
			want:       "code",
		},
		{
			name:       "invalid icon",
			catalogYML: strings.Replace(oneProfileCatalog("general"), "message-circle", "<svg>bad</svg>", 1),
			want:       "icon",
		},
		{
			name:       "missing agents",
			catalogYML: oneProfileCatalog("general"),
			prepare: func(t *testing.T, workspace string) {
				if err := os.MkdirAll(filepath.Join(workspace, "profiles", "general"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: "AGENTS.md",
		},
		{
			name:       "invalid skill",
			catalogYML: oneProfileCatalog("general"),
			prepare: func(t *testing.T, workspace string) {
				writeProfile(t, workspace, "general", "general", "broken", "")
			},
			want: "Skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := writeCatalogFixture(t, tt.catalogYML)
			if tt.prepare != nil {
				tt.prepare(t, workspace)
			}
			_, err := NewCatalog(pi.WorkDir(workspace))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("NewCatalog() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewCatalogRejectsSymlinkedProfileDirectory(t *testing.T) {
	workspace := writeCatalogFixture(t, oneProfileCatalog("general"))
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "AGENTS.md"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "profiles", "general")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := NewCatalog(pi.WorkDir(workspace))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "profile") {
		t.Fatalf("NewCatalog() error = %v, want profile containment error", err)
	}
}

func writeCatalogFixture(t *testing.T, catalog string) string {
	t.Helper()
	workspace := t.TempDir()
	profiles := filepath.Join(workspace, "profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "catalog.yaml"), []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func writeProfile(t *testing.T, workspace, code, instructions, skillName, skillDescription string) {
	t.Helper()
	root := filepath.Join(workspace, "profiles", code)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(instructions), 0o600); err != nil {
		t.Fatal(err)
	}
	if skillName == "" {
		return
	}
	skillDir := filepath.Join(root, "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + skillName + "\ndescription: " + skillDescription + "\n---\nSkill body.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func oneProfileCatalog(code string) string {
	return `version: 1
default_profile: ` + code + `
profiles:
  - code: ` + code + `
    name: 通用助手
    description: 日常问答
    icon: message-circle
    order: 10
    selectable: true
    welcome: 今天想一起完成什么？
    starters: []
`
}
