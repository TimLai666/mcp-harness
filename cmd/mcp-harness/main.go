package main

import (
	"context"
	"log"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type harnessArgs struct {
	Message    string             `json:"message" jsonschema:"natural language instructions and optional harness tool call blocks"`
	Project    string             `json:"project,omitempty" jsonschema:"optional project id, project name, or absolute path"`
	Mode       harness.Mode       `json:"mode,omitempty" jsonschema:"inspect or work"`
	AccessMode harness.AccessMode `json:"access_mode,omitempty" jsonschema:"default, auto, or full_access"`
	SessionID  string             `json:"session_id,omitempty" jsonschema:"optional existing session id"`
}

type emptyArgs struct{}

type approvalListArgs struct {
	Status harness.ApprovalStatus `json:"status,omitempty" jsonschema:"optional approval status filter: pending, approved, or rejected"`
}

type historyListArgs struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"optional project id filter"`
	SessionID   string `json:"session_id,omitempty" jsonschema:"optional session id filter"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of events to return"`
	IncludeDiff bool   `json:"include_diff,omitempty" jsonschema:"include recorded diff text in returned events"`
}

type historyShowArgs struct {
	EventID string `json:"event_id" jsonschema:"history event id"`
}

type historyRestorePreviewArgs struct {
	VersionID string `json:"version_id" jsonschema:"workspace version id to preview"`
}

func main() {
	server := newServer(harness.NewRuntime())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func newServer(runtime *harness.Runtime) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-harness", Version: "0.1.0"}, nil)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "harness",
			Description: "Run a local harness turn. The message may contain <harness_tool_call> JSON blocks.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args harnessArgs) (*mcp.CallToolResult, any, error) {
			result, err := runtime.Run(ctx, harness.RunRequest{
				Message:    args.Message,
				Project:    args.Project,
				Mode:       args.Mode,
				AccessMode: args.AccessMode,
				SessionID:  args.SessionID,
			})
			return nil, result, err
		},
	)
	registerDirectTools(server)
	return server
}

func registerDirectTools(server *mcp.Server) {
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "project_list",
			Description: "List configured harness projects without starting a harness turn.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
			projects, err := (harness.ProjectRegistry{}).List()
			return nil, map[string]any{"projects": projects}, err
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "skill_list",
			Description: "List available harness skills without loading full skill content.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
			return nil, map[string]any{"skills": harness.NewSkillRegistry().List()}, nil
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "mcp_list",
			Description: "List configured external MCP servers. This does not probe or call those servers.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
			servers, err := harness.LoadMCPServers()
			return nil, map[string]any{"servers": servers}, err
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "approval_list",
			Description: "List harness approval records for Web UI or user review. This tool cannot approve or reject records.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args approvalListArgs) (*mcp.CallToolResult, any, error) {
			records, err := (harness.ApprovalStore{}).List()
			if err != nil {
				return nil, nil, err
			}
			if args.Status == "" {
				return nil, map[string]any{"approvals": records}, nil
			}
			filtered := make([]harness.ApprovalRecord, 0, len(records))
			for _, record := range records {
				if record.Status == args.Status {
					filtered = append(filtered, record)
				}
			}
			return nil, map[string]any{"approvals": filtered}, nil
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "history_list",
			Description: "List recorded harness tool-call history events without starting a harness turn.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args historyListArgs) (*mcp.CallToolResult, any, error) {
			events, err := harness.ListHistoryEvents(args.ProjectID, args.SessionID, args.Limit, args.IncludeDiff)
			return nil, map[string]any{"events": events}, err
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "history_show",
			Description: "Show one recorded harness history event including its diff.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args historyShowArgs) (*mcp.CallToolResult, any, error) {
			event, err := harness.GetHistoryEvent(args.EventID)
			return nil, map[string]any{"event": event}, err
		},
	)
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "history_restore_preview",
			Description: "Preview the diff that would be applied by restoring a recorded workspace version. This does not modify files.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args historyRestorePreviewArgs) (*mcp.CallToolResult, any, error) {
			version, err := harness.LoadWorkspaceVersion(args.VersionID)
			if err != nil {
				return nil, nil, err
			}
			preview, diff, truncated, err := harness.PreviewRestoreWorkspaceVersion(version.WorkspaceRoot, args.VersionID)
			return nil, map[string]any{"version": preview, "diff": diff, "diff_truncated": truncated}, err
		},
	)
}
