package chat

import (
	"encoding/json"
	"path"
	"strings"
)

func isReadTool(name string) bool { return name == "read" }

func isSkillRead(name string, arguments json.RawMessage) bool {
	if !isReadTool(name) {
		return false
	}
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(arguments, &input) != nil {
		return false
	}
	normalized := path.Clean(strings.ReplaceAll(strings.TrimSpace(input.Path), `\`, "/"))
	parts := strings.FieldsFunc(normalized, func(value rune) bool {
		return value == '/'
	})
	if len(parts) < 3 || parts[len(parts)-1] != "SKILL.md" {
		return false
	}
	for _, part := range parts[:len(parts)-2] {
		if part == "skills" {
			return true
		}
	}
	return false
}
