package harness

import (
	"fmt"
	"os"
	"strings"
)

type patchFile struct {
	action string
	path   string
	lines  []string
}

func ApplyHarnessPatch(root, patch string) ([]map[string]string, error) {
	files, err := parsePatch(patch)
	if err != nil {
		return nil, err
	}
	var results []map[string]string
	for _, file := range files {
		path, err := ResolveInside(root, file.path)
		if err != nil {
			return nil, err
		}
		switch file.action {
		case "add":
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("cannot add existing file: %s", file.path)
			}
			var lines []string
			for _, line := range file.lines {
				if strings.HasPrefix(line, "+") {
					lines = append(lines, strings.TrimPrefix(line, "+"))
				}
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				return nil, err
			}
		case "delete":
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		case "update":
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			updated, err := applyUpdate(string(data), file.lines, file.path)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown patch action: %s", file.action)
		}
		results = append(results, map[string]string{"action": file.action, "path": file.path})
	}
	return results, nil
}

func parsePatch(patch string) ([]patchFile, error) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "*** Begin Patch" {
		return nil, fmt.Errorf("patch must start with *** Begin Patch")
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, fmt.Errorf("patch must end with *** End Patch")
	}
	var files []patchFile
	current := -1
	for _, line := range lines[1 : len(lines)-1] {
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			files = append(files, patchFile{action: "add", path: strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))})
			current = len(files) - 1
		case strings.HasPrefix(line, "*** Delete File: "):
			files = append(files, patchFile{action: "delete", path: strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))})
			current = len(files) - 1
		case strings.HasPrefix(line, "*** Update File: "):
			files = append(files, patchFile{action: "update", path: strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))})
			current = len(files) - 1
		default:
			if current >= 0 {
				files[current].lines = append(files[current].lines, line)
			}
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("patch has no file operations")
	}
	return files, nil
}

func applyUpdate(original string, lines []string, path string) (string, error) {
	hunks := splitHunks(lines)
	updated := original
	for _, hunk := range hunks {
		var oldLines, newLines []string
		for _, line := range hunk {
			if line == "" {
				oldLines = append(oldLines, "")
				newLines = append(newLines, "")
				continue
			}
			prefix, body := line[0], line[1:]
			switch prefix {
			case ' ':
				oldLines = append(oldLines, body)
				newLines = append(newLines, body)
			case '-':
				oldLines = append(oldLines, body)
			case '+':
				newLines = append(newLines, body)
			case '\\':
			default:
				return "", fmt.Errorf("invalid hunk line in %s: %s", path, line)
			}
		}
		oldText := strings.Join(oldLines, "\n")
		newText := strings.Join(newLines, "\n")
		if oldText == "" {
			updated += newText
			continue
		}
		if !strings.Contains(updated, oldText) {
			return "", fmt.Errorf("hunk did not match %s", path)
		}
		updated = strings.Replace(updated, oldText, newText, 1)
	}
	return updated, nil
}

func splitHunks(lines []string) [][]string {
	var hunks [][]string
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if len(current) > 0 {
				hunks = append(hunks, current)
				current = nil
			}
			continue
		}
		if strings.HasPrefix(line, "***") {
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		hunks = append(hunks, current)
	}
	return hunks
}
