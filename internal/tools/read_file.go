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

const maxReadFileBytes = 8000

const readFileTruncationMarker = "...[文件内容超过限制，已截断至前 8000 字节]..."

// ReadFileTool 读取 os.Root 能力边界内的普通文本文件。
type ReadFileTool struct {
	root *os.Root
}

var _ BaseTool = (*ReadFileTool)(nil)

// NewReadFileTool 为 workDir 创建一个不能逃逸的文件读取工具。
func NewReadFileTool(workDir string) (*ReadFileTool, error) {
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
	return &ReadFileTool{root: root}, nil
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:         t.Name(),
		Description:  "读取工作区内指定相对路径的文本文件内容。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对于工作区的文件路径，例如 cmd/reagent/main.go",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	}
}

// Close 释放 ReadFileTool 持有的工作区 Root。
func (t *ReadFileTool) Close() error {
	if t == nil || t.root == nil {
		return nil
	}
	return t.root.Close()
}

func (t *ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t == nil || t.root == nil {
		return "", errors.New("read_file 未初始化")
	}
	if ctx == nil {
		return "", errors.New("context 不能为空")
	}

	input, err := decodeReadFileArgs(args)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("读取已取消: %w", err)
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		return "", errors.New("path 不能为空")
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", errors.New("path 必须是相对于工作区的相对路径")
	}

	file, err := t.root.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("检查文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("只允许读取普通文件")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("读取已取消: %w", err)
	}

	content, err := io.ReadAll(io.LimitReader(file, maxReadFileBytes+utf8.UTFMax))
	if err != nil {
		return "", fmt.Errorf("读取文件内容失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("读取已取消: %w", err)
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return "", errors.New("文件包含 NUL 字节，疑似二进制内容")
	}

	if len(content) <= maxReadFileBytes {
		if !utf8.Valid(content) {
			return "", errors.New("文件内容不是有效的 UTF-8 文本")
		}
		return string(content), nil
	}
	if !validUTF8Window(content) {
		return "", errors.New("文件内容不是有效的 UTF-8 文本")
	}

	cut, ok := validUTF8Cut(content, maxReadFileBytes)
	if !ok {
		return "", errors.New("文件内容不是有效的 UTF-8 文本")
	}
	return string(content[:cut]) + "\n\n" + readFileTruncationMarker, nil
}

type readFileArgs struct {
	Path string `json:"path"`
}

func decodeReadFileArgs(args json.RawMessage) (readFileArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()

	var input readFileArgs
	if err := decoder.Decode(&input); err != nil {
		return readFileArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return readFileArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return readFileArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}

func validUTF8Window(content []byte) bool {
	for len(content) > 0 {
		if !utf8.FullRune(content) {
			return true
		}
		runeValue, size := utf8.DecodeRune(content)
		if runeValue == utf8.RuneError && size == 1 {
			return false
		}
		content = content[size:]
	}
	return true
}

func validUTF8Cut(content []byte, maximum int) (int, bool) {
	minimum := maximum - (utf8.UTFMax - 1)
	if minimum < 0 {
		minimum = 0
	}
	for cut := maximum; cut >= minimum; cut-- {
		if utf8.Valid(content[:cut]) {
			return cut, true
		}
	}
	return 0, false
}
