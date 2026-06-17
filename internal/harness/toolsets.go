package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolsetRegistry struct {
	workspace Workspace
	skills    *SkillRegistry
	projects  ProjectRegistry
	sessionID string
	access    AccessMode
	approval  ApprovalStore
	schemas   map[string]ToolSchema
	handlers  map[string]func(context.Context, map[string]any) (any, error)
}

// NewToolsetRegistry builds the local tool registry. access is the server-side
// permission policy resolved by the operator, not anything the agent supplies.
func NewToolsetRegistry(workspace Workspace, skills *SkillRegistry, sessionID string, access AccessMode) *ToolsetRegistry {
	if access == "" {
		access = AccessDefault
	}
	r := &ToolsetRegistry{workspace: workspace, skills: skills, sessionID: sessionID, access: access, schemas: BuiltinToolSchemas()}
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
		"project.add":           r.projectAdd,
		"project.create":        r.projectCreate,
		"project.clone":         r.projectClone,
		"project.rename":        r.projectRename,
		"project.relocate":      r.projectRelocate,
		"project.remove":        r.projectRemove,
		"skill.list":            r.skillList,
		"skill.use":             r.skillUse,
		"mcp.list":              r.mcpList,
		"mcp.call":              r.mcpCall,
		"mcp.add":               r.mcpAdd,
		"mcp.remove":            r.mcpRemove,
		"history.list":          r.historyList,
		"history.show":          r.historyShow,
		"history.restore":       r.historyRestore,
	}
	return r
}

func (r *ToolsetRegistry) Catalog() map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for name := range r.handlers {
		parts := strings.SplitN(name, ".", 2)
		item := map[string]any{"name": parts[1]}
		if schema, ok := r.schemas[name]; ok {
			item["description"] = schema.Description
			item["args"] = schema.Args
		}
		out[parts[0]] = append(out[parts[0]], item)
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
	if !r.toolsetAllowed(call.Tool) {
		return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: "toolset is not allowed for this project"}
	}
	if err := ValidateToolArgs(call.Tool, call.Args, r.schemas); err != nil {
		return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: "invalid args: " + err.Error()}
	}
	if reason := r.approvalReason(call); reason != "" {
		if r.access == AccessFullAccess {
			// Operator policy grants full access; execute and record the result.
		} else if approvalID := getString(call.Args, "approval_id", ""); approvalID == "" || !r.approval.IsApproved(approvalID, r.sessionID, call.Tool, call.Args) {
			record, err := r.approval.Create(r.sessionID, r.workspace, call, reason)
			if err != nil {
				return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: err.Error()}
			}
			return Observation{
				CallID: callID,
				Tool:   call.Tool,
				Status: "approval_required",
				Result: map[string]any{
					"approval": record,
					"message":  "Operation queued for Web UI approval. After an operator approves it there, call the same tool again with approval_id set to the returned approval id.",
				},
			}
		}
	}
	result, err := handler(ctx, call.Args)
	if err != nil {
		return Observation{CallID: callID, Tool: call.Tool, Status: "error", Error: err.Error()}
	}
	return Observation{CallID: callID, Tool: call.Tool, Status: "ok", Result: result}
}

func (r *ToolsetRegistry) toolsetAllowed(tool string) bool {
	if r.workspace.Project == nil || len(r.workspace.Project.AllowedToolsets) == 0 {
		return true
	}
	toolset, _, ok := strings.Cut(tool, ".")
	if !ok {
		return false
	}
	for _, allowed := range r.workspace.Project.AllowedToolsets {
		if allowed == toolset {
			return true
		}
	}
	return false
}

func (r *ToolsetRegistry) approvalReason(call HarnessCall) string {
	switch call.Tool {
	case "workspace.apply_patch", "workspace.write_file":
		return "File mutation requires approval."
	case "history.restore":
		return "Restoring a workspace version modifies files and requires approval."
	case "project.add", "project.create", "project.clone", "project.rename", "project.relocate":
		return "Changing project registry or creating harness-managed workspaces requires approval."
	case "project.remove":
		return "Removing a project (and optionally deleting its files) requires approval."
	case "mcp.add", "mcp.remove":
		return "Changing MCP server configuration requires approval."
	case "mcp.call":
		serverID := getString(call.Args, "server", "")
		config, err := FindMCPServer(serverID)
		if err != nil || !config.Trusted {
			return "Calling an untrusted external MCP server requires approval."
		}
	case "terminal.run":
		if looksDestructive(getString(call.Args, "command", "")) {
			return "Destructive terminal command requires approval."
		}
	}
	return ""
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
	cwd, err := ResolveInside(r.workspace.Root, getString(args, "cwd", "."))
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(getInt(args, "timeout_ms", 30000)) * time.Millisecond
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, command)
	cmd.Dir = cwd
	callID := CallIDFromContext(ctx)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go r.streamOutput(&wg, stdoutPipe, &stdout, callID, command, "stdout")
	go r.streamOutput(&wg, stderrPipe, &stderr, callID, command, "stderr")
	wg.Wait()

	err = cmd.Wait()
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

