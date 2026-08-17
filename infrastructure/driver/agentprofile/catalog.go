package agentprofile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	agentprofileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	agentprofilerepo "github.com/PycMono/go-reagent/domain/repository/agentprofile"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/harness/skills"
	"gopkg.in/yaml.v3"
)

const catalogVersion = 1

var (
	profileCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	allowedIcons       = map[string]struct{}{
		"message-circle": {},
		"pen-line":       {},
		"graduation-cap": {},
		"heart-pulse":    {},
		"scale":          {},
		"car-front":      {},
		"briefcase":      {},
		"baby":           {},
	}
)

type catalogFile struct {
	Version        int           `yaml:"version"`
	DefaultProfile string        `yaml:"default_profile"`
	Profiles       []profileFile `yaml:"profiles"`
}

type profileFile struct {
	Code        string        `yaml:"code"`
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Icon        string        `yaml:"icon"`
	Order       int           `yaml:"order"`
	Selectable  bool          `yaml:"selectable"`
	Welcome     string        `yaml:"welcome"`
	Starters    []starterFile `yaml:"starters"`
}

type starterFile struct {
	Title  string `yaml:"title"`
	Prompt string `yaml:"prompt"`
}

type immutableCatalog struct {
	profiles    []agentprofileentity.Profile
	byCode      map[string]agentprofileentity.Profile
	defaultCode string
}

// NewCatalog loads and validates the Profile Catalog under the chat Workspace.
func NewCatalog(workDir pi.WorkDir) (agentprofilerepo.Catalog, error) {
	workspace, err := resolveDirectory(string(workDir))
	if err != nil {
		return nil, fmt.Errorf("agent profile catalog: resolve workspace: %w", err)
	}
	profilesRoot, err := resolveDirectory(filepath.Join(workspace, "profiles"))
	if err != nil {
		return nil, fmt.Errorf("agent profile catalog: resolve profiles directory: %w", err)
	}
	if !pathWithin(workspace, profilesRoot) {
		return nil, errors.New("agent profile catalog: profiles directory escapes workspace")
	}

	configuration, err := readCatalog(filepath.Join(profilesRoot, "catalog.yaml"))
	if err != nil {
		return nil, err
	}
	if configuration.Version != catalogVersion {
		return nil, fmt.Errorf("agent profile catalog: unsupported version %d", configuration.Version)
	}
	if len(configuration.Profiles) == 0 {
		return nil, errors.New("agent profile catalog: profiles are required")
	}

	profiles := make([]agentprofileentity.Profile, 0, len(configuration.Profiles))
	byCode := make(map[string]agentprofileentity.Profile, len(configuration.Profiles))
	for index, raw := range configuration.Profiles {
		profile, loadErr := loadProfile(workspace, profilesRoot, raw)
		if loadErr != nil {
			return nil, fmt.Errorf("agent profile catalog: profile %d: %w", index, loadErr)
		}
		if _, exists := byCode[profile.Code]; exists {
			return nil, fmt.Errorf("agent profile catalog: duplicate code %q", profile.Code)
		}
		profiles = append(profiles, profile)
		byCode[profile.Code] = profile
	}

	defaultCode := strings.TrimSpace(configuration.DefaultProfile)
	defaultProfile, found := byCode[defaultCode]
	if !found {
		return nil, fmt.Errorf("agent profile catalog: default profile %q does not exist", defaultCode)
	}
	if !defaultProfile.Selectable {
		return nil, fmt.Errorf("agent profile catalog: default profile %q is not selectable", defaultCode)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Order == profiles[j].Order {
			return profiles[i].Code < profiles[j].Code
		}
		return profiles[i].Order < profiles[j].Order
	})
	return &immutableCatalog{profiles: cloneProfiles(profiles), byCode: cloneProfileMap(byCode), defaultCode: defaultCode}, nil
}

func (catalog *immutableCatalog) List() []agentprofileentity.Profile {
	if catalog == nil {
		return nil
	}
	return cloneProfiles(catalog.profiles)
}

func (catalog *immutableCatalog) Find(code string) (agentprofileentity.Profile, bool) {
	if catalog == nil {
		return agentprofileentity.Profile{}, false
	}
	profile, found := catalog.byCode[strings.TrimSpace(code)]
	if !found {
		return agentprofileentity.Profile{}, false
	}
	return cloneProfile(profile), true
}

func (catalog *immutableCatalog) DefaultCode() string {
	if catalog == nil {
		return ""
	}
	return catalog.defaultCode
}

