package context

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

type SkillEnvironment struct {
	GOOS      string
	EnvLookup func(name string) bool
	BinLookup func(name string) bool
}

// DefaultSkillEnvironment 创建基于当前进程的 Skill 运行环境，
// 用于检查当前操作系统、环境变量和可执行文件是否满足 Skill 的使用条件。
func DefaultSkillEnvironment() SkillEnvironment {
	return SkillEnvironment{
		GOOS: runtime.GOOS,
		EnvLookup: func(name string) bool {
			_, ok := os.LookupEnv(name)
			return ok
		},
		BinLookup: func(name string) bool {
			_, err := exec.LookPath(name)
			return err == nil
		},
	}
}

type skillSourceSpec struct {
	Source    SkillSource
	Directory string
}

var skillSources = []skillSourceSpec{
	{Source: SkillSourceWorkspace, Directory: "skills"},
	{Source: SkillSourceAgents, Directory: ".agents/skills"},
	{Source: SkillSourceClaw, Directory: ".claw/skills"},
}

type discoveredSkill struct {
	Summary SkillSummary
}

// Discover 从工作区约定的 Skill 目录中扫描可用的 SKILL.md，
// 过滤格式无效或不满足运行环境要求的 Skill，再按来源优先级处理同名项，
// 最终返回排序稳定的 Skill 摘要和诊断信息快照。
func (s *SkillLoader) Discover(env SkillEnvironment) (*SkillSnapshot, error) {
	absoluteWorkDir, err := filepath.Abs(s.workDir)
	if err != nil {
		return nil, fmt.Errorf("解析技能工作区失败: %w", err)
	}
	root, err := os.OpenRoot(absoluteWorkDir)
	if err != nil {
		return nil, fmt.Errorf("打开技能工作区失败: %w", err)
	}
	defer root.Close()

	bySource := make(map[SkillSource][]discoveredSkill, len(skillSources))
	diagnostics := make([]SkillDiagnostic, 0)
	for _, source := range skillSources {
		candidates, sourceDiagnostics, err := discoverSkillSource(root, source, env)
		if err != nil {
			return nil, err
		}
		bySource[source.Source] = candidates
		diagnostics = append(diagnostics, sourceDiagnostics...)
	}

	winners, mergeDiagnostics := mergeDiscoveredSkills(bySource)
	diagnostics = append(diagnostics, mergeDiagnostics...)
	sort.Slice(winners, func(i, j int) bool {
		if winners[i].Name == winners[j].Name {
			return winners[i].Location < winners[j].Location
		}
		return winners[i].Name < winners[j].Name
	})
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return newSkillSnapshot(winners, diagnostics), nil
}

