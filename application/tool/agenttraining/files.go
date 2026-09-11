// Package agenttraining exposes only candidate asset operations to an author.
package agenttraining

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/pi/ai"
)

type FileTool struct {
	root string
	mu   sync.Mutex
}

func NewFileTool(root string) (*FileTool, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid candidate root")
	}
	return &FileTool{root: absolute}, nil
}
func (t *FileTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{Name: "training_file", Description: "Read, list, write, edit or delete this training candidate's assets. Use AGENTS.md for behavior; documents/ and assets/ for supporting files; skills/<id>/SKILL.md with scripts/, references/ and assets/ under that Skill. Never changes production. Scripts cannot execute through this tool.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"list", "read", "write", "edit", "delete"}}, "path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "old_text": map[string]any{"type": "string"}}, "required": []string{"action"}, "additionalProperties": false}}
}

type fileRequest struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Content string `json:"content"`
	OldText string `json:"old_text"`
}

func allowedFile(name string) bool {
	if name == "AGENTS.md" {
		return true
	}
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == ".git" || strings.HasPrefix(part, ".author-") {
			return false
		}
	}
	switch parts[0] {
	case "skills", "documents", "assets", ".tmp":
		return true
	}
	return false
}
func (t *FileTool) Execute(ctx context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var q fileRequest
	if err := agentversion.DecodeStrict(raw, &q); err != nil {
		return ai.ToolOutput{}, err
	}
	if err := ctx.Err(); err != nil {
		return ai.ToolOutput{}, err
	}
	root, err := os.OpenRoot(t.root)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	defer root.Close()
	output := func(s string) (ai.ToolOutput, error) {
		return ai.ToolOutput{Content: ai.ContentBlocks{ai.TextBlock(s)}}, nil
	}
	if q.Action == "list" {
		var names []string
		err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if name != "." && !allowedFile(name+"/x") {
					return fs.SkipDir
				}
				return nil
			}
			if allowedFile(name) {
				names = append(names, name)
			}
			if len(names) > 2000 {
				return errors.New("candidate file limit exceeded")
			}
			return nil
		})
		if err != nil {
			return ai.ToolOutput{}, err
		}
		return output(strings.Join(names, "\n"))
	}
	if !allowedFile(q.Path) {
		return ai.ToolOutput{}, fs.ErrPermission
	}
	parts := strings.Split(q.Path, "/")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[:i], "/")
		info, err := root.Lstat(parent)
		if os.IsNotExist(err) && q.Action == "write" {
			if err = root.Mkdir(parent, 0755); err != nil {
				return ai.ToolOutput{}, err
			}
			continue
		}
		if err != nil {
			return ai.ToolOutput{}, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ai.ToolOutput{}, fs.ErrPermission
		}
	}
	info, statErr := root.Lstat(q.Path)
	if statErr == nil {
		if !info.Mode().IsRegular() || info.Size() > 1<<20 || multipleLinks(info) {
			return ai.ToolOutput{}, fs.ErrPermission
		}
	} else if !os.IsNotExist(statErr) {
		return ai.ToolOutput{}, statErr
	}
	read := func() (string, error) {
		f, err := root.Open(q.Path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		if err != nil {
			return "", err
		}
		if len(b) > 1<<20 || !utf8.Valid(b) {
			return "", errors.New("asset is not bounded text")
		}
		return string(b), nil
	}
	switch q.Action {
	case "read":
		s, err := read()
		if err != nil {
			return ai.ToolOutput{}, err
		}
		return output(s)
	case "delete":
		if q.Path == "AGENTS.md" {
			return ai.ToolOutput{}, errors.New("behavior entry cannot be deleted")
		}
		if err := root.Remove(q.Path); err != nil {
			return ai.ToolOutput{}, err
		}
		return output("Deleted " + q.Path)
	case "edit":
		s, err := read()
		if err != nil {
			return ai.ToolOutput{}, err
		}
		if q.OldText == "" || strings.Count(s, q.OldText) != 1 {
			return ai.ToolOutput{}, errors.New("old_text must match exactly once")
		}
		q.Content = strings.Replace(s, q.OldText, q.Content, 1)
	case "write":
	default:
		return ai.ToolOutput{}, errors.New("unknown asset action")
	}
	if len(q.Content) > 1<<20 || !utf8.ValidString(q.Content) || strings.ContainsRune(q.Content, 0) {
		return ai.ToolOutput{}, errors.New("asset must be bounded UTF-8 text")
	}
	if err := ctx.Err(); err != nil {
		return ai.ToolOutput{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ai.ToolOutput{}, err
	}
	temp := path.Join(path.Dir(q.Path), ".author-"+hex.EncodeToString(nonce[:]))
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	defer root.Remove(temp)
	_, writeErr := io.WriteString(f, q.Content)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return ai.ToolOutput{}, err
	}
	if err := root.Rename(temp, q.Path); err != nil {
		return ai.ToolOutput{}, err
	}
	return output("Updated " + q.Path)
}
func multipleLinks(info fs.FileInfo) bool {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		n := v.FieldByName("Nlink")
		if n.IsValid() && n.CanUint() {
			return n.Uint() > 1
		}
	}
	return false
}
