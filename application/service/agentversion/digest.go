package agentversion

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

var ErrInvalidDigestInput = errors.New("invalid digest input")

type Entry struct {
	Path          string `json:"path"`
	Mode          string `json:"mode"`
	Size          int64  `json:"size"`
	ContentSHA256 string `json:"content_sha256"`
}

func BundleDigest(entries []Entry) (string, error) {
	ordered := slices.Clone(entries)
	slices.SortFunc(ordered, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	for i, e := range ordered {
		if !validEntryPath(e.Path) || (e.Mode != "100644" && e.Mode != "100755" && e.Mode != "120000") || e.Size < 0 || !validSHA256(e.ContentSHA256) || (i > 0 && ordered[i-1].Path == e.Path) {
			return "", ErrInvalidDigestInput
		}
	}
	encoded, err := canonicalMarshal(struct {
		DigestVersion int     `json:"digest_version"`
		Entries       []Entry `json:"entries"`
	}{1, ordered})
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func SpecDigest(bundle string, snapshot Snapshot) (string, error) {
	if !validSHA256(bundle) || snapshot.validate() != nil {
		return "", ErrInvalidDigestInput
	}
	encoded, err := canonicalMarshal(struct {
		DigestVersion int           `json:"digest_version"`
		BundleDigest  string        `json:"bundle_digest"`
		Model         ModelConfig   `json:"model"`
		Tools         ToolPolicy    `json:"tools"`
		Runtime       RuntimeConfig `json:"runtime"`
	}{1, bundle, snapshot.Model, snapshot.Tools, snapshot.Runtime})
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func canonicalMarshal(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonicalizeJSON(raw)
}
func canonicalizeJSON(raw []byte) ([]byte, error) { return jsoncanonicalizer.Transform(raw) }
func digestBytes(data []byte) string              { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }
func validSHA256(v string) bool {
	if len(v) != 71 || !strings.HasPrefix(v, "sha256:") {
		return false
	}
	for _, c := range v[7:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func validEntryPath(v string) bool {
	return v != "" && utf8.ValidString(v) && !strings.ContainsRune(v, 0) && !strings.HasPrefix(v, "/") && path.Clean(v) == v && v != "." && !strings.HasPrefix(v, "../")
}
