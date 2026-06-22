package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/oidcauth"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var activitySeq atomic.Int64

func nextActivityID() string {
	return "act-" + strconv.FormatInt(activitySeq.Add(1), 10)
}

// addTool registers a gated MCP tool. Every invocation emits a live activity
// event (so the dashboard reacts instantly) and must carry a session_id that
// this server issued via the harness tool. A missing or unknown session_id is
// rejected before the handler runs, which forces the agent through the protocol
// guide before it can do anything.
func addTool[In any](server *mcp.Server, runtime *harness.Runtime, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) {
	name := tool.Name
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		id := nextActivityID()
		harness.PublishActivity(ownerFromContext(ctx), id, name, "start", "running", "")
		if !runtime.ValidSession(sessionIDOf(args)) {
			err := fmt.Errorf("missing or invalid session_id: call the harness tool first, then pass its returned session_id to every other tool")
			harness.PublishActivity(ownerFromContext(ctx), id, name, "end", "error", err.Error())
			return nil, nil, err
		}
		res, out, err := handler(ctx, req, args)
		status, errText := "ok", ""
		if err != nil {
			status, errText = "error", err.Error()
		}
		harness.PublishActivity(ownerFromContext(ctx), id, name, "end", status, errText)
		return res, out, err
	})
}

// addOpenTool registers a tool that does not require a session_id. Only the
// harness tool uses it, since it is what mints the session id in the first place.
func addOpenTool[In any](server *mcp.Server, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) {
	name := tool.Name
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		id := nextActivityID()
		harness.PublishActivity(ownerFromContext(ctx), id, name, "start", "running", "")
		res, out, err := handler(ctx, req, args)
		status, errText := "ok", ""
		if err != nil {
			status, errText = "error", err.Error()
		}
		harness.PublishActivity(ownerFromContext(ctx), id, name, "end", status, errText)
		return res, out, err
	})
}

// sessionIDOf extracts the session_id field from any typed tool-args struct.
func sessionIDOf(args any) string {
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	if value, ok := m["session_id"].(string); ok {
		return value
	}
	return ""
}

// New builds the mcp-harness MCP server. The capability surface is a set of
// narrow, structured tools instead of a single catch-all that hides intent in
// free text. `harness` itself is prompt-only: it returns the protocol guide and
// does no local work. Permission control lives in the operator's access policy
// and the Web UI approval queue, never in agent-supplied parameters.
func New(runtime *harness.Runtime) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-harness", Version: "0.1.0"}, nil)
	registerGuideTool(server, runtime)
	registerDiscoveryTools(server, runtime)
	registerWorkspaceTools(server, runtime)
	registerGitTools(server, runtime)
	registerProjectTools(server, runtime)
	registerSkillTools(server, runtime)
	registerMCPTools(server, runtime)
	registerHistoryTools(server, runtime)
	return server
}

// AuthOptions configures how the /mcp endpoint authenticates callers. When
// Verifier is set, OIDC bearer tokens (e.g. from Logto) are validated and the
// tenant is the token subject. Otherwise StaticBearer (if set) is a single
// shared token mapped to the default tenant. With neither, the endpoint is open
// (single-user/local).
type AuthOptions struct {
	StaticBearer        string
	Verifier            *oidcauth.Verifier
	ResourceMetadataURL string
}

func StreamableHTTPHandler(runtime *harness.Runtime, opts AuthOptions) http.Handler {
	server := New(runtime)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
	})

	if opts.Verifier != nil {
		verify := func(ctx context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
			claims, err := opts.Verifier.Verify(ctx, token)
			if err != nil {
				logTokenRejection(opts.Verifier, token, err)
				return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
			}
			if authDebug() {
				log.Printf("[mcp-auth] accept: owner=%s aud=%v iss=%s", claims.Subject, claims.Audience, claims.Issuer)
			}
			expiry := claims.Expiry
			if expiry.IsZero() {
				expiry = time.Now().Add(time.Hour)
			}
			return &auth.TokenInfo{
				UserID:     harness.NormalizeOwner(claims.Subject),
				Expiration: expiry,
				Extra:      map[string]any{"email": claims.Email},
			}, nil
		}
		return auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: opts.ResourceMetadataURL})(handler)
	}

	bearerToken := strings.TrimSpace(opts.StaticBearer)
	if bearerToken == "" {
		return handler
	}
	verify := func(ctx context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(token), []byte(bearerToken)) != 1 {
			return nil, fmt.Errorf("%w", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{
			Expiration: time.Now().Add(24 * time.Hour),
			UserID:     harness.DefaultOwner,
		}, nil
	}
	return auth.RequireBearerToken(verify, nil)(handler)
}

