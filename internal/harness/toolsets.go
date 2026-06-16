package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ToolsetRegistry struct {
	workspace Workspace
	skills    *SkillRegistry
	projects  ProjectRegistry
	handlers  map[string]func(context.Context, map[string]any) (any, error)
}

func NewToolsetRegistry(workspace Workspace, skills *SkillRegistry) *ToolsetRegistry {
	r := &ToolsetRegistry{workspace: workspace, skills: skills}
	r.handlers = map[string]func(context.Context, map[string]any) (any, error){
		"workspace.list_files":  r.workspaceListFiles,
		"workspace.read_file":   r.workspaceReadFile,
		"workspace.search":      r.workspaceSearch,
		"workspace.apply_patch": r.workspaceApplyPatch,
		"workspace.write_file":  r.workspaceWriteFile,
		"terminal.run":          r.terminalRun,
		"git.status":            r.gitStatus,
		"git.diff":              r.gitDiff,
		"git.log":               r.gitLog,
		"git.show":              r.gitShow,
		"project.list":          r.projectList,
		"project.current":       r.projectCurrent,
		"skill.list":            r.skillList,
		"skill.use":             r.skillUse,
		"mcp.list":              r.mcpList,
		"mcp.call":              r.mcpCall,
	}
	return r
}

func (r *ToolsetRegistry) Catalog() map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for name := range r.handlers {
		parts := strings.SplitN(name, ".", 2)
		out[parts[0]] = append(out[parts[0]], map[string]any{"name": parts[1]})
	}
	return out
}

func (r *ToolsetRegistry) Execute(ctx context.Context, call HarnessCall) (observation Observation) {
	callID := fmt.Sprintf("call-%d-%d", call.Index, time.Now().UnixNano())
	defer func() {
		if recovered := recover(); recovered != nil {
			observation = Observation{
				CallID: callID,
				Tool:   call.Tool,
				Status: "error",
				Error:  fmt.Sprint(recovered),
			}
		}
	}()
	handler, ok := r.handlers[call.Tool]
	if !ok {
		return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: "unknown tool"}
	}
	result, err := handler(ctx, call.Args)
	if err != nil {
		return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: err.Error()}
	}
	return Observation{CallID: callID, Tool: call.Tool, Status: "ok", Result: result}
}

func (r *ToolsetRegistry) workspaceListFiles(ctx context.Context, args map[string]any) (any, error) {
	start, err := ResolveInside(r.workspace.Root, getString(args, "path", "."))
	if err != nil {
		return nil, err
	}
	recursive := getBool(args, "recursive", false)
	pattern := getString(args, "glob", "*")
	maxEntries := getInt(args, "max_entries", 200)
	var entries []map[string]any
	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil || strings.Contains(filepath.ToSlash(path), "/.git/") {
			return nil
		}
		if path == start {
			return nil
		}
		rel := Rel(r.workspace.Root, path)
		match, _ := filepath.Match(pattern, filepath.Base(path))
		if !match && pattern != "*" {
			globMatch, _ := filepath.Match(filepath.ToSlash(pattern), filepath.ToSlash(rel))
			if !globMatch {
				return nil
			}
		}
		typ := "file"
		var size any = info.Size()
		if info.IsDir() {
			typ = "dir"
			size = nil
		}
		entries = append(entries, map[string]any{"path": rel, "type": typ, "size": size})
		if len(entries) >= maxEntries {
			return filepath.SkipAll
		}
		if info.IsDir() && !recursive {
			return filepath.SkipDir
		}
		return nil
	}
	info, err := os.Stat(start)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", getString(args, "path", "."))
	}
	_ = filepath.Walk(start, walk)
	return map[string]any{"root": r.workspace.Root, "entries": entries, "truncated": len(entries) >= maxEntries}, nil
}

