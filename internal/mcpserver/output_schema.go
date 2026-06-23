package mcpserver

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	outputSchemasOnce sync.Once
	outputSchemas     map[string]any
	outputSchemasErr  error
)

type commandResult struct {
	ReturnCode int    `json:"returncode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	GitHubAuth string `json:"github_auth,omitempty"`
}

type terminalRunResult struct {
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
	ReturnCode int    `json:"returncode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	GitHubAuth string `json:"github_auth,omitempty"`
}

type gitStatusResult struct {
	ReturnCode int             `json:"returncode"`
	Stdout     string          `json:"stdout"`
	Stderr     string          `json:"stderr"`
	GitHubAuth string          `json:"github_auth,omitempty"`
	GitInfo    harness.GitInfo `json:"git_info"`
	Dirty      bool            `json:"dirty"`
}

type workspaceMkdirResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

type workspaceMoveResult struct {
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Type            string `json:"type"`
}

type workspaceDeleteResult struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Recursive bool   `json:"recursive"`
}

type workspaceWriteFileResult struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type workspaceReplaceLinesResult struct {
	Path          string `json:"path"`
	RemovedLines  int    `json:"removed_lines"`
	InsertedLines int    `json:"inserted_lines"`
	LineCount     int    `json:"line_count"`
}

type patchAppliedResult struct {
	Action string `json:"action"`
	Path   string `json:"path"`
}

type workspaceApplyPatchResult struct {
	Applied []patchAppliedResult `json:"applied"`
}

type workspaceSearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type workspaceSearchResult struct {
	Matches   []workspaceSearchMatch `json:"matches"`
	Truncated bool                   `json:"truncated"`
}