// streamOutput copies a command pipe into the result buffer while broadcasting
// each chunk to subscribers so the Web UI sees terminal output as it happens.
func (r *ToolsetRegistry) streamOutput(wg *sync.WaitGroup, reader io.Reader, buf *bytes.Buffer, callID, command, stream string) {
	defer wg.Done()
	chunk := make([]byte, 4096)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			data := string(chunk[:n])
			buf.WriteString(data)
			PublishTerminalOutput(callID, r.sessionID, r.workspace, command, stream, data)
		}
		if err != nil {
			return
		}
	}
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

func (r *ToolsetRegistry) projectAdd(ctx context.Context, args map[string]any) (any, error) {
	project, err := r.projects.AddWithAllowedToolsets(
		mustString(args, "path"),
		getString(args, "name", ""),
		getString(args, "project_id", ""),
		getString(args, "description", ""),
		Mode(getString(args, "default_mode", "")),
		stringSliceArg(args, "allowed_toolsets"),
	)
	if err != nil {
		return nil, err
	}
	PublishProjectChange("added", &project)
	return map[string]any{"project": project}, nil
}

func (r *ToolsetRegistry) projectCreate(ctx context.Context, args map[string]any) (any, error) {
	project, err := r.projects.CreateWorkspace(
		mustString(args, "name"),
		getString(args, "project_id", ""),
		getString(args, "description", ""),
		Mode(getString(args, "default_mode", "")),
		stringSliceArg(args, "allowed_toolsets"),
	)
	if err != nil {
		return nil, err
	}
	PublishProjectChange("created", &project)
	return map[string]any{"project": project, "workspaces_root": filepath.Dir(project.Path)}, nil
}

func (r *ToolsetRegistry) projectClone(ctx context.Context, args map[string]any) (any, error) {
	timeout := time.Duration(getInt(args, "timeout_ms", 120000)) * time.Millisecond
	result, err := r.projects.CloneWorkspace(
		ctx,
		mustString(args, "repo_url"),
		getString(args, "branch", ""),
		getString(args, "name", ""),
		getString(args, "project_id", ""),
		getString(args, "description", ""),
		Mode(getString(args, "default_mode", "")),
		stringSliceArg(args, "allowed_toolsets"),
		getInt(args, "depth", 0),
		timeout,
	)
	if err != nil {
		return result, err
	}
	PublishProjectChange("cloned", &result.Project)
	return result, nil
}

func (r *ToolsetRegistry) projectRename(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Project == nil {
		return nil, fmt.Errorf("no project selected to rename; pass project as the target")
	}
	project, err := r.projects.Rename(r.workspace.Project.ID, mustString(args, "name"), getString(args, "description", ""))
	if err != nil {
		return nil, err
	}
	PublishProjectChange("renamed", &project)
	return map[string]any{"project": project}, nil
}

func (r *ToolsetRegistry) projectRelocate(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Project == nil {
		return nil, fmt.Errorf("no project selected to relocate; pass project as the target")
	}
	project, err := r.projects.Relocate(r.workspace.Project.ID, mustString(args, "path"))
	if err != nil {
		return nil, err
	}
	PublishProjectChange("relocated", &project)
	return map[string]any{"project": project}, nil
}

func (r *ToolsetRegistry) projectRemove(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Project == nil {
		return nil, fmt.Errorf("no project selected to remove; pass project as the target")
	}
	project, filesDeleted, err := r.projects.Remove(r.workspace.Project.ID, getBool(args, "delete_files", false))
	if err != nil {
		return nil, err
	}
	PublishProjectChange("removed", &project)
	return map[string]any{"removed": project.ID, "project": project, "files_deleted": filesDeleted}, nil
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
	servers, err := LoadMCPServers()
	if err != nil {
		return nil, err
	}
	result := map[string]any{"servers": servers}
	if !getBool(args, "probe", false) {
		return result, nil
	}
	toolsByServer := map[string]any{}
	for _, server := range servers {
		tools, err := r.listMCPTools(ctx, server, MCPTimeout(args))
		if err != nil {
			toolsByServer[server.ID] = map[string]any{"error": err.Error()}
			continue
		}
		toolsByServer[server.ID] = tools
	}
	result["tools"] = toolsByServer
	return result, nil
}

