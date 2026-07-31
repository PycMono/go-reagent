package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PycMono/go-reagent/internal/schema"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type EditFileTool struct {
	root *os.Root
}

var _ Tool = (*EditFileTool)(nil)

func NewEditFileTool(workDir string) (*EditFileTool, error) {
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
	return &EditFileTool{root: root}, nil
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "对工作区内的现有文本文件执行一次局部字符串替换；old_text 必须唯一匹配。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对于工作区的文件路径，例如 cmd/reagent/main.go",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "文件中要替换的唯一文本片段；请提供足够上下文",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "替换后的文本；可为空字符串以删除匹配片段",
				},
			},
			"required":             []string{"path", "old_text", "new_text"},
			"additionalProperties": false,
		},
	}
}

func (t *EditFileTool) Close() error {
	if t == nil || t.root == nil {
		return nil
	}
	return t.root.Close()
}

func (t *EditFileTool) Execute(ctx context.Context, args json.RawMessage, _ UpdateEmitter) (schema.ToolOutput, error) {
	output, err := t.execute(ctx, args)
	return textToolOutput(output), err
}

func (t *EditFileTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t == nil || t.root == nil {
		return "", errors.New("edit_file 未初始化")
	}
	if ctx == nil {
		return "", errors.New("context 不能为空")
	}

	input, err := decodeEditFileArgs(args)
	if err != nil {
		return "", err
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		return "", errors.New("path 不能为空")
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", errors.New("path 必须是相对于工作区的相对路径")
	}
	if strings.TrimSpace(input.OldText) == "" {
		return "", errors.New("old_text 不能为空")
	}
	if input.NewText == nil {
		return "", errors.New("new_text 字段不能为空缺失")
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("修改已取消: %w", err)
	}
	file, err := t.root.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("检查文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("只允许修改普通文件")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("修改已取消: %w", err)
	}

	contentBytes, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("读取文件内容失败: %w", err)
	}
	if !utf8.Valid(contentBytes) {
		return "", errors.New("文件内容不是有效的 UTF-8 文本")
	}
	if bytes.IndexByte(contentBytes, 0) >= 0 {
		return "", errors.New("文件包含 NUL 字节，疑似二进制内容")
	}

	originalContent := string(contentBytes)
	match, err := findUniqueTextMatch(originalContent, input.OldText)
	if err != nil {
		return "", err
	}
	replacement := replacementForMatch(originalContent, match, *input.NewText)
	newContent := originalContent[:match.start] + replacement + originalContent[match.end:]
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("修改已取消: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("定位文件写入位置失败: %w", err)
	}
	if err := writeAll(file, []byte(newContent)); err != nil {
		return "", fmt.Errorf("写回文件失败: %w", err)
	}
	if err := file.Truncate(int64(len(newContent))); err != nil {
		return "", fmt.Errorf("截断文件失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("修改已取消: %w", err)
	}

	return fmt.Sprintf("成功修改文件: %s", path), nil
}