type workspaceGrepResult struct {
	ReturnCode int    `json:"returncode"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	Stderr     string `json:"stderr,omitempty"`
}

type projectEnvelope struct {
	Project harness.Project `json:"project"`
}

type projectCreateResult struct {
	Project        harness.Project `json:"project"`
	WorkspacesRoot string          `json:"workspaces_root"`
}

type projectRemoveResult struct {
	Removed      string          `json:"removed"`
	Project      harness.Project `json:"project"`
	FilesDeleted bool            `json:"files_deleted"`
}

type skillUseResult struct {
	Skill   harness.SkillSpec `json:"skill"`
	Content string            `json:"content"`
}

type historyRestorePreviewResult struct {
	Version       harness.WorkspaceVersion `json:"version"`
	Diff          string                   `json:"diff"`
	DiffTruncated bool                     `json:"diff_truncated"`
}

type historyRestoreResult struct {
	RestoredVersion string `json:"restored_version"`
	Label           string `json:"label"`
	Diff            string `json:"diff"`
	DiffTruncated   bool   `json:"diff_truncated"`
}

type stringResult struct {
	Removed string `json:"removed"`
}

type serverEnvelope struct {
	Server harness.MCPServerConfig `json:"server"`
}

func ensureToolOutputSchema(tool *mcp.Tool) *mcp.Tool {
	if tool == nil {
		return nil
	}
	if tool.OutputSchema != nil {
		return tool
	}
	outputSchemasOnce.Do(func() {
		outputSchemas, outputSchemasErr = buildOutputSchemas()
	})
	if outputSchemasErr != nil {
		panic(outputSchemasErr)
	}
	schema, ok := outputSchemas[tool.Name]
	if !ok {
		panic(fmt.Sprintf("missing output schema for MCP tool %q", tool.Name))
	}
	clone := *tool
	clone.OutputSchema = schema
	return &clone
}

func buildOutputSchemas() (map[string]any, error) {
	projectSchema, err := schemaFor[harness.Project]()
	if err != nil {
		return nil, err
	}
	workspaceSchema, err := schemaFor[harness.Workspace]()
	if err != nil {
		return nil, err
	}
	guideSchema, err := schemaFor[harness.GuideResult]()
	if err != nil {
		return nil, err
	}
	skillSchema, err := schemaFor[harness.SkillSpec]()
	if err != nil {
		return nil, err
	}
	approvalSchema, err := schemaFor[harness.ApprovalRecord]()
	if err != nil {
		return nil, err
	}
	historyEventSchema, err := schemaFor[harness.HistoryEvent]()
	if err != nil {
		return nil, err
	}
	cloneSchema, err := schemaFor[harness.CloneResult]()
	if err != nil {
		return nil, err
	}
	commandSchema, err := schemaFor[commandResult]()
	if err != nil {
		return nil, err
	}
	terminalSchema, err := schemaFor[terminalRunResult]()
	if err != nil {
		return nil, err
	}
	gitStatusSchema, err := schemaFor[gitStatusResult]()
	if err != nil {
		return nil, err
	}
	mkdirSchema, err := schemaFor[workspaceMkdirResult]()
	if err != nil {
		return nil, err
	}
	moveSchema, err := schemaFor[workspaceMoveResult]()
	if err != nil {
		return nil, err
	}
	deleteSchema, err := schemaFor[workspaceDeleteResult]()
	if err != nil {
		return nil, err
	}
	writeFileSchema, err := schemaFor[workspaceWriteFileResult]()
	if err != nil {
		return nil, err
	}
	replaceLinesSchema, err := schemaFor[workspaceReplaceLinesResult]()
	if err != nil {
		return nil, err
	}
	applyPatchSchema, err := schemaFor[workspaceApplyPatchResult]()
	if err != nil {
		return nil, err
	}
	searchSchema, err := schemaFor[workspaceSearchResult]()
	if err != nil {
		return nil, err
	}
	grepSchema, err := schemaFor[workspaceGrepResult]()
	if err != nil {
		return nil, err
	}
	projectEnvelopeSchema, err := schemaFor[projectEnvelope]()
	if err != nil {
		return nil, err
	}
	projectCreateSchema, err := schemaFor[projectCreateResult]()
	if err != nil {
		return nil, err
	}
	projectRemoveSchema, err := schemaFor[projectRemoveResult]()
	if err != nil {
		return nil, err
	}
	skillUseSchema, err := schemaFor[skillUseResult]()
	if err != nil {
		return nil, err
	}
	historyRestorePreviewSchema, err := schemaFor[historyRestorePreviewResult]()
	if err != nil {
		return nil, err
	}
	historyRestoreSchema, err := schemaFor[historyRestoreResult]()
	if err != nil {
		return nil, err
	}
	stringResultSchema, err := schemaFor[stringResult]()
	if err != nil {
		return nil, err
	}
	serverConfigSchema, err := schemaFor[harness.MCPServerConfig]()
	if err != nil {
		return nil, err
	}
	serverEnvelopeSchema, err := schemaFor[serverEnvelope]()
	if err != nil {
		return nil, err
	}
	setObjectProperty(guideSchema, "projects", nullableSchema(arraySchema(projectSchema)))
	setObjectProperty(guideSchema, "skills", nullableSchema(arraySchema(skillSchema)))
	setObjectProperty(searchSchema, "matches", nullableSchema(arraySchema(objectSchema(map[string]any{
		"path": stringSchema(),
		"line": integerSchema(),
		"text": stringSchema(),
	}, "path", "line", "text"))))

	approvalRequired := objectSchema(map[string]any{
		"approval": approvalSchema,
		"message":  stringSchema(),
	}, "approval", "message")

	toolCallOutputSchema := func(resultSchema any, allowApproval bool) any {
		if allowApproval {
			resultSchema = anyOfSchema(resultSchema, approvalRequired)
		}
		return objectSchema(map[string]any{
			"session_id":     stringSchema(),
			"tool":           stringSchema(),
			"status":         stringSchema(),
			"result":         resultSchema,
			"error":          stringSchema(),
			"project":        projectSchema,
			"workspace_root": stringSchema(),
			"mode":           stringSchema(),
			"access_mode":    stringSchema(),
			"active_skills":  arraySchema(stringSchema()),
			"history_event":  historyEventSchema,
		}, "session_id", "tool", "status", "workspace_root", "mode", "access_mode")
	}

	workspaceListFilesSchema := objectSchema(map[string]any{
		"root": stringSchema(),
		"entries": nullableSchema(arraySchema(objectSchema(map[string]any{
			"path": stringSchema(),
			"type": stringSchema(),
			"size": nullableSchema(integerSchema()),
		}, "path", "type"))),
		"truncated": boolSchema(),
	}, "root", "entries", "truncated")

	workspaceReadFileSchema := objectSchema(map[string]any{
		"path":             stringSchema(),
		"type":             stringSchema(),
		"size":             integerSchema(),
		"offset":           integerSchema(),
		"truncated":        boolSchema(),
		"content":          stringSchema(),
		"start_line":       integerSchema(),
		"numbered_content": stringSchema(),
	}, "path", "type", "size")

	mcpListSchema := objectSchema(map[string]any{
		"servers": nullableSchema(arraySchema(serverConfigSchema)),
		"tools": map[string]any{
			"type": "object",
			"additionalProperties": anyOfSchema(
				arraySchema(objectSchema(nil)),
				objectSchema(map[string]any{"error": stringSchema()}, "error"),
			),
		},
	}, "servers")

	externalCallResultSchema := objectSchema(map[string]any{
		"_meta":             objectSchema(nil),
		"content":           arraySchema(objectSchema(nil)),
		"structuredContent": objectSchema(nil),
		"isError":           boolSchema(),
	}, "content")

	schemas := map[string]any{
		"harness":                 guideSchema,
		"project_list":            objectSchema(map[string]any{"projects": nullableSchema(arraySchema(projectSchema))}, "projects"),
		"list_skills":             objectSchema(map[string]any{"skills": nullableSchema(arraySchema(skillSchema))}, "skills"),
		"approval_list":           objectSchema(map[string]any{"approvals": nullableSchema(arraySchema(approvalSchema))}, "approvals"),
		"history_list":            objectSchema(map[string]any{"events": nullableSchema(arraySchema(historyEventSchema))}, "events"),
		"history_show":            objectSchema(map[string]any{"event": historyEventSchema}, "event"),
		"history_restore_preview": historyRestorePreviewSchema,

		"workspace_list_files":    toolCallOutputSchema(workspaceListFilesSchema, false),
		"workspace_read_file":     toolCallOutputSchema(workspaceReadFileSchema, false),
		"workspace_search":        toolCallOutputSchema(searchSchema, false),
		"workspace_grep":          toolCallOutputSchema(grepSchema, false),
		"workspace_mkdir":         toolCallOutputSchema(mkdirSchema, true),
		"workspace_move":          toolCallOutputSchema(moveSchema, true),
		"workspace_delete":        toolCallOutputSchema(deleteSchema, true),
		"workspace_write_file":    toolCallOutputSchema(writeFileSchema, true),
		"workspace_apply_patch":   toolCallOutputSchema(applyPatchSchema, true),
		"workspace_replace_lines": toolCallOutputSchema(replaceLinesSchema, true),
		"terminal_run":            toolCallOutputSchema(terminalSchema, true),

		"git_status":   toolCallOutputSchema(gitStatusSchema, false),
		"git_diff":     toolCallOutputSchema(commandSchema, false),
		"git_log":      toolCallOutputSchema(commandSchema, false),
		"git_show":     toolCallOutputSchema(commandSchema, false),
		"git_add":      toolCallOutputSchema(commandSchema, false),
		"git_commit":   toolCallOutputSchema(commandSchema, false),
		"git_checkout": toolCallOutputSchema(commandSchema, false),
		"git_branch":   toolCallOutputSchema(commandSchema, true),
		"git_fetch":    toolCallOutputSchema(commandSchema, false),
		"git_pull":     toolCallOutputSchema(commandSchema, false),
		"git_push":     toolCallOutputSchema(commandSchema, true),
		"git_merge":    toolCallOutputSchema(commandSchema, false),
		"git_reset":    toolCallOutputSchema(commandSchema, true),
		"git_stash":    toolCallOutputSchema(commandSchema, false),
		"git_tag":      toolCallOutputSchema(commandSchema, false),

		"github_pr_create":    toolCallOutputSchema(commandSchema, true),
		"github_pr_list":      toolCallOutputSchema(commandSchema, false),
		"github_pr_view":      toolCallOutputSchema(commandSchema, false),
		"github_pr_merge":     toolCallOutputSchema(commandSchema, true),
		"github_issue_list":   toolCallOutputSchema(commandSchema, false),
		"github_issue_create": toolCallOutputSchema(commandSchema, true),
		"github_issue_view":   toolCallOutputSchema(commandSchema, false),
		"github_repo_view":    toolCallOutputSchema(commandSchema, false),

		"project_current":  toolCallOutputSchema(workspaceSchema, false),
		"project_add":      toolCallOutputSchema(projectEnvelopeSchema, true),
		"project_create":   toolCallOutputSchema(projectCreateSchema, true),
		"project_clone":    toolCallOutputSchema(cloneSchema, true),
		"project_rename":   toolCallOutputSchema(projectEnvelopeSchema, true),
		"project_relocate": toolCallOutputSchema(projectEnvelopeSchema, true),
		"project_remove":   toolCallOutputSchema(projectRemoveSchema, true),

		"use_skill": toolCallOutputSchema(skillUseSchema, false),

		"mcp_list":   toolCallOutputSchema(mcpListSchema, false),
		"mcp_call":   toolCallOutputSchema(externalCallResultSchema, true),
		"mcp_add":    toolCallOutputSchema(serverEnvelopeSchema, true),
		"mcp_remove": toolCallOutputSchema(stringResultSchema, true),

		"history_restore": toolCallOutputSchema(historyRestoreSchema, true),
	}
	return schemas, nil
}

func schemaFor[T any]() (map[string]any, error) {
	return schemaForValue(*new(T))
}

func schemaForValue[T any](value T) (map[string]any, error) {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object"}
	if properties != nil {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func integerSchema() map[string]any {
	return map[string]any{"type": "integer"}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func nullableSchema(schema any) map[string]any {
	return anyOfSchema(schema, map[string]any{"type": "null"})
}

func anyOfSchema(schemas ...any) map[string]any {
	items := make([]any, 0, len(schemas))
	for _, schema := range schemas {
		items = append(items, schema)
	}
	return map[string]any{"anyOf": items}
}

func setObjectProperty(schema map[string]any, key string, value any) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	properties[key] = value
}