func readCatalog(path string) (catalogFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return catalogFile{}, fmt.Errorf("agent profile catalog: read catalog.yaml: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var configuration catalogFile
	if err := decoder.Decode(&configuration); err != nil {
		return catalogFile{}, fmt.Errorf("agent profile catalog: decode catalog.yaml: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return catalogFile{}, errors.New("agent profile catalog: catalog.yaml contains multiple documents")
		}
		return catalogFile{}, fmt.Errorf("agent profile catalog: decode trailing content: %w", err)
	}
	return configuration, nil
}

func loadProfile(workspace, profilesRoot string, raw profileFile) (agentprofileentity.Profile, error) {
	code := strings.TrimSpace(raw.Code)
	name := strings.TrimSpace(raw.Name)
	description := strings.TrimSpace(raw.Description)
	icon := strings.TrimSpace(raw.Icon)
	welcome := strings.TrimSpace(raw.Welcome)
	if code == "" || utf8.RuneCountInString(code) > 64 || !profileCodePattern.MatchString(code) {
		return agentprofileentity.Profile{}, fmt.Errorf("invalid code %q", code)
	}
	if name == "" || utf8.RuneCountInString(name) > 32 {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q name must contain 1 to 32 characters", code)
	}
	if description == "" || utf8.RuneCountInString(description) > 120 {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q description must contain 1 to 120 characters", code)
	}
	if _, allowed := allowedIcons[icon]; !allowed {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q icon %q is not allowed", code, icon)
	}
	if welcome == "" || utf8.RuneCountInString(welcome) > 120 {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q welcome must contain 1 to 120 characters", code)
	}
	if len(raw.Starters) > 4 {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q has more than 4 starters", code)
	}

	starters := make([]agentprofileentity.Starter, len(raw.Starters))
	for index, starter := range raw.Starters {
		title := strings.TrimSpace(starter.Title)
		prompt := strings.TrimSpace(starter.Prompt)
		if title == "" || utf8.RuneCountInString(title) > 60 || prompt == "" || utf8.RuneCountInString(prompt) > 1000 {
			return agentprofileentity.Profile{}, fmt.Errorf("profile %q starter %d is invalid", code, index)
		}
		starters[index] = agentprofileentity.Starter{Title: title, Prompt: prompt}
	}

	profileRoot, err := resolveDirectory(filepath.Join(profilesRoot, code))
	if err != nil {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q directory: %w", code, err)
	}
	if !pathWithin(profilesRoot, profileRoot) || profileRoot != filepath.Join(profilesRoot, code) {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q directory escapes profiles root", code)
	}
	instructions, err := readInstructions(profileRoot)
	if err != nil {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q: %w", code, err)
	}
	snapshot, err := skills.Discover(profileRoot)
	if err != nil {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q Skill discovery: %w", code, err)
	}
	if diagnostics := snapshot.Diagnostics(); len(diagnostics) > 0 {
		return agentprofileentity.Profile{}, fmt.Errorf("profile %q Skill diagnostics: %s", code, diagnostics[0].Code)
	}
	profileSkills := make([]agentprofileentity.Skill, 0, len(snapshot.Skills()))
	for _, summary := range snapshot.Skills() {
		absoluteSkill := filepath.Join(profileRoot, filepath.FromSlash(summary.Location))
		resolvedSkill, resolveErr := filepath.EvalSymlinks(absoluteSkill)
		if resolveErr != nil || !pathWithin(profileRoot, resolvedSkill) {
			return agentprofileentity.Profile{}, fmt.Errorf("profile %q Skill path is invalid", code)
		}
		location, relativeErr := filepath.Rel(workspace, resolvedSkill)
		if relativeErr != nil || strings.HasPrefix(location, "..") {
			return agentprofileentity.Profile{}, fmt.Errorf("profile %q Skill path escapes workspace", code)
		}
		profileSkills = append(profileSkills, agentprofileentity.Skill{
			Name: summary.Name, Description: summary.Description, Location: filepath.ToSlash(location),
		})
	}

	return agentprofileentity.Profile{
		Code: code, Name: name, Description: description, Icon: icon, Order: raw.Order,
		Selectable: raw.Selectable, Welcome: welcome, Starters: starters,
		Instructions: instructions, Skills: profileSkills,
	}, nil
}

func readInstructions(profileRoot string) (string, error) {
	path := filepath.Join(profileRoot, "AGENTS.md")
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("read AGENTS.md: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("AGENTS.md must be a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read AGENTS.md: %w", err)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return "", errors.New("AGENTS.md must be valid UTF-8 text")
	}
	instructions := strings.TrimSpace(string(content))
	if instructions == "" {
		return "", errors.New("AGENTS.md must not be empty")
	}
	return instructions, nil
}

func resolveDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cloneProfile(profile agentprofileentity.Profile) agentprofileentity.Profile {
	profile.Starters = append([]agentprofileentity.Starter(nil), profile.Starters...)
	profile.Skills = append([]agentprofileentity.Skill(nil), profile.Skills...)
	return profile
}

func cloneProfiles(profiles []agentprofileentity.Profile) []agentprofileentity.Profile {
	result := make([]agentprofileentity.Profile, len(profiles))
	for index := range profiles {
		result[index] = cloneProfile(profiles[index])
	}
	return result
}

func cloneProfileMap(profiles map[string]agentprofileentity.Profile) map[string]agentprofileentity.Profile {
	result := make(map[string]agentprofileentity.Profile, len(profiles))
	for code, profile := range profiles {
		result[code] = cloneProfile(profile)
	}
	return result
}

var _ agentprofilerepo.Catalog = (*immutableCatalog)(nil)