func (r *ToolsetRegistry) mcpCall(ctx context.Context, args map[string]any) (any, error) {
	serverID := mustString(args, "server")
	tool := mustString(args, "tool")
	arguments, _ := args["arguments"].(map[string]any)
	if arguments == nil {
		arguments = map[string]any{}
	}
	config, err := FindMCPServer(serverID)
	if err != nil {
		return nil, err
	}
	timeout := MCPTimeout(args)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport, err := MCPTransport(config)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-harness", Version: "0.1.0"}, nil)
	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	tools, err := session.ListTools(cctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	var target *mcp.Tool
	for _, candidate := range tools.Tools {
		if candidate.Name == tool {
			target = candidate
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("external MCP tool not found: %s.%s", serverID, tool)
	}
	if err := ValidateExternalMCPArgs(target.InputSchema, arguments); err != nil {
		return nil, fmt.Errorf("invalid external MCP args for %s.%s: %w", serverID, tool, err)
	}
	res, err := session.CallTool(cctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (r *ToolsetRegistry) mcpAdd(ctx context.Context, args map[string]any) (any, error) {
	config := MCPServerConfig{
		ID:        mustString(args, "id"),
		Name:      getString(args, "name", mustString(args, "id")),
		Transport: getString(args, "transport", "stdio"),
		Command:   getString(args, "command", ""),
		Endpoint:  getString(args, "endpoint", ""),
		Trusted:   getBool(args, "trusted", false),
	}
	if rawArgs, ok := args["args"].([]any); ok {
		for _, arg := range rawArgs {
			config.Args = append(config.Args, fmt.Sprint(arg))
		}
	}
	if env, ok := args["env"].(map[string]any); ok {
		config.Env = map[string]string{}
		for key, value := range env {
			config.Env[key] = fmt.Sprint(value)
		}
	}
	if err := AddMCPServer(config); err != nil {
		return nil, err
	}
	return map[string]any{"server": config}, nil
}

func (r *ToolsetRegistry) mcpRemove(ctx context.Context, args map[string]any) (any, error) {
	id := mustString(args, "id")
	if err := DeleteMCPServer(id); err != nil {
		return nil, err
	}
	return map[string]any{"removed": id}, nil
}

func (r *ToolsetRegistry) historyList(ctx context.Context, args map[string]any) (any, error) {
	projectID := getString(args, "project_id", "")
	if projectID == "" && getBool(args, "current_project", false) && r.workspace.Project != nil {
		projectID = r.workspace.Project.ID
	}
	events, err := ListHistoryEvents(projectID, getString(args, "session_id", ""), getInt(args, "limit", 50), getBool(args, "include_diff", false))
	return map[string]any{"events": events}, err
}

func (r *ToolsetRegistry) historyShow(ctx context.Context, args map[string]any) (any, error) {
	event, err := GetHistoryEvent(mustString(args, "event_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": event}, nil
}

func (r *ToolsetRegistry) historyRestore(ctx context.Context, args map[string]any) (any, error) {
	if r.workspace.Mode != ModeWork {
		return nil, fmt.Errorf("history.restore requires mode=work")
	}
	version, diff, truncated, err := RestoreWorkspaceVersion(r.workspace.Root, mustString(args, "version_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"restored_version": version.ID, "label": version.Label, "diff": diff, "diff_truncated": truncated}, nil
}

func (r *ToolsetRegistry) listMCPTools(ctx context.Context, config MCPServerConfig, timeout time.Duration) (any, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport, err := MCPTransport(config)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-harness", Version: "0.1.0"}, nil)
	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	res, err := session.ListTools(cctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(res.Tools)
	if err != nil {
		return res.Tools, nil
	}
	var tools []map[string]any
	if err := json.Unmarshal(data, &tools); err != nil {
		return res.Tools, nil
	}
	return tools, nil
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

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		out = append(out, fmt.Sprint(value))
	}
	return out
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
