package tools

import (
	"errors"
	"fmt"
	"strings"
)

type patchOperationKind uint8

const (
	patchAdd patchOperationKind = iota + 1
	patchUpdate
	patchDelete
)

type patchOperation struct {
	kind   patchOperationKind
	path   string
	moveTo string
	add    string
	hunks  []patchHunk
}

type patchHunk struct {
	section   string
	lines     []patchLine
	endOfFile bool
}

type patchLine struct {
	kind byte
	text string
}

func parseStructuredPatch(input string) ([]patchOperation, error) {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(input, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "*** Begin Patch" || lines[len(lines)-1] != "*** End Patch" {
		return nil, errors.New("补丁必须由 *** Begin Patch 和 *** End Patch 包裹")
	}
	var operations []patchOperation
	for index := 1; index < len(lines)-1; {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path, err := cleanWorkspaceFilePath(strings.TrimPrefix(line, "*** Add File: "))
			if err != nil {
				return nil, fmt.Errorf("Add File 路径无效: %w", err)
			}
			index++
			var content []string
			for index < len(lines)-1 && !isPatchOperationHeader(lines[index]) {
				if !strings.HasPrefix(lines[index], "+") {
					return nil, fmt.Errorf("Add File %s 的内容行必须以 + 开头", path)
				}
				content = append(content, strings.TrimPrefix(lines[index], "+"))
				index++
			}
			if len(content) == 0 {
				return nil, fmt.Errorf("Add File %s 缺少以 + 开头的内容", path)
			}
			operations = append(operations, patchOperation{kind: patchAdd, path: path, add: strings.Join(content, "\n") + "\n"})

		case strings.HasPrefix(line, "*** Delete File: "):
			path, err := cleanWorkspaceFilePath(strings.TrimPrefix(line, "*** Delete File: "))
			if err != nil {
				return nil, fmt.Errorf("Delete File 路径无效: %w", err)
			}
			operations = append(operations, patchOperation{kind: patchDelete, path: path})
			index++

		case strings.HasPrefix(line, "*** Update File: "):
			operation, next, err := parseUpdateOperation(lines, index)
			if err != nil {
				return nil, err
			}
			operations = append(operations, operation)
			index = next

		default:
			return nil, fmt.Errorf("无法识别补丁指令: %s", line)
		}
	}
	if len(operations) == 0 {
		return nil, errors.New("补丁不包含文件操作")
	}
	return operations, nil
}

func parseUpdateOperation(lines []string, index int) (patchOperation, int, error) {
	path, err := cleanWorkspaceFilePath(strings.TrimPrefix(lines[index], "*** Update File: "))
	if err != nil {
		return patchOperation{}, index, fmt.Errorf("Update File 路径无效: %w", err)
	}
	operation := patchOperation{kind: patchUpdate, path: path}
	index++
	if index < len(lines)-1 && strings.HasPrefix(lines[index], "*** Move to: ") {
		operation.moveTo, err = cleanWorkspaceFilePath(strings.TrimPrefix(lines[index], "*** Move to: "))
		if err != nil {
			return patchOperation{}, index, fmt.Errorf("Move to 路径无效: %w", err)
		}
		index++
	}
	for index < len(lines)-1 && !isPatchOperationHeader(lines[index]) {
		if !strings.HasPrefix(lines[index], "@@") {
			return patchOperation{}, index, fmt.Errorf("Update File %s 的修改块必须以 @@ 开头", path)
		}
		hunk := patchHunk{section: strings.TrimSpace(strings.TrimPrefix(lines[index], "@@"))}
		index++
		for index < len(lines)-1 && !strings.HasPrefix(lines[index], "@@") && !isPatchOperationHeader(lines[index]) {
			if lines[index] == "*** End of File" {
				hunk.endOfFile = true
				index++
				break
			}
			if lines[index] == "" || !strings.ContainsRune(" +-", rune(lines[index][0])) {
				return patchOperation{}, index, fmt.Errorf("Update File %s 包含无效修改行 %q", path, lines[index])
			}
			hunk.lines = append(hunk.lines, patchLine{kind: lines[index][0], text: lines[index][1:]})
			index++
		}
		if len(hunk.lines) == 0 {
			return patchOperation{}, index, fmt.Errorf("Update File %s 包含空修改块", path)
		}
		operation.hunks = append(operation.hunks, hunk)
	}
	if len(operation.hunks) == 0 && operation.moveTo == "" {
		return patchOperation{}, index, fmt.Errorf("Update File %s 不包含修改块", path)
	}
	return operation, index, nil
}

func isPatchOperationHeader(line string) bool {
	return line == "*** End Patch" ||
		strings.HasPrefix(line, "*** Add File: ") ||
		strings.HasPrefix(line, "*** Update File: ") ||
		strings.HasPrefix(line, "*** Delete File: ")
}

func applyPatchHunks(path, content string, hunks []patchHunk) (string, error) {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	hadFinalNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(normalized, "\n")
	if hadFinalNewline {
		lines = lines[:len(lines)-1]
	}
	cursor := 0
	for _, hunk := range hunks {
		if hunk.section != "" {
			sectionIndex, count := findSection(lines, cursor, hunk.section)
			switch {
			case count == 0:
				return "", fmt.Errorf("未找到补丁段落 %q: %s", hunk.section, path)
			case count > 1:
				return "", fmt.Errorf("补丁段落 %q 在 %s 中匹配多处", hunk.section, path)
			}
			cursor = sectionIndex
		}
		oldLines, newLines := hunkOldAndNewLines(hunk)
		matches := findLineSequence(lines, cursor, oldLines)
		if hunk.endOfFile {
			matches = filterEndOfFileMatches(matches, len(oldLines), len(lines))
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("未找到补丁上下文: %s", path)
		case 1:
			index := matches[0]
			updated := make([]string, 0, len(lines)-len(oldLines)+len(newLines))
			updated = append(updated, lines[:index]...)
			updated = append(updated, newLines...)
			updated = append(updated, lines[index+len(oldLines):]...)
			lines = updated
			cursor = index + len(newLines)
		default:
			return "", fmt.Errorf("补丁上下文在 %s 中匹配多处", path)
		}
	}
	result := strings.Join(lines, "\n")
	if hadFinalNewline && len(lines) > 0 {
		result += "\n"
	}
	if newline == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return result, nil
}

func hunkOldAndNewLines(hunk patchHunk) ([]string, []string) {
	var oldLines, newLines []string
	for _, line := range hunk.lines {
		if line.kind != '+' {
			oldLines = append(oldLines, line.text)
		}
		if line.kind != '-' {
			newLines = append(newLines, line.text)
		}
	}
	return oldLines, newLines
}

func findSection(lines []string, start int, section string) (int, int) {
	first, count := -1, 0
	for index := start; index < len(lines); index++ {
		if strings.Contains(strings.TrimSpace(lines[index]), section) {
			if first < 0 {
				first = index
			}
			count++
		}
	}
	return first, count
}

func findLineSequence(lines []string, start int, sequence []string) []int {
	if len(sequence) == 0 {
		return []int{len(lines)}
	}
	var matches []int
	for index := start; index+len(sequence) <= len(lines); index++ {
		matched := true
		for offset := range sequence {
			if lines[index+offset] != sequence[offset] {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, index)
		}
	}
	return matches
}

func filterEndOfFileMatches(matches []int, sequenceLength, lineCount int) []int {
	filtered := matches[:0]
	for _, index := range matches {
		if index+sequenceLength == lineCount {
			filtered = append(filtered, index)
		}
	}
	return filtered
}