func authDebug() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MCP_HARNESS_AUTH_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// logTokenRejection writes a precise, actionable reason an MCP bearer token was
// rejected. It decodes the token's claims WITHOUT trusting them (never logs the
// token string or signature) so the operator can see issuer/audience mismatches
// or — the most common case — an opaque token that is not a JWT.
func logTokenRejection(verifier *oidcauth.Verifier, token string, err error) {
	peek := oidcauth.PeekClaims(token)
	if !peek.IsJWT {
		log.Printf("[mcp-auth] reject: %v — token is opaque (not a JWT). The client must request resource=%q so Logto issues a JWT for the API resource; check the OAuth resource indicator / protected-resource metadata.", err, verifier.Audience())
		return
	}
	log.Printf("[mcp-auth] reject: %v — token alg=%s kid=%s iss=%q aud=%v sub=%q; expected iss=%q aud=%q",
		err, peek.Alg, peek.Kid, peek.Issuer, peek.Audience, peek.Subject, verifier.Issuer(), verifier.Audience())
}

// ownerFromContext returns the authenticated tenant id from the validated bearer
// token, or the default owner when authentication is disabled.
func ownerFromContext(ctx context.Context) string {
	if info := auth.TokenInfoFromContext(ctx); info != nil && info.UserID != "" {
		return harness.NormalizeOwner(info.UserID)
	}
	return harness.DefaultOwner
}

// exec runs one direct tool through the runtime, mapping the public MCP tool
// name to its internal toolset name and returning the structured result. The
// tenant is taken from the authenticated token, never from the agent.
func exec(ctx context.Context, runtime *harness.Runtime, internalTool, project, sessionID string, args map[string]any) (*mcp.CallToolResult, any, error) {
	result, err := runtime.ExecuteTool(ctx, harness.ToolCallRequest{
		Tool:      internalTool,
		Owner:     ownerFromContext(ctx),
		Project:   project,
		SessionID: sessionID,
		Args:      args,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// toArgs converts a typed tool-argument struct into the args map the toolset
// handlers expect, dropping the routing fields that are not tool args.
func toArgs(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	args := map[string]any{}
	_ = json.Unmarshal(data, &args)
	delete(args, "project")
	delete(args, "session_id")
	return args
}

// --- guide -----------------------------------------------------------------

type guideArgs struct {
	Project string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path to orient the guide; empty uses the default sandbox"`
}

func registerGuideTool(server *mcp.Server, runtime *harness.Runtime) {
	addOpenTool(server, &mcp.Tool{
		Name:        "harness",
		Description: "Read this first. Returns the mcp-harness operating guide (working rules and tool protocol), a snapshot of available projects and skills, and a session_id. It performs no local work. Every other mcp-harness tool REQUIRES the session_id returned here: call harness once, then pass that session_id to every subsequent tool call. Calls without a valid server-issued session_id are rejected.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args guideArgs) (*mcp.CallToolResult, any, error) {
		return nil, runtime.Guide(ownerFromContext(ctx), args.Project), nil
	})
}

// --- discovery (read-only, not recorded as history) ------------------------

type sessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required"`
}

type approvalListArgs struct {
	SessionID string                 `json:"session_id" jsonschema:"session id issued by the harness tool; required"`
	Status    harness.ApprovalStatus `json:"status,omitempty" jsonschema:"optional approval status filter: pending, approved, or rejected"`
}

type historyListArgs struct {
	SessionID   string `json:"session_id" jsonschema:"session id issued by the harness tool; required"`
	ProjectID   string `json:"project_id,omitempty" jsonschema:"optional project id filter"`
	HistorySID  string `json:"history_session_id,omitempty" jsonschema:"optional session id to filter history events by"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of events to return"`
	IncludeDiff bool   `json:"include_diff,omitempty" jsonschema:"include recorded diff text in returned events"`
}

type historyShowArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required"`
	EventID   string `json:"event_id" jsonschema:"history event id"`
}

type historyRestorePreviewArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required"`
	VersionID string `json:"version_id" jsonschema:"workspace version id to preview"`
}

func registerDiscoveryTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "project_list",
		Description: "List configured harness projects. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionArgs) (*mcp.CallToolResult, any, error) {
		projects, err := harness.ProjectRegistry{Owner: ownerFromContext(ctx)}.List()
		return nil, map[string]any{"projects": projects}, err
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "list_skills",
		Description: "List available skills with their metadata, without loading full skill content. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionArgs) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"skills": runtime.Skills().List()}, nil
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "approval_list",
		Description: "List approval records for operator review. Read-only; it cannot approve or reject.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args approvalListArgs) (*mcp.CallToolResult, any, error) {
		records, err := harness.ApprovalStore{Owner: ownerFromContext(ctx)}.List()
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
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "history_list",
		Description: "List recorded tool-call history events. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args historyListArgs) (*mcp.CallToolResult, any, error) {
		events, err := harness.ListHistoryEventsFor(ownerFromContext(ctx), args.ProjectID, args.HistorySID, args.Limit, args.IncludeDiff)
		return nil, map[string]any{"events": events}, err
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "history_show",
		Description: "Show one recorded history event including its diff. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args historyShowArgs) (*mcp.CallToolResult, any, error) {
		event, err := harness.GetHistoryEventFor(ownerFromContext(ctx), args.EventID)
		return nil, map[string]any{"event": event}, err
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "history_restore_preview",
		Description: "Preview the diff that restoring a recorded workspace version would apply. Does not modify files.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args historyRestorePreviewArgs) (*mcp.CallToolResult, any, error) {
		version, err := harness.LoadWorkspaceVersionFor(ownerFromContext(ctx), args.VersionID)
		if err != nil {
			return nil, nil, err
		}
		preview, diff, truncated, err := harness.PreviewRestoreWorkspaceVersionFor(ownerFromContext(ctx), version.WorkspaceRoot, args.VersionID)
		return nil, map[string]any{"version": preview, "diff": diff, "diff_truncated": truncated}, err
	})
}

// --- workspace --------------------------------------------------------------

type listFilesArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Path       string `json:"path,omitempty" jsonschema:"workspace-relative directory; defaults to ."`
	Recursive  bool   `json:"recursive,omitempty" jsonschema:"recurse into subdirectories"`
	Glob       string `json:"glob,omitempty" jsonschema:"optional glob filter"`
	MaxEntries int    `json:"max_entries,omitempty" jsonschema:"maximum entries to return"`
}

type readFileArgs struct {
	Project     string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID   string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Path        string `json:"path" jsonschema:"workspace-relative file path to read"`
	Offset      int    `json:"offset,omitempty" jsonschema:"byte offset to start reading from"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to return"`
	LineNumbers *bool  `json:"line_numbers,omitempty" jsonschema:"include numbered_content with 1-based line numbers (default true)"`
}

type replaceLinesArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Path       string `json:"path" jsonschema:"workspace-relative file path to edit"`
	StartLine  int    `json:"start_line" jsonschema:"first line to replace (1-based, inclusive)"`
	EndLine    int    `json:"end_line" jsonschema:"last line to replace (1-based, inclusive); use start_line-1 to insert without removing lines"`
	Content    string `json:"content,omitempty" jsonschema:"replacement text for the line range; omit to delete the range"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type searchArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Pattern    string `json:"pattern" jsonschema:"text or regular expression to search for"`
	Glob       string `json:"glob,omitempty" jsonschema:"optional glob filter for files to search"`
	Regex      bool   `json:"regex,omitempty" jsonschema:"treat pattern as a regular expression"`
	MaxMatches int    `json:"max_matches,omitempty" jsonschema:"maximum matches to return"`
}

type writeFileArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Path       string `json:"path" jsonschema:"workspace-relative file path to write"`
	Content    string `json:"content,omitempty" jsonschema:"file content to write"`
	Overwrite  bool   `json:"overwrite,omitempty" jsonschema:"overwrite an existing file"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type applyPatchArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Patch      string `json:"patch" jsonschema:"harness patch text to apply"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type terminalRunArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Command    string `json:"command" jsonschema:"shell command to run inside the workspace"`
	Cwd        string `json:"cwd,omitempty" jsonschema:"workspace-relative working directory"`
	TimeoutMS  int    `json:"timeout_ms,omitempty" jsonschema:"command timeout in milliseconds"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

func registerWorkspaceTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_list_files",
		Description: "List files in a project or sandbox workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listFilesArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.list_files", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_read_file",
		Description: "Read a workspace file with bounded output.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readFileArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.read_file", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_search",
		Description: "Search text files in a workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.search", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_write_file",
		Description: "Write a workspace file. This mutates files: under the default access policy it queues for operator approval and returns approval_required with an approval id; call again with that approval_id after it is approved.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args writeFileArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.write_file", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_apply_patch",
		Description: "Apply a harness patch to workspace files. This mutates files and follows the same approval flow as workspace_write_file.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args applyPatchArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.apply_patch", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "workspace_replace_lines",
		Description: "Replace an inclusive 1-based line range in a file with new content — edit large files in fragments instead of rewriting them whole. Read the file first (it returns line numbers). Mutates files; follows the same approval flow as workspace_write_file.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args replaceLinesArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "workspace.replace_lines", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "terminal_run",
		Description: "Run a shell command inside the workspace. Output is bounded and timed out. Obviously destructive commands queue for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args terminalRunArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "terminal.run", args.Project, args.SessionID, toArgs(args))
	})
}

