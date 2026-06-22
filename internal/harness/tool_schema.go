package harness

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type ArgType string

const (
	ArgString ArgType = "string"
	ArgBool   ArgType = "bool"
	ArgInt    ArgType = "int"
	ArgObject ArgType = "object"
	ArgArray  ArgType = "array"
)

type ArgSchema struct {
	Type        ArgType `json:"type"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
}

type ToolSchema struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Args        map[string]ArgSchema `json:"args,omitempty"`
}

func BuiltinToolSchemas() map[string]ToolSchema {
	return map[string]ToolSchema{
		"workspace.list_files": {
			Description: "List files under the current workspace.",
			Args: map[string]ArgSchema{
				"path":        {Type: ArgString, Description: "Workspace-relative directory. Defaults to ."},
				"recursive":   {Type: ArgBool},
				"glob":        {Type: ArgString},
				"max_entries": {Type: ArgInt},
			},
		},
		"workspace.read_file": {
			Description: "Read a workspace file with bounded output. Returns numbered_content (line-numbered) so edits can target exact lines.",
			Args: map[string]ArgSchema{
				"path":         {Type: ArgString, Required: true},
				"offset":       {Type: ArgInt},
				"max_bytes":    {Type: ArgInt},
				"line_numbers": {Type: ArgBool},
			},
		},
		"workspace.replace_lines": {
			Description: "Replace an inclusive 1-based line range with new content. Edits files in fragments instead of rewriting the whole file. Read the file first to get line numbers.",
			Args: map[string]ArgSchema{
				"path":       {Type: ArgString, Required: true},
				"start_line": {Type: ArgInt, Required: true},
				"end_line":   {Type: ArgInt, Required: true},
				"content":    {Type: ArgString},
			},
		},
		"workspace.search": {
			Description: "Search text files in the workspace.",
			Args: map[string]ArgSchema{
				"pattern":     {Type: ArgString, Required: true},
				"glob":        {Type: ArgString},
				"regex":       {Type: ArgBool},
				"max_matches": {Type: ArgInt},
			},
		},
		"workspace.grep": {
			Description: "Fast text search using ripgrep. More powerful than workspace.search — supports glob/type filters, context lines, regex, and fixed strings.",
			Args: map[string]ArgSchema{
				"pattern":          {Type: ArgString, Required: true, Description: "Search pattern (regex by default, or literal with fixed_strings)."},
				"path":             {Type: ArgString, Description: "Directory or file to search, relative to workspace root. Defaults to ."},
				"glob":             {Type: ArgString, Description: "Glob filter, e.g. \"*.go\" or \"!*.test.*\". Can be comma-separated for multiple."},
				"file_type":        {Type: ArgString, Description: "File type filter, e.g. \"go\", \"js\", \"py\". Comma-separated for multiple."},
				"case_insensitive": {Type: ArgBool, Description: "Case-insensitive search (-i)."},
				"fixed_strings":    {Type: ArgBool, Description: "Treat pattern as a literal string, not regex (-F)."},
				"context":          {Type: ArgInt, Description: "Lines of context around each match (-C)."},
				"max_matches":      {Type: ArgInt, Description: "Maximum total matches to return. Defaults to 200."},
				"include_hidden":   {Type: ArgBool, Description: "Search hidden files and directories."},
				"multiline":        {Type: ArgBool, Description: "Enable multiline matching (-U)."},
				"word":             {Type: ArgBool, Description: "Match whole words only (-w)."},
				"invert":           {Type: ArgBool, Description: "Show lines that do NOT match (-v)."},
			},
		},
		"workspace.apply_patch": {
			Description: "Apply a harness patch in work mode.",
			Args: map[string]ArgSchema{
				"patch": {Type: ArgString, Required: true},
			},
		},
		"workspace.write_file": {
			Description: "Write a workspace file in work mode.",
			Args: map[string]ArgSchema{
				"path":      {Type: ArgString, Required: true},
				"content":   {Type: ArgString},
				"overwrite": {Type: ArgBool},
			},
		},
		"terminal.run": {
			Description: "Run a shell command within the workspace.",
			Args: map[string]ArgSchema{
				"command":    {Type: ArgString, Required: true},
				"cwd":        {Type: ArgString},
				"timeout_ms": {Type: ArgInt},
			},
		},
		"git.status": {Description: "Run git status."},
		"git.diff": {
			Description: "Run git diff.",
			Args: map[string]ArgSchema{
				"cached": {Type: ArgBool},
			},
		},
		"git.log": {
			Description: "Show recent git commits.",
			Args: map[string]ArgSchema{
				"limit": {Type: ArgInt},
			},
		},
		"git.show": {
			Description: "Show one git revision.",
			Args: map[string]ArgSchema{
				"rev": {Type: ArgString},
			},
		},
		"git.add": {
			Description: "Stage files for the next commit.",
			Args: map[string]ArgSchema{
				"paths": {Type: ArgArray, Required: true, Description: "File paths to stage. Use [\".\"] to stage all."},
			},
		},
		"git.commit": {
			Description: "Commit staged changes.",
			Args: map[string]ArgSchema{
				"message": {Type: ArgString, Required: true, Description: "Commit message."},
				"all":     {Type: ArgBool, Description: "Stage all modified tracked files before committing (-a)."},
			},
		},
		"git.checkout": {
			Description: "Switch branches or create a new branch.",
			Args: map[string]ArgSchema{
				"ref":    {Type: ArgString, Required: true, Description: "Branch name or commit to check out."},
				"create": {Type: ArgBool, Description: "Create a new branch (-b)."},
			},
		},
		"git.branch": {
			Description: "List, create, or delete branches.",
			Args: map[string]ArgSchema{
				"name":   {Type: ArgString, Description: "Branch name to create or delete. Omit to list."},
				"delete": {Type: ArgBool, Description: "Delete the named branch. Requires approval."},
				"all":    {Type: ArgBool, Description: "List remote-tracking branches too."},
			},
		},
		"git.fetch": {
			Description: "Download objects and refs from a remote.",
			Args: map[string]ArgSchema{
				"remote":     {Type: ArgString, Description: "Remote name. Defaults to origin."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"git.pull": {
			Description: "Fetch and integrate changes from a remote.",
			Args: map[string]ArgSchema{
				"remote":     {Type: ArgString, Description: "Remote name."},
				"branch":     {Type: ArgString, Description: "Remote branch."},
				"ff_only":    {Type: ArgBool, Description: "Only fast-forward (--ff-only)."},
				"rebase":     {Type: ArgBool, Description: "Rebase instead of merge (--rebase)."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"git.push": {
			Description: "Push commits to a remote repository. Requires approval.",
			Args: map[string]ArgSchema{
				"remote":       {Type: ArgString, Description: "Remote name. Defaults to origin."},
				"branch":       {Type: ArgString, Description: "Branch to push."},
				"set_upstream": {Type: ArgBool, Description: "Set upstream tracking (-u)."},
				"force":        {Type: ArgBool, Description: "Force push (--force). Destructive."},
				"timeout_ms":   {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"git.merge": {
			Description: "Merge a branch into the current branch, or abort/continue a merge in progress.",
			Args: map[string]ArgSchema{
				"branch":   {Type: ArgString, Description: "Branch to merge. Required unless abort or continue is set."},
				"message":  {Type: ArgString, Description: "Merge commit message."},
				"no_ff":    {Type: ArgBool, Description: "Create a merge commit even for fast-forward merges."},
				"abort":    {Type: ArgBool, Description: "Abort a merge in progress and restore pre-merge state."},
				"continue": {Type: ArgBool, Description: "Continue a merge after resolving conflicts (same as git merge --continue)."},
			},
		},
		"git.reset": {
			Description: "Unstage files or reset the current branch. Hard mode requires approval.",
			Args: map[string]ArgSchema{
				"paths": {Type: ArgArray, Description: "Paths to unstage. If omitted, resets HEAD."},
				"mode":  {Type: ArgString, Description: "Reset mode: soft, mixed (default), or hard."},
				"ref":   {Type: ArgString, Description: "Target ref to reset to. Defaults to HEAD."},
			},
		},
		"git.stash": {
			Description: "Stash or restore uncommitted changes.",
			Args: map[string]ArgSchema{
				"action":  {Type: ArgString, Required: true, Description: "Action: push, pop, list, apply, or drop."},
				"message": {Type: ArgString, Description: "Stash message (for push)."},
				"index":   {Type: ArgInt, Description: "Stash index (for pop, apply, drop). Defaults to 0."},
			},
		},
		"git.tag": {
			Description: "List or create tags.",
			Args: map[string]ArgSchema{
				"name":    {Type: ArgString, Description: "Tag name to create. Omit to list."},
				"message": {Type: ArgString, Description: "Tag message (creates annotated tag)."},
				"ref":     {Type: ArgString, Description: "Ref to tag. Defaults to HEAD."},
			},
		},
		"github.pr_create": {
			Description: "Create a pull request on GitHub. Requires approval.",
			Args: map[string]ArgSchema{
				"title":      {Type: ArgString, Required: true, Description: "PR title."},
				"body":       {Type: ArgString, Description: "PR body/description."},
				"base":       {Type: ArgString, Description: "Base branch."},
				"head":       {Type: ArgString, Description: "Head branch. Defaults to current."},
				"draft":      {Type: ArgBool, Description: "Create as draft PR."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.pr_list": {
			Description: "List pull requests on GitHub.",
			Args: map[string]ArgSchema{
				"state":      {Type: ArgString, Description: "Filter: open, closed, merged, all."},
				"limit":      {Type: ArgInt, Description: "Max PRs to list."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.pr_view": {
			Description: "View a pull request on GitHub.",
			Args: map[string]ArgSchema{
				"number":     {Type: ArgInt, Required: true, Description: "PR number."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.pr_merge": {
			Description: "Merge a pull request on GitHub. Requires approval.",
			Args: map[string]ArgSchema{
				"number":     {Type: ArgInt, Required: true, Description: "PR number."},
				"method":     {Type: ArgString, Description: "Merge method: merge, squash, or rebase."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.issue_list": {
			Description: "List issues on GitHub.",
			Args: map[string]ArgSchema{
				"state":      {Type: ArgString, Description: "Filter: open, closed, all."},
				"labels":     {Type: ArgString, Description: "Comma-separated label filter."},
				"limit":      {Type: ArgInt, Description: "Max issues to list."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.issue_create": {
			Description: "Create an issue on GitHub. Requires approval.",
			Args: map[string]ArgSchema{
				"title":      {Type: ArgString, Required: true, Description: "Issue title."},
				"body":       {Type: ArgString, Description: "Issue body."},
				"labels":     {Type: ArgString, Description: "Comma-separated labels."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.issue_view": {
			Description: "View an issue on GitHub.",
			Args: map[string]ArgSchema{
				"number":     {Type: ArgInt, Required: true, Description: "Issue number."},
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"github.repo_view": {
			Description: "View current repository information on GitHub.",
			Args: map[string]ArgSchema{
				"timeout_ms": {Type: ArgInt, Description: "Timeout in milliseconds."},
			},
		},
		"project.list":    {Description: "List configured projects."},
		"project.current": {Description: "Show the current workspace."},
		"project.add": {
			Description: "Register an existing directory as a harness project.",
			Args: map[string]ArgSchema{
				"path":             {Type: ArgString, Required: true},
				"name":             {Type: ArgString},
				"project_id":       {Type: ArgString},
				"description":      {Type: ArgString},
				"default_mode":     {Type: ArgString},
				"allowed_toolsets": {Type: ArgArray},
			},
		},
		"project.create": {
			Description: "Create a persistent harness-managed workspace and register it as a project.",
			Args: map[string]ArgSchema{
				"name":             {Type: ArgString, Required: true},
				"project_id":       {Type: ArgString},
				"description":      {Type: ArgString},
				"default_mode":     {Type: ArgString},
				"allowed_toolsets": {Type: ArgArray},
			},
		},
		"project.clone": {
			Description: "Clone a git repository into a persistent harness-managed workspace and register it as a project.",
			Args: map[string]ArgSchema{
				"repo_url":         {Type: ArgString, Required: true},
				"branch":           {Type: ArgString},
				"name":             {Type: ArgString},
				"project_id":       {Type: ArgString},
				"description":      {Type: ArgString},
				"default_mode":     {Type: ArgString},
				"allowed_toolsets": {Type: ArgArray},
				"depth":            {Type: ArgInt},
				"timeout_ms":       {Type: ArgInt},
			},
		},
		"project.rename": {
			Description: "Rename the selected project. Pass the target as project; the project id is preserved.",
			Args: map[string]ArgSchema{
				"name":        {Type: ArgString, Required: true},
				"description": {Type: ArgString},
			},
		},
		"project.relocate": {
			Description: "Repoint the selected project at a different existing directory. Updates the registry only; does not move files.",
			Args: map[string]ArgSchema{
				"path": {Type: ArgString, Required: true},
			},
		},
		"project.remove": {
			Description: "Unregister the selected project. With delete_files it also deletes the workspace directory, but only for harness-managed workspaces.",
			Args: map[string]ArgSchema{
				"delete_files": {Type: ArgBool},
			},
		},
		"skill.list": {Description: "List available skills."},
		"skill.use": {
			Description: "Load a skill and mark it active for the session.",
			Args: map[string]ArgSchema{
				"name":   {Type: ArgString, Required: true},
				"reason": {Type: ArgString},
			},
		},
		"mcp.list": {
			Description: "List configured external MCP servers.",
			Args: map[string]ArgSchema{
				"probe":      {Type: ArgBool},
				"timeout_ms": {Type: ArgInt},
			},
		},
		"mcp.call": {
			Description: "Call a tool on a configured external MCP server.",
			Args: map[string]ArgSchema{
				"server":     {Type: ArgString, Required: true},
				"tool":       {Type: ArgString, Required: true},
				"arguments":  {Type: ArgObject},
				"timeout_ms": {Type: ArgInt},
			},
		},
		"mcp.add": {
			Description: "Add or replace an external MCP server config.",
			Args: map[string]ArgSchema{
				"id":        {Type: ArgString, Required: true},
				"name":      {Type: ArgString},
				"transport": {Type: ArgString},
				"command":   {Type: ArgString},
				"endpoint":  {Type: ArgString},
				"args":      {Type: ArgArray},
				"env":       {Type: ArgObject},
				"trusted":   {Type: ArgBool},
			},
		},
		"mcp.remove": {
			Description: "Remove an external MCP server config.",
			Args: map[string]ArgSchema{
				"id": {Type: ArgString, Required: true},
			},
		},
		"history.list": {
			Description: "List recorded tool-call history events.",
			Args: map[string]ArgSchema{
				"project_id":      {Type: ArgString},
				"current_project": {Type: ArgBool},
				"session_id":      {Type: ArgString},
				"limit":           {Type: ArgInt},
				"include_diff":    {Type: ArgBool},
			},
		},
		"history.show": {
			Description: "Show one history event and diff.",
			Args: map[string]ArgSchema{
				"event_id": {Type: ArgString, Required: true},
			},
		},
		"history.restore": {
			Description: "Restore workspace files to a recorded version.",
			Args: map[string]ArgSchema{
				"version_id": {Type: ArgString, Required: true},
			},
		},
	}
}

func ValidateToolArgs(tool string, args map[string]any, schemas map[string]ToolSchema) error {
	schema, ok := schemas[tool]
	if !ok {
		return fmt.Errorf("unknown tool")
	}
	for key, spec := range schema.Args {
		value, exists := args[key]
		if spec.Required && (!exists || isEmptyString(value)) {
			return fmt.Errorf("missing required arg %q", key)
		}
		if exists && !argMatchesType(value, spec.Type) {
			return fmt.Errorf("arg %q must be %s", key, spec.Type)
		}
	}
	allowed := map[string]bool{
		"approval_id": true,
	}
	for key := range schema.Args {
		allowed[key] = true
	}
	var unknown []string
	for key := range args {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown arg(s): %s", strings.Join(unknown, ", "))
	}
	return nil
}

func argMatchesType(value any, typ ArgType) bool {
	switch typ {
	case ArgString:
		_, ok := value.(string)
		return ok
	case ArgBool:
		_, ok := value.(bool)
		return ok
	case ArgInt:
		switch v := value.(type) {
		case int, int64:
			return true
		case float64:
			return math.Trunc(v) == v
		default:
			return false
		}
	case ArgObject:
		_, ok := value.(map[string]any)
		return ok
	case ArgArray:
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}

func isEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}
