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

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/schema"
)

const defaultWrittenFileMode = 0o644

type WriteDetails struct {
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Changed bool   `json:"changed"`
}

// WriteTool creates or overwrites UTF-8 text files in the shared workspace.
type WriteTool struct{ workspace *Workspace }

var _ Tool = (*WriteTool)(nil)

func NewWriteTool(workspace *Workspace) (*WriteTool, error) {
	if workspace == nil {
		return nil, errors.New("workspace 不能为空")
	}
	return &WriteTool{workspace: workspace}, nil
}

func (t *WriteTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        "write",
		Description: "创建或完整覆盖工作区内的 UTF-8 文本文件，并自动创建父目录。仅用于新文件或完整重写。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "相对于工作区的文件路径"},
				"content": map[string]any{"type": "string", "description": "文件的完整 UTF-8 文本内容"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
	}
}

func (t *WriteTool) Execute(ctx context.Context, args json.RawMessage, _ UpdateEmitter) (schema.ToolOutput, error) {
	output, details, err := t.executeWithDetails(ctx, args)
	if err != nil {
		return schema.ToolOutput{}, err
	}
	return schema.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(output)}, Details: details}, nil
}

func (t *WriteTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	output, _, err := t.executeWithDetails(ctx, args)
	return output, err
}

func (t *WriteTool) executeWithDetails(ctx context.Context, args json.RawMessage) (string, WriteDetails, error) {
	if t == nil || t.workspace == nil {
		return "", WriteDetails{}, errors.New("write 未初始化")
	}
	if ctx == nil {
		return "", WriteDetails{}, errors.New("context 不能为空")
	}
	if !utf8.Valid(args) {
		return "", WriteDetails{}, errors.New("参数不是有效的 UTF-8 JSON")
	}
	input, err := decodeWriteFileArgs(args)
	if err != nil {
		return "", WriteDetails{}, err
	}
	path, err := cleanRelativePath(input.Path, true)
	if err != nil {
		return "", WriteDetails{}, err
	}
	if input.Content == nil {
		return "", WriteDetails{}, errors.New("content 字段不能为空缺失")
	}
	content := *input.Content
	if !utf8.ValidString(content) {
		return "", WriteDetails{}, errors.New("content 不是有效的 UTF-8 文本")
	}
	if strings.IndexByte(content, 0) >= 0 {
		return "", WriteDetails{}, errors.New("content 包含 NUL 字节")
	}
	if err := ctx.Err(); err != nil {
		return "", WriteDetails{}, fmt.Errorf("写入已取消: %w", err)
	}

	info, statErr := t.workspace.Stat(path)
	switch {
	case statErr == nil:
		if !info.Mode().IsRegular() {
			return "", WriteDetails{}, errors.New("只允许覆盖普通文件")
		}
		current, readErr := t.workspace.ReadFile(path)
		if readErr != nil {
			return "", WriteDetails{}, fmt.Errorf("读取现有文件失败: %w", readErr)
		}
		if bytes.Equal(current, []byte(content)) {
			return fmt.Sprintf("文件内容未变化: %s", path), WriteDetails{Path: path, Bytes: len(current), Changed: false}, nil
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return "", WriteDetails{}, fmt.Errorf("检查目标文件失败: %w", statErr)
	}

	parent := filepath.Dir(path)
	if parent != "." {
		if err := t.workspace.MkdirAll(parent, 0o755); err != nil {
			return "", WriteDetails{}, fmt.Errorf("创建父目录失败: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", WriteDetails{}, fmt.Errorf("写入已取消: %w", err)
	}
	file, err := t.workspace.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, defaultWrittenFileMode)
	if err != nil {
		return "", WriteDetails{}, fmt.Errorf("打开目标文件失败: %w", err)
	}
	writeErr := writeAll(file, []byte(content))
	closeErr := file.Close()
	if writeErr != nil {
		return "", WriteDetails{}, fmt.Errorf("写入文件失败: %w", writeErr)
	}
	if closeErr != nil {
		return "", WriteDetails{}, fmt.Errorf("关闭文件失败: %w", closeErr)
	}
	details := WriteDetails{Path: path, Bytes: len([]byte(content)), Changed: true}
	return fmt.Sprintf("成功写入文件: %s (%d bytes)", path, details.Bytes), details, nil
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