func (r *ToolsetRegistry) workspaceReadFile(ctx context.Context, args map[string]any) (any, error) {
	path, err := ResolveInside(r.workspace.Root, mustString(args, "path"))
	if err != nil {
		return nil, err
	}
	if IsSensitive(path) {
		return nil, fmt.Errorf("refusing to read sensitive path: %s", Rel(r.workspace.Root, path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsBinary(data) {
		return map[string]any{"path": Rel(r.workspace.Root, path), "type": "binary", "size": len(data)}, nil
	}
	offset := getInt(args, "offset", 0)
	maxBytes := getInt(args, "max_bytes", 120000)
	if offset > len(data) {
		offset = len(data)
	}
	end := min(len(data), offset+maxBytes)
	return map[string]any{
		"path":      Rel(r.workspace.Root, path),
		"type":      "text",
		"size":      len(data),
		"offset":    offset,
		"truncated": end < len(data),
		"content":   string(data[offset:end]),
	}, nil
}

func (r *ToolsetRegistry) workspaceSearch(ctx context.Context, args map[string]any) (any, error) {
	pattern := mustString(args, "pattern")
	glob := getString(args, "glob", "**/*")
	regex := getBool(args, "regex", false)
	maxMatches := getInt(args, "max_matches", 80)
	var re *regexp.Regexp
	var err error
	if regex {
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
	}
	var matches []map[string]any
	_ = filepath.Walk(r.workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || strings.Contains(filepath.ToSlash(path), "/.git/") {
			return nil
		}
		if len(matches) >= maxMatches {
			return filepath.SkipAll
		}
		rel := Rel(r.workspace.Root, path)
		if ok, _ := filepath.Match(filepath.ToSlash(glob), filepath.ToSlash(rel)); !ok && glob != "**/*" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || IsBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			ok := strings.Contains(line, pattern)
			if regex {
				ok = re.MatchString(line)
			}
			if ok {
				matches = append(matches, map[string]any{"path": rel, "line": i + 1, "text": truncate(line, 500)})
				if len(matches) >= maxMatches {
					break
				}
			}
		}
		return nil
	})
	return map[string]any{"matches": matches, "truncated": len(matches) >= maxMatches}, nil
}

func (r *ToolsetRegistry) workspaceApplyPatch(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Mode != ModeWork {
		return nil, fmt.Errorf("workspace.apply_patch requires mode=work")
	}
	results, err := ApplyHarnessPatch(r.workspace.Root, mustString(args, "patch"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"applied": results}, nil
}

func (r *ToolsetRegistry) workspaceWriteFile(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Mode != ModeWork {
		return nil, fmt.Errorf("workspace.write_file requires mode=work")
	}
	path, err := ResolveInside(r.workspace.Root, mustString(args, "path"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil && !getBool(args, "overwrite", false) {
		return nil, fmt.Errorf("file exists and overwrite=false: %s", Rel(r.workspace.Root, path))
	}
	content := getString(args, "content", "")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": Rel(r.workspace.Root, path), "bytes": len([]byte(content))}, nil
}

func (r *ToolsetRegistry) terminalRun(ctx context.Context, args map[string]any) (any, error) {
	command := mustString(args, "command")
	if looksDestructive(command) && !getBool(args, "allow_destructive", false) {
		return nil, fmt.Errorf("command appears destructive")
	}
	cwd, err := ResolveInside(r.workspace.Root, getString(args, "cwd", "."))
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(getInt(args, "timeout_ms", 30000)) * time.Millisecond
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, command)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		code = 1
		var exitErr *exec.ExitError
		if ok := errorAs(err, &exitErr); ok {
			code = exitErr.ExitCode()
		}
	}
	return map[string]any{
		"command":    command,
		"cwd":        Rel(r.workspace.Root, cwd),
		"returncode": code,
		"stdout":     tail(stdout.String(), 20000),
		"stderr":     tail(stderr.String(), 20000),
	}, nil
}

func (r *ToolsetRegistry) gitStatus(ctx context.Context, args map[string]any) (any, error) {
	return r.git(ctx, []string{"status", "--short", "--branch"})
}

func (r *ToolsetRegistry) gitDiff(ctx context.Context, args map[string]any) (any, error) {
	cmd := []string{"diff"}
	if getBool(args, "cached", false) {
		cmd = append(cmd, "--cached")
	}
	cmd = append(cmd, "--")
	return r.git(ctx, cmd)
}

func (r *ToolsetRegistry) gitLog(ctx context.Context, args map[string]any) (any, error) {
	return r.git(ctx, []string{"log", fmt.Sprintf("-%d", getInt(args, "limit", 10)), "--oneline", "--decorate"})
}

func (r *ToolsetRegistry) gitShow(ctx context.Context, args map[string]any) (any, error) {
	return r.git(ctx, []string{"show", "--stat", "--oneline", "--decorate", getString(args, "rev", "HEAD")})
}

func (r *ToolsetRegistry) projectList(ctx context.Context, args map[string]any) (any, error) {
	projects, err := r.projects.List()
	return map[string]any{"projects": projects}, err
}

func (r *ToolsetRegistry) projectCurrent(ctx context.Context, args map[string]any) (any, error) {
	return r.workspace, nil
}

func (r *ToolsetRegistry) skillList(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"skills": r.skills.List()}, nil
}

func (r *ToolsetRegistry) skillUse(ctx context.Context, args map[string]any) (any, error) {
	skill, err := r.skills.Get(mustString(args, "name"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"skill": skill, "content": skill.content}, nil
}

func (r *ToolsetRegistry) mcpList(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"servers": []any{}, "note": "External MCP registry is reserved for the next implementation phase."}, nil
}

func (r *ToolsetRegistry) mcpCall(ctx context.Context, args map[string]any) (any, error) {
	return nil, fmt.Errorf("external MCP calls are not implemented in this MVP")
}

func (r *ToolsetRegistry) git(ctx context.Context, args []string) (any, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = r.workspace.Root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		var exitErr *exec.ExitError
		if ok := errorAs(err, &exitErr); ok {
			code = exitErr.ExitCode()
		}
	}
	return map[string]any{"returncode": code, "stdout": tail(stdout.String(), 20000), "stderr": tail(stderr.String(), 20000)}, nil
}

func getString(args map[string]any, key, fallback string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return fallback
}

func mustString(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok || value == "" {
		panic(fmt.Sprintf("missing string arg: %s", key))
	}
	return value
}

func getBool(args map[string]any, key string, fallback bool) bool {
	if value, ok := args[key].(bool); ok {
		return value
	}
	return fallback
}

func getInt(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}

func looksDestructive(command string) bool {
	lower := " " + strings.ToLower(command) + " "
	for _, token := range []string{"rm -rf", "remove-item", " del ", " rmdir ", "git reset", "git clean", "git push --force", "format "} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if os.PathSeparator == '\\' {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **exec.ExitError:
		if v, ok := err.(*exec.ExitError); ok {
			*t = v
			return true
		}
	}
	return false
}
