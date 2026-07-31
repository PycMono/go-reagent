package tools

import (
	"bufio"
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

const (
	defaultReadFileMaxLines = 2000
	defaultReadFileMaxBytes = 50 * 1024
)

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
		Description:  "按行读取工作区内指定相对路径的 UTF-8 文本文件。单页最多 2000 行且最终输出不超过 50 KiB；出现 Use offset=N to continue 时请用 offset 继续读取。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对于工作区的文件路径，例如 cmd/reagent/main.go",
				},
				"offset": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "可选，1-based 起始行，默认 1",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     defaultReadFileMaxLines,
					"description": "可选，最多返回行数，默认且最大 2000",
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
	offset := 1
	if input.Offset != nil {
		offset = *input.Offset
	}
	if offset < 1 {
		return "", errors.New("offset 必须大于等于 1")
	}
	limit := defaultReadFileMaxLines
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > defaultReadFileMaxLines {
		return "", fmt.Errorf("limit 必须在 1 到 %d 之间", defaultReadFileMaxLines)
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

	reader := bufio.NewReader(file)
	for line := 1; line < offset; line++ {
		exists, err := discardReadFileLine(ctx, reader)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", nil
		}
	}
	return readFilePage(ctx, reader, offset, limit)
}

type readFileArgs struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
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

func readFilePage(ctx context.Context, reader *bufio.Reader, offset int, limit int) (string, error) {
	lines := make([][]byte, 0, limit)
	contentBytes := 0
	hasMore := false

	for len(lines) < limit {
		line, exists, tooLong, err := readFileLine(ctx, reader, defaultReadFileMaxBytes)
		if err != nil {
			return "", err
		}
		if !exists {
			break
		}
		if tooLong || contentBytes+len(line) > defaultReadFileMaxBytes {
			hasMore = true
			if len(lines) == 0 {
				return "", errors.New("单行超过 read_file 单页限制")
			}
			break
		}
		lines = append(lines, line)
		contentBytes += len(line)
	}

	if !hasMore && len(lines) == limit {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("读取已取消: %w", err)
		}
		_, err := reader.Peek(1)
		switch {
		case err == nil:
			hasMore = true
		case errors.Is(err, io.EOF):
			hasMore = false
		default:
			return "", fmt.Errorf("探测后续文件内容失败: %w", err)
		}
	}

	if len(lines) == 0 {
		return "", nil
	}
	if !hasMore {
		if err := validateReadFileLines(lines); err != nil {
			return "", err
		}
		return string(joinReadFileLines(lines)), nil
	}

	for len(lines) > 0 {
		content := joinReadFileLines(lines)
		end := offset + len(lines) - 1
		marker := readFileContinuationMarker(offset, end, end+1)
		separator := readFileMarkerSeparator(content)
		if len(content)+len(separator)+len(marker) <= defaultReadFileMaxBytes {
			if err := validateReadFileLines(lines); err != nil {
				return "", err
			}
			return string(content) + separator + marker, nil
		}
		lines = lines[:len(lines)-1]
	}
	return "", errors.New("单行超过 read_file 单页限制")
}

func validateReadFileLines(lines [][]byte) error {
	for _, line := range lines {
		if bytes.IndexByte(line, 0) >= 0 {
			return errors.New("文件包含 NUL 字节，疑似二进制内容")
		}
		if !utf8.Valid(line) {
			return errors.New("文件内容不是有效的 UTF-8 文本")
		}
	}
	return nil
}

func readFileLine(ctx context.Context, reader *bufio.Reader, maximum int) ([]byte, bool, bool, error) {
	line := make([]byte, 0, min(maximum, reader.Size()))
	tooLong := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, false, fmt.Errorf("读取已取消: %w", err)
		}
		fragment, err := reader.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(fragment) > maximum {
				tooLong = true
				line = nil
			} else {
				line = append(line, fragment...)
			}
		}
		switch {
		case err == nil:
			return line, true, tooLong, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(fragment) == 0 && len(line) == 0 && !tooLong {
				return nil, false, false, nil
			}
			return line, true, tooLong, nil
		default:
			return nil, false, false, fmt.Errorf("读取文件内容失败: %w", err)
		}
	}
}

func discardReadFileLine(ctx context.Context, reader *bufio.Reader) (bool, error) {
	readAny := false
	for {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("读取已取消: %w", err)
		}
		fragment, err := reader.ReadSlice('\n')
		readAny = readAny || len(fragment) > 0
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return readAny, nil
		default:
			return false, fmt.Errorf("跳过文件内容失败: %w", err)
		}
	}
}

func joinReadFileLines(lines [][]byte) []byte {
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	content := make([]byte, 0, total)
	for _, line := range lines {
		content = append(content, line...)
	}
	return content
}

func readFileContinuationMarker(start int, end int, next int) string {
	return fmt.Sprintf("[Showing lines %d-%d. Use offset=%d to continue.]", start, end, next)
}

func readFileMarkerSeparator(content []byte) string {
	if bytes.HasSuffix(content, []byte{'\n'}) {
		return "\n"
	}
	return "\n\n"
}
