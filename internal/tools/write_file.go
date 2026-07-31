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
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/internal/schema"
)

const defaultWrittenFileMode = 0o644

type WriteFileTool struct {
	root *os.Root
}

var _ BaseTool = (*WriteFileTool)(nil)

func NewWriteFileTool(workDir string) (*WriteFileTool, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil, errors.New("workDir 不能为空")
	}
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("解析工作区失败: %w", err)
	}
	root, err := os.OpenRoot(absoluteWorkDir)
	if err != nil {
		return nil, fmt.Errorf("打开工作区失败: %w", err)
	}
	return &WriteFileTool{root: root}, nil
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "创建或完整覆盖工作区内的 UTF-8 文本文件，并自动创建父目录。仅用于新文件或完整重写。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对于工作区的文件路径",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "文件的完整 UTF-8 文本内容",
				},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
	}
}

func (t *WriteFileTool) Close() error {
	if t == nil || t.root == nil {
		return nil
	}
	return t.root.Close()
}

func (t *WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t == nil || t.root == nil {
		return "", errors.New("write_file 未初始化")
	}
	if ctx == nil {
		return "", errors.New("context 不能为空")
	}
	if !utf8.Valid(args) {
		return "", errors.New("参数不是有效的 UTF-8 JSON")
	}
	input, err := decodeWriteFileArgs(args)
	if err != nil {
		return "", err
	}
	path, err := cleanWorkspaceFilePath(input.Path)
	if err != nil {
		return "", err
	}
	if input.Content == nil {
		return "", errors.New("content 字段不能为空缺失")
	}
	content := *input.Content
	if !utf8.ValidString(content) {
		return "", errors.New("content 不是有效的 UTF-8 文本")
	}
	if strings.IndexByte(content, 0) >= 0 {
		return "", errors.New("content 包含 NUL 字节")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("写入已取消: %w", err)
	}

	existing, statErr := t.root.Stat(path)
	switch {
	case statErr == nil && !existing.Mode().IsRegular():
		return "", errors.New("只允许覆盖普通文件")
	case statErr == nil:
		current, err := t.root.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("读取现有文件失败: %w", err)
		}
		if bytes.Equal(current, []byte(content)) {
			return fmt.Sprintf("文件内容未变化: %s", path), nil
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return "", fmt.Errorf("检查目标文件失败: %w", statErr)
	}

	parent := filepath.Dir(path)
	if parent != "." {
		if err := t.root.MkdirAll(parent, 0o755); err != nil {
			return "", fmt.Errorf("创建父目录失败: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("写入已取消: %w", err)
	}
	file, err := t.root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, defaultWrittenFileMode)
	if err != nil {
		return "", fmt.Errorf("打开目标文件失败: %w", err)
	}
	writeErr := writeAll(file, []byte(content))
	closeErr := file.Close()
	if writeErr != nil {
		return "", fmt.Errorf("写入文件失败: %w", writeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("关闭文件失败: %w", closeErr)
	}
	return fmt.Sprintf("成功写入文件: %s (%d bytes)", path, len([]byte(content))), nil
}

type writeFileArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

func decodeWriteFileArgs(args json.RawMessage) (writeFileArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var input writeFileArgs
	if err := decoder.Decode(&input); err != nil {
		return writeFileArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return writeFileArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return writeFileArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}

func cleanWorkspaceFilePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path 不能为空")
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", errors.New("path 必须是相对于工作区的相对路径")
	}
	path = filepath.Clean(path)
	if path == "." {
		return "", errors.New("path 必须指向文件")
	}
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", errors.New("path 不能逃逸工作区")
	}
	return path, nil
}