// --- git --------------------------------------------------------------------

type gitStatusArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
}

type gitDiffArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Cached    bool   `json:"cached,omitempty" jsonschema:"diff staged changes instead of the working tree"`
}

type gitLogArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Limit     int    `json:"limit,omitempty" jsonschema:"number of commits to show"`
}

type gitShowArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Rev       string `json:"rev,omitempty" jsonschema:"git revision to show; defaults to HEAD"`
}

func registerGitTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "git_status",
		Description: "Show git status (branch and short status) for a workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args gitStatusArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "git.status", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "git_diff",
		Description: "Show git diff for a workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args gitDiffArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "git.diff", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "git_log",
		Description: "Show recent git commits for a workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args gitLogArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "git.log", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "git_show",
		Description: "Show one git revision for a workspace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args gitShowArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "git.show", args.Project, args.SessionID, toArgs(args))
	})
}

// --- projects ---------------------------------------------------------------

type projectCurrentArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
}

type projectAddArgs struct {
	SessionID       string   `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Path            string   `json:"path" jsonschema:"absolute path of an existing directory the harness process can see"`
	Name            string   `json:"name,omitempty" jsonschema:"optional display name"`
	ProjectID       string   `json:"project_id,omitempty" jsonschema:"optional stable project id"`
	Description     string   `json:"description,omitempty" jsonschema:"optional description for the agent"`
	DefaultMode     string   `json:"default_mode,omitempty" jsonschema:"default mode: inspect or work"`
	AllowedToolsets []string `json:"allowed_toolsets,omitempty" jsonschema:"optional list of toolsets this project may use"`
	ApprovalID      string   `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type projectCreateArgs struct {
	SessionID       string   `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Name            string   `json:"name" jsonschema:"workspace display name"`
	ProjectID       string   `json:"project_id,omitempty" jsonschema:"optional stable project id"`
	Description     string   `json:"description,omitempty" jsonschema:"optional description for the agent"`
	DefaultMode     string   `json:"default_mode,omitempty" jsonschema:"default mode: inspect or work"`
	AllowedToolsets []string `json:"allowed_toolsets,omitempty" jsonschema:"optional list of toolsets this project may use"`
	ApprovalID      string   `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type projectCloneArgs struct {
	SessionID       string   `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	RepoURL         string   `json:"repo_url" jsonschema:"git repository url to clone"`
	Branch          string   `json:"branch,omitempty" jsonschema:"optional branch to check out"`
	Name            string   `json:"name,omitempty" jsonschema:"optional display name"`
	ProjectID       string   `json:"project_id,omitempty" jsonschema:"optional stable project id"`
	Description     string   `json:"description,omitempty" jsonschema:"optional description for the agent"`
	DefaultMode     string   `json:"default_mode,omitempty" jsonschema:"default mode: inspect or work"`
	AllowedToolsets []string `json:"allowed_toolsets,omitempty" jsonschema:"optional list of toolsets this project may use"`
	Depth           int      `json:"depth,omitempty" jsonschema:"optional shallow clone depth"`
	TimeoutMS       int      `json:"timeout_ms,omitempty" jsonschema:"clone timeout in milliseconds"`
	ApprovalID      string   `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type projectRenameArgs struct {
	Project     string `json:"project" jsonschema:"project id, name, or path to rename"`
	SessionID   string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Name        string `json:"name" jsonschema:"new display name; the project id is preserved"`
	Description string `json:"description,omitempty" jsonschema:"optional new description"`
	ApprovalID  string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type projectRelocateArgs struct {
	Project    string `json:"project" jsonschema:"project id, name, or current path to relocate"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Path       string `json:"path" jsonschema:"new absolute directory path the project should point at; the directory must already exist"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type projectRemoveArgs struct {
	Project     string `json:"project" jsonschema:"project id, name, or path to remove"`
	SessionID   string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	DeleteFiles bool   `json:"delete_files,omitempty" jsonschema:"also delete the workspace directory on disk; only allowed for harness-managed workspaces"`
	ApprovalID  string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

func registerProjectTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "project_current",
		Description: "Show the resolved workspace for a project id, name, or path (or the default sandbox).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectCurrentArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.current", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_add",
		Description: "Register an existing directory as a harness project. Changes the project registry and queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectAddArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.add", "", args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_create",
		Description: "Create an empty persistent harness-managed workspace and register it as a project. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectCreateArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.create", "", args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_clone",
		Description: "Clone a git repository into a persistent harness-managed workspace and register it as a project. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectCloneArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.clone", "", args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_rename",
		Description: "Rename a registered project (the project id stays the same). Pass the target as project. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectRenameArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.rename", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_relocate",
		Description: "Repoint a registered project at a different existing directory. Updates the registry only; does not move files. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectRelocateArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.relocate", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "project_remove",
		Description: "Unregister a project. With delete_files it also deletes the workspace directory, but only for harness-managed workspaces under MCP_HARNESS_HOME/workspaces. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args projectRemoveArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "project.remove", args.Project, args.SessionID, toArgs(args))
	})
}

// --- skills -----------------------------------------------------------------

type useSkillArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Name      string `json:"name" jsonschema:"skill name to load and activate"`
	Reason    string `json:"reason,omitempty" jsonschema:"optional reason this skill is being used"`
}

func registerSkillTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "use_skill",
		Description: "Load a skill's full content and mark it active for the session. Call list_skills first to discover names.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args useSkillArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "skill.use", args.Project, args.SessionID, toArgs(args))
	})
}

