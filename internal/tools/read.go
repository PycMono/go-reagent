package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

const (
	defaultReadFileMaxLines = 2000
	defaultReadFileMaxBytes = 50 * 1024
)

type ReadDetails struct {
	Path       string `json:"path"`
	Lines      int    `json:"lines"`
	Bytes      int    `json:"bytes"`
	Truncated  bool   `json:"truncated"`
	NextOffset int    `json:"nextOffset,omitempty"`
}

// ReadTool reads regular UTF-8 text files from the shared workspace.
type ReadTool struct{ workspace *Workspace }

var _ agent.Tool = (*ReadTool)(nil)

func NewReadTool(workspace *Workspace) (*ReadTool, error) {
	if workspace == nil {
		return nil, errors.New("workspace 不能为空")
	}
	return &ReadTool{workspace: workspace}, nil
}

func (t *ReadTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         "read",
		Description:  "按行读取工作区内指定相对路径的 UTF-8 文本文件。单页最多 2000 行且最终输出不超过 50 KiB；出现 Use offset=N to continue 时请用 offset 继续读取。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "相对于工作区的文件路径，例如 cmd/reagent/main.go"},
				"offset": map[string]any{"type": "integer", "minimum": 1, "description": "可选，1-based 起始行，默认 1"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": defaultReadFileMaxLines, "description": "可选，最多返回行数，默认且最大 2000"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	}
}

func (t *ReadTool) Execute(ctx context.Context, args json.RawMessage, _ agent.UpdateEmitter) (agent.ToolOutput, error) {
	output, details, err := t.executeWithDetails(ctx, args)
	if err != nil {
		return agent.ToolOutput{}, err
	}
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(output)}, Details: details}, nil
}

func (t *ReadTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	output, _, err := t.executeWithDetails(ctx, args)
	return output, err
}

func (t *ReadTool) executeWithDetails(ctx context.Context, args json.RawMessage) (string, ReadDetails, error) {
	if t == nil || t.workspace == nil {
		return "", ReadDetails{}, errors.New("read 未初始化")
	}
	if ctx == nil {
		return "", ReadDetails{}, errors.New("context 不能为空")
	}
	input, err := decodeReadFileArgs(args)
	if err != nil {
		return "", ReadDetails{}, err
	}
	if err := ctx.Err(); err != nil {
		return "", ReadDetails{}, fmt.Errorf("读取已取消: %w", err)
	}
	path, err := cleanRelativePath(input.Path, true)
	if err != nil {
		return "", ReadDetails{}, err
	}
	offset, limit := 1, defaultReadFileMaxLines
	if input.Offset != nil {
		offset = *input.Offset
	}
	if offset < 1 {
		return "", ReadDetails{}, errors.New("offset 必须大于等于 1")
	}
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > defaultReadFileMaxLines {
		return "", ReadDetails{}, fmt.Errorf("limit 必须在 1 到 %d 之间", defaultReadFileMaxLines)
	}
	info, err := t.workspace.Stat(path)
	if err != nil {
		return "", ReadDetails{}, fmt.Errorf("检查文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", ReadDetails{}, errors.New("只允许读取普通文件")
	}
	file, err := t.workspace.Open(path)
	if err != nil {
		return "", ReadDetails{}, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for line := 1; line < offset; line++ {
		exists, err := discardReadFileLine(ctx, reader)
		if err != nil {
			return "", ReadDetails{}, err
		}
		if !exists {
			return "", ReadDetails{Path: path}, nil
		}
	}
	output, lines, contentBytes, truncated, nextOffset, err := readFilePage(ctx, reader, offset, limit)
	if err != nil {
		return "", ReadDetails{}, err
	}
	details := ReadDetails{Path: path, Lines: lines, Bytes: contentBytes, Truncated: truncated}
	if truncated {
		details.NextOffset = nextOffset
	}
	return output, details, nil
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

func readFilePage(ctx context.Context, reader *bufio.Reader, offset, limit int) (string, int, int, bool, int, error) {
	lines, contentBytes, hasMore := make([][]byte, 0, limit), 0, false
	for len(lines) < limit {
		line, exists, tooLong, err := readFileLine(ctx, reader, defaultReadFileMaxBytes)
		if err != nil {
			return "", 0, 0, false, 0, err
		}
		if !exists {
			break
		}
		if tooLong || contentBytes+len(line) > defaultReadFileMaxBytes {
			hasMore = true
			if len(lines) == 0 {
				return "", 0, 0, false, 0, errors.New("单行超过 read 单页限制")
			}
			break
		}
		lines, contentBytes = append(lines, line), contentBytes+len(line)
	}
	if !hasMore && len(lines) == limit {
		if err := ctx.Err(); err != nil {
			return "", 0, 0, false, 0, fmt.Errorf("读取已取消: %w", err)
		}
		_, err := reader.Peek(1)
		switch {
		case err == nil:
			hasMore = true
		case errors.Is(err, io.EOF):
		default:
			return "", 0, 0, false, 0, fmt.Errorf("探测后续文件内容失败: %w", err)
		}
	}
	if len(lines) == 0 {
		return "", 0, 0, false, 0, nil
	}
	if !hasMore {
		if err := validateReadFileLines(lines); err != nil {
			return "", 0, 0, false, 0, err
		}
		return string(joinReadFileLines(lines)), len(lines), contentBytes, false, 0, nil
	}
	for len(lines) > 0 {
		content := joinReadFileLines(lines)
		end := offset + len(lines) - 1
		marker := readFileContinuationMarker(offset, end, end+1)
		separator := readFileMarkerSeparator(content)
		if len(content)+len(separator)+len(marker) <= defaultReadFileMaxBytes {
			if err := validateReadFileLines(lines); err != nil {
				return "", 0, 0, false, 0, err
			}
			return string(content) + separator + marker, len(lines), len(content), true, end + 1, nil
		}
		lines = lines[:len(lines)-1]
	}
	return "", 0, 0, false, 0, errors.New("单行超过 read 单页限制")
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

func readFileContinuationMarker(start, end, next int) string {
	return fmt.Sprintf("[Showing lines %d-%d. Use offset=%d to continue.]", start, end, next)
}

func readFileMarkerSeparator(content []byte) string {
	if bytes.HasSuffix(content, []byte{'\n'}) {
		return "\n"
	}
	return "\n\n"
}