func writeAll(writer io.Writer, content []byte) error {
	for len(content) > 0 {
		written, err := writer.Write(content)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

type editFileArgs struct {
	Path    string  `json:"path"`
	OldText string  `json:"old_text"`
	NewText *string `json:"new_text"`
}

func decodeEditFileArgs(args json.RawMessage) (editFileArgs, error) {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()

	var input editFileArgs
	if err := decoder.Decode(&input); err != nil {
		return editFileArgs{}, fmt.Errorf("参数解析失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return editFileArgs{}, fmt.Errorf("参数包含多余内容: %w", err)
		}
		return editFileArgs{}, errors.New("参数包含多余 JSON 内容")
	}
	return input, nil
}

type textMatch struct {
	start int
	end   int
	level int
}

func findUniqueTextMatch(content, oldText string) (textMatch, error) {
	if strings.TrimSpace(oldText) == "" {
		return textMatch{}, fmt.Errorf("old_text 不能为空")
	}

	exactMatches := matchesAtIndexes(occurrenceIndexes(content, oldText), len(oldText), 1, nil)
	if match, found, err := requireUniqueMatch(exactMatches); found || err != nil {
		return match, err
	}

	normalizedContent, boundaries := normalizeNewlinesWithBoundaries(content)
	normalizedOld, _ := normalizeNewlinesWithBoundaries(oldText)
	newlineMatches := matchesAtIndexes(
		occurrenceIndexes(normalizedContent, normalizedOld),
		len(normalizedOld),
		2,
		boundaries,
	)
	if match, found, err := requireUniqueMatch(newlineMatches); found || err != nil {
		return match, err
	}

	trimmedOld := strings.TrimSpace(normalizedOld)
	trimmedMatches := matchesAtIndexes(
		occurrenceIndexes(normalizedContent, trimmedOld),
		len(trimmedOld),
		3,
		boundaries,
	)
	if match, found, err := requireUniqueMatch(trimmedMatches); found || err != nil {
		return match, err
	}

	lineMatches := lineByLineMatches(content, normalizedOld)
	if match, found, err := requireUniqueMatch(lineMatches); found || err != nil {
		return match, err
	}

	return textMatch{}, fmt.Errorf("在文件中未找到 old_text，请先调用 read_file 确认文件内容和缩进")
}

func occurrenceIndexes(content, needle string) []int {
	if needle == "" {
		return nil
	}
	var indexes []int
	for offset := 0; offset <= len(content)-len(needle); {
		index := strings.Index(content[offset:], needle)
		if index < 0 {
			break
		}
		index += offset
		indexes = append(indexes, index)
		offset = index + 1
	}
	return indexes
}

func matchesAtIndexes(indexes []int, length, level int, boundaries []int) []textMatch {
	matches := make([]textMatch, 0, len(indexes))
	for _, index := range indexes {
		start, end := index, index+length
		if boundaries != nil {
			start = boundaries[start]
			end = boundaries[end]
		}
		matches = append(matches, textMatch{start: start, end: end, level: level})
	}
	return matches
}

func requireUniqueMatch(matches []textMatch) (textMatch, bool, error) {
	switch len(matches) {
	case 0:
		return textMatch{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return textMatch{}, false, fmt.Errorf("模糊匹配到了 %d 处相似代码，请提供更多上下文以确保唯一性", len(matches))
	}
}

func normalizeNewlinesWithBoundaries(input string) (string, []int) {
	normalized := make([]byte, 0, len(input))
	boundaries := make([]int, 1, len(input)+1)
	for index := 0; index < len(input); {
		if input[index] == '\r' && index+1 < len(input) && input[index+1] == '\n' {
			normalized = append(normalized, '\n')
			index += 2
			boundaries = append(boundaries, index)
			continue
		}
		normalized = append(normalized, input[index])
		index++
		boundaries = append(boundaries, index)
	}
	return string(normalized), boundaries
}

type textLine struct {
	text  string
	start int
	end   int
}

func lineByLineMatches(content, oldText string) []textMatch {
	normalizedContent, boundaries := normalizeNewlinesWithBoundaries(content)
	contentLines := splitTextLines(normalizedContent, boundaries)
	trimmedOld := strings.TrimSpace(oldText)
	if trimmedOld == "" {
		return nil
	}
	oldLines := strings.Split(trimmedOld, "\n")
	if len(contentLines) < len(oldLines) {
		return nil
	}
	for index := range oldLines {
		oldLines[index] = strings.TrimSpace(oldLines[index])
	}

	var matches []textMatch
	for start := 0; start <= len(contentLines)-len(oldLines); start++ {
		matched := true
		for offset := range oldLines {
			if strings.TrimSpace(contentLines[start+offset].text) != oldLines[offset] {
				matched = false
				break
			}
		}
		if matched {
			last := start + len(oldLines) - 1
			matches = append(matches, textMatch{
				start: contentLines[start].start,
				end:   contentLines[last].end,
				level: 4,
			})
		}
	}
	return matches
}

func splitTextLines(normalized string, boundaries []int) []textLine {
	var lines []textLine
	lineStart := 0
	for index := 0; index <= len(normalized); index++ {
		if index < len(normalized) && normalized[index] != '\n' {
			continue
		}
		lines = append(lines, textLine{
			text:  normalized[lineStart:index],
			start: boundaries[lineStart],
			end:   boundaries[index],
		})
		lineStart = index + 1
	}
	return lines
}

func replacementForMatch(content string, match textMatch, newText string) string {
	style := firstNewlineStyle(content[match.start:match.end])
	if style == "" {
		style = dominantNewlineStyle(content)
	}
	if style == "" {
		return newText
	}

	normalized, _ := normalizeNewlinesWithBoundaries(newText)
	if style == "\r\n" {
		return strings.ReplaceAll(normalized, "\n", "\r\n")
	}
	return normalized
}

func firstNewlineStyle(content string) string {
	for index := 0; index < len(content); index++ {
		switch content[index] {
		case '\r':
			if index+1 < len(content) && content[index+1] == '\n' {
				return "\r\n"
			}
		case '\n':
			return "\n"
		}
	}
	return ""
}

func dominantNewlineStyle(content string) string {
	crlfCount := strings.Count(content, "\r\n")
	lfCount := strings.Count(content, "\n") - crlfCount
	if crlfCount > lfCount {
		return "\r\n"
	}
	if lfCount > crlfCount {
		return "\n"
	}
	return firstNewlineStyle(content)
}