// --- external mcp -----------------------------------------------------------

type mcpListArgs struct {
	Project   string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID string `json:"session_id" jsonschema:"session id issued by the harness tool; required for every tool except harness"`
	Probe     bool   `json:"probe,omitempty" jsonschema:"connect to each server and list its tools"`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"per-server probe timeout in milliseconds"`
}

type mcpCallArgs struct {
	Project    string         `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string         `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	Server     string         `json:"server" jsonschema:"configured external MCP server id"`
	Tool       string         `json:"tool" jsonschema:"tool name on that external MCP server"`
	Arguments  map[string]any `json:"arguments,omitempty" jsonschema:"arguments object for the external MCP tool"`
	TimeoutMS  int            `json:"timeout_ms,omitempty" jsonschema:"call timeout in milliseconds"`
	ApprovalID string         `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type mcpAddArgs struct {
	SessionID  string         `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	ID         string         `json:"id" jsonschema:"external MCP server id"`
	Name       string         `json:"name,omitempty" jsonschema:"display name"`
	Transport  string         `json:"transport,omitempty" jsonschema:"stdio or streamable_http"`
	Command    string         `json:"command,omitempty" jsonschema:"command for stdio transport"`
	Endpoint   string         `json:"endpoint,omitempty" jsonschema:"endpoint url for streamable_http transport"`
	Args       []string       `json:"args,omitempty" jsonschema:"command arguments for stdio transport"`
	Env        map[string]any `json:"env,omitempty" jsonschema:"environment variables for stdio transport"`
	Trusted    bool           `json:"trusted,omitempty" jsonschema:"mark this server trusted so its calls skip approval"`
	ApprovalID string         `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

type mcpRemoveArgs struct {
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	ID         string `json:"id" jsonschema:"external MCP server id to remove"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

func registerMCPTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "mcp_list",
		Description: "List configured external MCP servers. With probe=true it connects to each and lists its tools.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpListArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "mcp.list", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "mcp_call",
		Description: "Call a tool on a configured external MCP server. Arguments are validated against the external tool's input schema. Calls to untrusted servers queue for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpCallArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "mcp.call", args.Project, args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "mcp_add",
		Description: "Add or replace an external MCP server configuration. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpAddArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "mcp.add", "", args.SessionID, toArgs(args))
	})

	addTool(server, runtime, &mcp.Tool{
		Name:        "mcp_remove",
		Description: "Remove an external MCP server configuration. Queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpRemoveArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "mcp.remove", "", args.SessionID, toArgs(args))
	})
}

// --- history mutation -------------------------------------------------------

type historyRestoreArgs struct {
	Project    string `json:"project,omitempty" jsonschema:"optional project id, name, or absolute path; empty uses the default sandbox"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"optional session id to group related tool calls"`
	VersionID  string `json:"version_id" jsonschema:"workspace version id to restore"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"approval id returned by a prior approval_required result, after an operator approved it"`
}

func registerHistoryTools(server *mcp.Server, runtime *harness.Runtime) {
	addTool(server, runtime, &mcp.Tool{
		Name:        "history_restore",
		Description: "Restore workspace files to a recorded version. This mutates files and queues for operator approval; re-run with approval_id after approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args historyRestoreArgs) (*mcp.CallToolResult, any, error) {
		return exec(ctx, runtime, "history.restore", args.Project, args.SessionID, toArgs(args))
	})
}
