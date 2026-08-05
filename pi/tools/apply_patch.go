package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type ApplyPatchTool struct {
	workspace *Workspace
}

var _ agent.Tool = (*ApplyPatchTool)(nil)

func NewApplyPatchTool(workspace *Workspace) *ApplyPatchTool {
	return &ApplyPatchTool{workspace: workspace}
}

type ApplyPatchDetails struct {
	Operations int      `json:"operations"`
	Files      []string `json:"files"`
}

func (t *ApplyPatchTool) Name() string {
	return "apply_patch"
}

func (t *ApplyPatchTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.Name(),
		Description: "使用 *** Begin Patch 结构化补丁在工作区内新增、更新、删除或移动多个文本文件。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "包含 *** Begin Patch 和 *** End Patch 的完整补丁",
				},
			},
			"required":             []string{"input"},
			"additionalProperties": false,
		},
	}
}

func (t *ApplyPatchTool) Execute(ctx context.Context, args json.RawMessage, _ agent.UpdateEmitter) (agent.ToolOutput, error) {
	output, details, err := t.executeWithDetails(ctx, args)
	if err != nil {
		return agent.ToolOutput{}, err
	}
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(output)}, Details: details}, nil
}

func (t *ApplyPatchTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	output, _, err := t.executeWithDetails(ctx, args)
	return output, err
}

func (t *ApplyPatchTool) executeWithDetails(ctx context.Context, args json.RawMessage) (string, ApplyPatchDetails, error) {
	if t == nil || t.workspace == nil {
		return "", ApplyPatchDetails{}, errors.New("apply_patch 未初始化")
	}
	if ctx == nil {
		return "", ApplyPatchDetails{}, errors.New("context 不能为空")
	}
	if !utf8.Valid(args) {
		return "", ApplyPatchDetails{}, errors.New("参数不是有效的 UTF-8 JSON")
	}
	input, err := decodeApplyPatchArgs(args)
	if err != nil {
		return "", ApplyPatchDetails{}, err
	}
	if input.Input == nil || strings.TrimSpace(*input.Input) == "" {
		return "", ApplyPatchDetails{}, errors.New("input 不能为空")
	}
	if strings.IndexByte(*input.Input, 0) >= 0 {
		return "", ApplyPatchDetails{}, errors.New("input 包含 NUL 字节")
	}
	if err := ctx.Err(); err != nil {
		return "", ApplyPatchDetails{}, fmt.Errorf("应用补丁已取消: %w", err)
	}
	operations, err := parseStructuredPatch(*input.Input)
	if err != nil {
		return "", ApplyPatchDetails{}, err
	}
	staged, order, err := t.preflight(ctx, operations)
	if err != nil {
		return "", ApplyPatchDetails{}, err
	}
	if err := validateStagedPatchPaths(staged); err != nil {
		return "", ApplyPatchDetails{}, err
	}
	if err := ctx.Err(); err != nil {
		return "", ApplyPatchDetails{}, fmt.Errorf("应用补丁已取消: %w", err)
	}
	for _, path := range order {
		file := staged[path]
		if file.exists {
			if err := t.writeStagedFile(path, file.content); err != nil {
				return "", ApplyPatchDetails{}, err
			}
		}
	}
	for _, path := range order {
		file := staged[path]
		if file.originallyExists {
			if file.exists {
				continue
			}
			if err := t.workspace.Remove(path); err != nil {
				return "", ApplyPatchDetails{}, fmt.Errorf("删除文件 %s 失败: %w", path, err)
			}
		}
	}
	files := make([]string, 0, len(staged))
	for path := range staged {
		files = append(files, path)
	}
	sort.Strings(files)
	details := ApplyPatchDetails{Operations: len(operations), Files: files}
	return fmt.Sprintf("Applied patch: %d operation(s) across %d file(s)", details.Operations, len(details.Files)), details, nil
}

type stagedPatchFile struct {
	exists           bool
	originallyExists bool
	content          string
}