// discoverSkillSource 递归扫描一个 Skill 来源目录，读取并解析其中的 SKILL.md。
// 它会跳过软链接和非普通文件，拒绝过大的文件和不符合当前环境要求的 Skill，
// 并将单个 Skill 的问题记录为诊断，避免影响同一来源中的其他有效 Skill。
func discoverSkillSource(
	root *os.Root,
	source skillSourceSpec,
	env SkillEnvironment,
) ([]discoveredSkill, []SkillDiagnostic, error) {
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

	candidates := make([]discoveredSkill, 0)
	diagnostics := make([]SkillDiagnostic, 0)
	err = fs.WalkDir(root.FS(), source.Directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}

		location := filepath.ToSlash(path)
		entryInfo, err := root.Lstat(filepath.FromSlash(path))
		if err != nil || !entryInfo.Mode().IsRegular() {
			return nil
		}
		if entryInfo.Size() > maxSkillFileBytes {
			diagnostics = append(diagnostics, skillDiagnostic(location, DiagnosticSeverityWarning,
				"skill_file_too_large", "SKILL.md 超过 256 KiB"))
			return nil
		}

		content, err := readLimitedSkillFile(root, filepath.FromSlash(path))
		if err != nil {
			diagnostics = append(diagnostics, skillDiagnostic(location, DiagnosticSeverityWarning,
				"skill_file_unreadable", "无法读取 SKILL.md"))
			return nil
		}
		if len(content) > maxSkillFileBytes {
			diagnostics = append(diagnostics, skillDiagnostic(location, DiagnosticSeverityWarning,
				"skill_file_too_large", "SKILL.md 超过 256 KiB"))
			return nil
		}

		parsed, err := parseSkillMD(content)
		if err != nil {
			var parseErr *skillParseError
			if errors.As(err, &parseErr) {
				diagnostics = append(diagnostics, skillDiagnostic(location, DiagnosticSeverityWarning,
					parseErr.Code, parseErr.Message))
			} else {
				diagnostics = append(diagnostics, skillDiagnostic(location, DiagnosticSeverityWarning,
					"skill_frontmatter_invalid", "无法解析 SKILL.md"))
			}
			return nil
		}
		if diagnostic, eligible := skillEligibility(location, parsed, env); !eligible {
			diagnostics = append(diagnostics, diagnostic)
			return nil
		}

		digest := sha256.Sum256(content)
		candidates = append(candidates, discoveredSkill{
			Summary: SkillSummary{
				Name:        parsed.Name,
				Description: parsed.Description,
				Location:    location,
				Version:     fmt.Sprintf("sha256:%x", digest[:8]),
				Source:      source.Source,
			},
		})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("扫描技能目录 %s 失败: %w", source.Directory, err)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Summary.Location < candidates[j].Summary.Location
	})
	return candidates, diagnostics, nil
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
func skillEligibility(path string, skill parsedSkill, env SkillEnvironment) (SkillDiagnostic, bool) {
	if skill.DisableModelInvocation {
		return skillDiagnostic(path, DiagnosticSeverityInfo, "skill_model_invocation_disabled",
			"Skill 已禁止模型调用"), false
	}
	goos := env.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		goos = "win32"
	}
	if len(skill.OS) > 0 && !containsString(skill.OS, goos) {
		return skillDiagnostic(path, DiagnosticSeverityInfo, "skill_os_ineligible",
			fmt.Sprintf("Skill 不支持当前操作系统 %s", goos)), false
	}
	for _, binary := range skill.RequiredBins {
		if env.BinLookup == nil || !env.BinLookup(binary) {
			return skillDiagnostic(path, DiagnosticSeverityInfo, "skill_missing_binary",
				fmt.Sprintf("Skill 缺少所需二进制 %s", binary)), false
		}
	}
	for _, variable := range skill.RequiredEnv {
		if env.EnvLookup == nil || !env.EnvLookup(variable) {
			return skillDiagnostic(path, DiagnosticSeverityInfo, "skill_missing_environment",
				fmt.Sprintf("Skill 缺少所需环境变量 %s", variable)), false
		}
	}
	return SkillDiagnostic{}, true
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
func mergeDiscoveredSkills(bySource map[SkillSource][]discoveredSkill) ([]SkillSummary, []SkillDiagnostic) {
	winners := make(map[string]SkillSummary)
	blocked := make(map[string]bool)
	diagnostics := make([]SkillDiagnostic, 0)
	for _, source := range skillSources {
		groups := make(map[string][]discoveredSkill)
		for _, candidate := range bySource[source.Source] {
			groups[candidate.Summary.Name] = append(groups[candidate.Summary.Name], candidate)
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
					diagnostics = append(diagnostics, skillDiagnostic(candidate.Summary.Location,
						DiagnosticSeverityWarning, "skill_duplicate_name", "同一来源存在重复 Skill name"))
				}
			}
			if blocked[name] || winners[name].Name != "" {
				for _, candidate := range group {
					diagnostics = append(diagnostics, skillDiagnostic(candidate.Summary.Location,
						DiagnosticSeverityInfo, "skill_shadowed", "Skill 被更高优先级来源覆盖"))
				}
				continue
			}
			if duplicate {
				blocked[name] = true
				continue
			}
			winners[name] = group[0].Summary
		}
	}

	result := make([]SkillSummary, 0, len(winners))
	for _, summary := range winners {
		result = append(result, summary)
	}
	return result, diagnostics
}

// skillDiagnostic 创建路径格式统一的 Skill 诊断信息，供发现流程记录跳过原因。
func skillDiagnostic(path string, severity DiagnosticSeverity, code string, message string) SkillDiagnostic {
	return SkillDiagnostic{Path: filepath.ToSlash(path), Severity: severity, Code: code, Message: message}
}
