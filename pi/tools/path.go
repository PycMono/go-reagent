package tools

import (
	"errors"
	"path/filepath"
	"strings"
)

func cleanRelativePath(path string, requiresFile bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		if requiresFile {
			return "", errors.New("path 不能为空")
		}
		return ".", nil
	}

	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" ||
		strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") ||
		(len(path) >= 2 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':') {
		return "", errors.New("path 必须是相对于工作区的相对路径")
	}

	portable := filepath.ToSlash(strings.ReplaceAll(path, "\\", "/"))
	portable = filepath.Clean(portable)
	if portable == ".." || strings.HasPrefix(portable, "../") {
		return "", errors.New("path 不能逃逸工作区")
	}

	path = filepath.Clean(path)
	if requiresFile && path == "." {
		return "", errors.New("path 必须指向文件")
	}
	return path, nil
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