func validateStagedPatchPaths(staged map[string]stagedPatchFile) error {
	paths := make([]string, 0, len(staged))
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	separator := string(filepath.Separator)
	for index, path := range paths {
		foldedPath := strings.ToLower(path)
		for _, other := range paths[index+1:] {
			foldedOther := strings.ToLower(other)
			if strings.EqualFold(path, other) {
				return fmt.Errorf("补丁路径存在大小写别名冲突: %s 与 %s", path, other)
			}
			if strings.HasPrefix(foldedOther, foldedPath+separator) || strings.HasPrefix(foldedPath, foldedOther+separator) {
				return fmt.Errorf("补丁路径存在父子文件冲突: %s 与 %s", path, other)
			}
		}
	}
	return nil
}

func (t *ApplyPatchTool) preflight(ctx context.Context, operations []patchOperation) (map[string]stagedPatchFile, []string, error) {
	staged := make(map[string]stagedPatchFile)
	var order []string
	load := func(path string) (stagedPatchFile, error) {
		if file, ok := staged[path]; ok {
			return file, nil
		}
		info, err := t.workspace.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			file := stagedPatchFile{}
			staged[path] = file
			order = append(order, path)
			return file, nil
		}
		if err != nil {
			return stagedPatchFile{}, fmt.Errorf("检查文件 %s 失败: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return stagedPatchFile{}, fmt.Errorf("补丁目标不是普通文件: %s", path)
		}
		content, err := t.workspace.ReadFile(path)
		if err != nil {
			return stagedPatchFile{}, fmt.Errorf("读取文件 %s 失败: %w", path, err)
		}
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return stagedPatchFile{}, fmt.Errorf("补丁目标不是有效 UTF-8 文本: %s", path)
		}
		file := stagedPatchFile{exists: true, originallyExists: true, content: string(content)}
		staged[path] = file
		order = append(order, path)
		return file, nil
	}
	store := func(path string, file stagedPatchFile) {
		staged[path] = file
	}

	for _, operation := range operations {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("应用补丁已取消: %w", err)
		}
		file, err := load(operation.path)
		if err != nil {
			return nil, nil, err
		}
		switch operation.kind {
		case patchAdd:
			if file.exists {
				return nil, nil, fmt.Errorf("Add File 目标已存在: %s", operation.path)
			}
			file.exists = true
			file.content = operation.add
			store(operation.path, file)

		case patchDelete:
			if !file.exists {
				return nil, nil, fmt.Errorf("Delete File 目标不存在: %s", operation.path)
			}
			file.exists = false
			file.content = ""
			store(operation.path, file)

		case patchUpdate:
			if !file.exists {
				return nil, nil, fmt.Errorf("Update File 目标不存在: %s", operation.path)
			}
			updated, err := applyPatchHunks(operation.path, file.content, operation.hunks)
			if err != nil {
				return nil, nil, err
			}
			if operation.moveTo == "" {
				file.content = updated
				store(operation.path, file)
				continue
			}
			destination, err := load(operation.moveTo)
			if err != nil {
				return nil, nil, err
			}
			if destination.exists {
				return nil, nil, fmt.Errorf("Move to 目标已存在: %s", operation.moveTo)
			}
			file.exists = false
			file.content = ""
			store(operation.path, file)
			destination.exists = true
			destination.content = updated
			store(operation.moveTo, destination)
		}
	}
	return staged, order, nil
}

func (t *ApplyPatchTool) writeStagedFile(path, content string) error {
	parent := filepath.Dir(path)
	if parent != "." {
		if err := t.workspace.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("创建父目录 %s 失败: %w", parent, err)
		}
	}
	file, err := t.workspace.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, defaultWrittenFileMode)
	if err != nil {
		return fmt.Errorf("打开补丁目标 %s 失败: %w", path, err)
	}
	writeErr := writeAll(file, []byte(content))
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("写入补丁目标 %s 失败: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭补丁目标 %s 失败: %w", path, closeErr)
	}
	return nil
}

type applyPatchArgs struct {
	Input *string `json:"input"`
}

func decodeApplyPatchArgs(args json.RawMessage) (applyPatchArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var input applyPatchArgs
	if err := decoder.Decode(&input); err != nil {
		return applyPatchArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return applyPatchArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return applyPatchArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}
