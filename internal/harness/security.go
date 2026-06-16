package harness

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveInside(root, requested string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var candidate string
	if filepath.IsAbs(requested) {
		candidate = requested
	} else {
		candidate = filepath.Join(rootAbs, requested)
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes workspace root: %s", requested)
	}
	return resolved, nil
}

func Rel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func IsSensitive(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case ".env", ".env.local", ".env.production", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		if part == ".ssh" {
			return true
		}
	}
	return false
}

func IsBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.Contains(data, []byte{0}) {
		return true
	}
	limit := min(len(data), 4096)
	text := 0
	for _, b := range data[:limit] {
		if b == '\t' || b == '\n' || b == '\r' || b >= 32 {
			text++
		}
	}
	return float64(text)/float64(limit) < 0.75
}
