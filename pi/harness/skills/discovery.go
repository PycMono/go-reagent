package skills

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const maxSkillFileBytes = 256 * 1024

// ErrInvalidWorkspace 表示 Skill 工作区无法解析、打开或扫描。
var ErrInvalidWorkspace = errors.New("skills: invalid workspace")

type environment struct {
	goos      string
	envLookup func(name string) bool
	binLookup func(name string) bool
}

// defaultEnvironment 创建基于当前进程的 Skill 运行环境，
// 用于检查当前操作系统、环境变量和可执行文件是否满足 Skill 的使用条件。
func defaultEnvironment() environment {
	return environment{
		goos: runtime.GOOS,
		envLookup: func(name string) bool {
			_, ok := os.LookupEnv(name)
			return ok
		},
		binLookup: func(name string) bool {
			_, err := exec.LookPath(name)
			return err == nil
		},
	}
}

type skillSourceSpec struct {
	Source    Source
	Directory string
}

var skillSources = []skillSourceSpec{
	{Source: SourceWorkspace, Directory: "skills"},
	{Source: SourceAgents, Directory: ".agents/skills"},
	{Source: SourceClaw, Directory: ".claw/skills"},
}

// Discover 从指定工作区约定的 Skill 目录中扫描可用的 SKILL.md，
// 过滤格式无效或不满足运行环境要求的 Skill，再按来源优先级处理同名项，
// 最终返回排序稳定的 Skill 摘要和诊断信息快照。
func Discover(workDir string) (*Snapshot, error) {
	return discover(workDir, defaultEnvironment())
}

// discover 使用指定运行环境发现 Skill，供包内测试注入稳定的环境条件。
func discover(workDir string, env environment) (*Snapshot, error) {
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("%w: 解析技能工作区失败: %w", ErrInvalidWorkspace, err)
	}
	root, err := os.OpenRoot(absoluteWorkDir)
	if err != nil {
		return nil, fmt.Errorf("%w: 打开技能工作区失败: %w", ErrInvalidWorkspace, err)
	}
	defer root.Close()

	bySource := make(map[Source][]Summary, len(skillSources))
	diagnostics := make([]Diagnostic, 0)
	for _, source := range skillSources {
		candidates, sourceDiagnostics, err := discoverSkillSource(root, source, env)
		if err != nil {
			return nil, fmt.Errorf("%w: 发现 Agent Skills 失败: %w", ErrInvalidWorkspace, err)
		}
		bySource[source.Source] = candidates
		diagnostics = append(diagnostics, sourceDiagnostics...)
	}

	winners, mergeDiagnostics := mergeDiscoveredSkills(bySource)
	diagnostics = append(diagnostics, mergeDiagnostics...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return newSnapshot(winners, diagnostics), nil
}

// discoverSkillSource 递归扫描一个 Skill 来源目录，读取并解析其中的 SKILL.md。
// 它会跳过软链接和非普通文件，拒绝过大的文件和不符合当前环境要求的 Skill，
// 并将单个 Skill 的问题记录为诊断，避免影响同一来源中的其他有效 Skill。
func discoverSkillSource(
	root *os.Root,
	source skillSourceSpec,
	env environment,
) ([]Summary, []Diagnostic, error) {
	info, err := root.Stat(filepath.FromSlash(source.Directory))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("检查技能目录 %s 失败: %w", source.Directory, err)
	}
	if !info.IsDir() {
		return nil, nil, nil
	}

	candidates := make([]Summary, 0)
	diagnostics := make([]Diagnostic, 0)
	err = fs.WalkDir(root.FS(), source.Directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}

		summary, diagnostic, accepted := inspectSkillFile(root, path, source.Source, env)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
		if !accepted {
			return nil
		}
		candidates = append(candidates, summary)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("扫描技能目录 %s 失败: %w", source.Directory, err)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Location < candidates[j].Location
	})
	return candidates, diagnostics, nil
}

