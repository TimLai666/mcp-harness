package harness

import (
	"os"
	"regexp"
	"strings"
)

var refRE = regexp.MustCompile(`@(?:"([^"]+)"|([^\s,，。;；:：)）\]}]+))`)

func FindReferences(message string) []string {
	matches := refRE.FindAllStringSubmatch(message, -1)
	seen := map[string]bool{}
	var refs []string
	for _, match := range matches {
		ref := match[1]
		if ref == "" {
			ref = match[2]
		}
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}

func ResolveReferences(message string, workspace Workspace, maxInlineBytes int64) []ReferencedFile {
	var out []ReferencedFile
	for _, ref := range FindReferences(message) {
		label := "@" + ref
		if strings.Contains(ref, " ") {
			label = "@\"" + ref + "\""
		}
		path, err := ResolveInside(workspace.Root, ref)
		if err != nil {
			out = append(out, ReferencedFile{Ref: label, Path: ref, Type: "unknown", Error: err.Error()})
			continue
		}
		rel := Rel(workspace.Root, path)
		info, err := os.Stat(path)
		if err != nil {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "unknown", Error: "not_found"})
			continue
		}
		if info.IsDir() {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "directory", Size: info.Size()})
			continue
		}
		if IsSensitive(path) {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "text", Error: "sensitive_path"})
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "unknown", Error: err.Error()})
			continue
		}
		if IsBinary(data) {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "binary", Size: int64(len(data))})
			continue
		}
		if int64(len(data)) > maxInlineBytes {
			out = append(out, ReferencedFile{Ref: label, Path: rel, Type: "text", Size: int64(len(data))})
			continue
		}
		out = append(out, ReferencedFile{
			Ref:      label,
			Path:     rel,
			Type:     "text",
			Complete: true,
			Content:  string(data),
			Size:     int64(len(data)),
		})
	}
	return out
}
