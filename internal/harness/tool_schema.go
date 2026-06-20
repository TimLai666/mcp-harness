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