// inspectSkillFile 检查、读取并解析一个 SKILL.md，返回可合并的摘要或诊断信息。
func inspectSkillFile(root *os.Root, path string, source Source, env environment) (Summary, *Diagnostic, bool) {
	location := filepath.ToSlash(path)
	entryInfo, err := root.Lstat(filepath.FromSlash(path))
	if err != nil {
		diagnostic := skillDiagnostic(location, SeverityWarning, "skill_file_unreadable", "无法检查 SKILL.md")
		return Summary{}, &diagnostic, false
	}
	if !entryInfo.Mode().IsRegular() {
		return Summary{}, nil, false
	}
	if entryInfo.Size() > maxSkillFileBytes {
		diagnostic := skillDiagnostic(location, SeverityWarning, "skill_file_too_large", "SKILL.md 超过 256 KiB")
		return Summary{}, &diagnostic, false
	}

	content, err := readLimitedSkillFile(root, filepath.FromSlash(path))
	if err != nil {
		diagnostic := skillDiagnostic(location, SeverityWarning, "skill_file_unreadable", "无法读取 SKILL.md")
		return Summary{}, &diagnostic, false
	}
	if len(content) > maxSkillFileBytes {
		diagnostic := skillDiagnostic(location, SeverityWarning, "skill_file_too_large", "SKILL.md 超过 256 KiB")
		return Summary{}, &diagnostic, false
	}

	parsed, err := parseSkillMD(content)
	if err != nil {
		var parseErr *skillParseError
		diagnostic := skillDiagnostic(location, SeverityWarning, "skill_frontmatter_invalid", "无法解析 SKILL.md")
		if errors.As(err, &parseErr) {
			diagnostic = skillDiagnostic(location, SeverityWarning, parseErr.Code, parseErr.Message)
		}
		return Summary{}, &diagnostic, false
	}
	if diagnostic, eligible := skillEligibility(location, parsed, env); !eligible {
		return Summary{}, &diagnostic, false
	}

	digest := sha256.Sum256(content)
	return Summary{
		Name:        parsed.Name,
		Description: parsed.Description,
		Location:    location,
		Version:     fmt.Sprintf("sha256:%x", digest[:8]),
		Source:      source,
	}, nil, true
}

// readLimitedSkillFile 从工作区根目录安全读取一个普通 SKILL.md，
// 最多读取 maxSkillFileBytes+1 字节，使调用方能够判断文件是否超过大小限制。
func readLimitedSkillFile(root *os.Root, path string) ([]byte, error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("SKILL.md 不是普通文件")
	}
	return io.ReadAll(io.LimitReader(file, maxSkillFileBytes+1))
}

// skillEligibility 检查 Skill 是否允许模型调用，并验证当前操作系统、
// 必需的二进制命令和环境变量是否满足要求；不满足时返回对应诊断和 false。
func skillEligibility(path string, skill parsedSkill, env environment) (Diagnostic, bool) {
	if skill.DisableModelInvocation {
		return skillDiagnostic(path, SeverityInfo, "skill_model_invocation_disabled",
			"Skill 已禁止模型调用"), false
	}
	goos := env.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		goos = "win32"
	}
	if len(skill.OS) > 0 && !containsString(skill.OS, goos) {
		return skillDiagnostic(path, SeverityInfo, "skill_os_ineligible",
			fmt.Sprintf("Skill 不支持当前操作系统 %s", goos)), false
	}
	for _, binary := range skill.RequiredBins {
		if !env.binLookup(binary) {
			return skillDiagnostic(path, SeverityInfo, "skill_missing_binary",
				fmt.Sprintf("Skill 缺少所需二进制 %s", binary)), false
		}
	}
	for _, variable := range skill.RequiredEnv {
		if !env.envLookup(variable) {
			return skillDiagnostic(path, SeverityInfo, "skill_missing_environment",
				fmt.Sprintf("Skill 缺少所需环境变量 %s", variable)), false
		}
	}
	return Diagnostic{}, true
}

// containsString 在清理列表元素两侧空白后，检查其中是否存在指定字符串。
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

// mergeDiscoveredSkills 按 skillSources 的顺序合并不同来源发现的 Skill。
// 高优先级来源会覆盖低优先级的同名 Skill；同一来源出现重复名称时，
// 该名称会被标记为冲突并阻止低优先级来源中的同名 Skill 补位。
func mergeDiscoveredSkills(bySource map[Source][]Summary) ([]Summary, []Diagnostic) {
	winners := make(map[string]Summary)
	blocked := make(map[string]bool)
	diagnostics := make([]Diagnostic, 0)
	for _, source := range skillSources {
		groups := make(map[string][]Summary)
		for _, candidate := range bySource[source.Source] {
			groups[candidate.Name] = append(groups[candidate.Name], candidate)
		}
		names := make([]string, 0, len(groups))
		for name := range groups {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			group := groups[name]
			duplicate := len(group) > 1
			if duplicate {
				for _, candidate := range group {
					diagnostics = append(diagnostics, skillDiagnostic(candidate.Location,
						SeverityWarning, "skill_duplicate_name", "同一来源存在重复 Skill name"))
				}
			}
			_, exists := winners[name]
			if blocked[name] || exists {
				for _, candidate := range group {
					diagnostics = append(diagnostics, skillDiagnostic(candidate.Location,
						SeverityInfo, "skill_shadowed", "Skill 被更高优先级来源覆盖"))
				}
				continue
			}
			if duplicate {
				blocked[name] = true
				continue
			}
			winners[name] = group[0]
		}
	}

	result := make([]Summary, 0, len(winners))
	for _, summary := range winners {
		result = append(result, summary)
	}
	return result, diagnostics
}

// skillDiagnostic 创建路径格式统一的 Skill 诊断信息，供发现流程记录跳过原因。
func skillDiagnostic(path string, severity Severity, code string, message string) Diagnostic {
	return Diagnostic{Path: filepath.ToSlash(path), Severity: severity, Code: code, Message: message}
}
