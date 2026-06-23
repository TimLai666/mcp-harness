package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/mcpserver"
)

const mcpEndpoint = "/mcp"

func ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, NewHandler())
}

func NewHandler() http.Handler {
	rt := harness.NewRuntime()
	app := newAuthConfig()
	mux := http.NewServeMux()
	authOpts := mcpserver.AuthOptions{StaticBearer: app.mcpToken, ResourceMetadataURL: app.resourceMetadataURL()}
	if app.enabled() {
		authOpts.Verifier = app.tokenVerif
	}
	mcpHandler := mcpserver.StreamableHTTPHandler(rt, authOpts)
	mux.Handle(mcpEndpoint, mcpHandler)
	mux.Handle(mcpEndpoint+"/", mcpHandler)
	app.registerAuthRoutes(mux)
	app.registerGitHubRoutes(mux)
	// requireOwner resolves the tenant for a Web UI API request, writing a 401
	// when OIDC is enabled and the caller is not logged in.
	requireOwner := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		owner, ok := app.owner(r)
		if !ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "login required", "login": "/auth/login"})
		}
		return owner, ok
	}
	runOperatorTool := func(r *http.Request, owner, project, tool string, args map[string]any) (harness.ToolCallResult, error) {
		sessionID := "webui-" + time.Now().Format("20060102T150405.000000000")
		call := harness.ToolCallRequest{
			Owner:     owner,
			Project:   project,
			SessionID: sessionID,
			Tool:      tool,
			Args:      cloneArgs(args),
		}
		result, err := rt.ExecuteTool(r.Context(), call)
		if err != nil || result.Status != "approval_required" {
			return result, err
		}
		record, ok := approvalRecordFromResult(result.Result)
		if !ok {
			return result, nil
		}
		if _, err := (harness.ApprovalStore{Owner: owner}).SetStatus(record.ID, harness.ApprovalApproved); err != nil {
			return result, err
		}
		call.Args["approval_id"] = record.ID
		return rt.ExecuteTool(r.Context(), call)
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := app.owner(r); !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		events, cancel := harness.DefaultBroker().Subscribe()
		defer cancel()
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-events:
				if ev.Owner != "" && ev.Owner != owner {
					continue // another tenant's event
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-heartbeat.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ok":            true,
			"mcp_endpoint":  mcpEndpoint,
			"mcp_transport": "streamable_http",
		})
	})
	mux.HandleFunc("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := app.owner(r)
		writeJSON(w, map[string]any{"enabled": app.enabled(), "authenticated": ok, "owner": owner, "login": "/auth/login"})
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		list, err := harness.ProjectRegistry{Owner: owner}.List()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"projects": list})
	})
	mux.HandleFunc("GET /api/git", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		workspace, err := harness.ProjectRegistry{Owner: owner}.Resolve(r.URL.Query().Get("project"), "")
		if err != nil {
			writeError(w, err)
			return
		}
		response := map[string]any{"git": harness.WorkspaceGitInfo(workspace.Root)}
		if r.URL.Query().Get("branches") == "true" {
			response["branches"] = harness.WorkspaceGitBranches(workspace.Root)
		}
		if r.URL.Query().Get("status_entries") == "true" {
			response["status_entries"] = harness.WorkspaceGitStatusEntries(workspace.Root)
		}
		writeJSON(w, response)
	})
	mux.HandleFunc("POST /api/git/add", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project string   `json:"project"`
			Paths   []string `json:"paths"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.add", map[string]any{"paths": stringSliceToAny(req.Paths)})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/git/fetch", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project string `json:"project"`
			Remote  string `json:"remote"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.fetch", map[string]any{"remote": req.Remote, "timeout_ms": 60000})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/git/pull", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project string `json:"project"`
			Remote  string `json:"remote"`
			Branch  string `json:"branch"`
			FFOnly  bool   `json:"ff_only"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.pull", map[string]any{
			"remote":     req.Remote,
			"branch":     req.Branch,
			"ff_only":    req.FFOnly,
			"timeout_ms": 60000,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/git/checkout", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project string `json:"project"`
			Ref     string `json:"ref"`
			Create  bool   `json:"create"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.checkout", map[string]any{"ref": req.Ref, "create": req.Create})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/git/commit", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project string `json:"project"`
			Message string `json:"message"`
			All     bool   `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.commit", map[string]any{"message": req.Message, "all": req.All})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/git/push", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Project     string `json:"project"`
			Remote      string `json:"remote"`
			Branch      string `json:"branch"`
			SetUpstream bool   `json:"set_upstream"`
			Force       bool   `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		result, err := runOperatorTool(r, owner, req.Project, "git.push", map[string]any{
			"remote":       req.Remote,
			"branch":       req.Branch,
			"set_upstream": req.SetUpstream,
			"force":        req.Force,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			Path            string       `json:"path"`
			Name            string       `json:"name"`
			ProjectID       string       `json:"project_id"`
			Description     string       `json:"description"`
			DefaultMode     harness.Mode `json:"default_mode"`
			AllowedToolsets []string     `json:"allowed_toolsets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		project, err := harness.ProjectRegistry{Owner: owner}.AddWithAllowedToolsets(req.Path, req.Name, req.ProjectID, req.Description, req.DefaultMode, req.AllowedToolsets)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"project": project})
	})
	mux.HandleFunc("GET /api/settings/access-mode", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		writeJSON(w, map[string]any{"access_mode": harness.CurrentAccessModeFor(owner)})
	})
	mux.HandleFunc("POST /api/settings/access-mode", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			AccessMode harness.AccessMode `json:"access_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		if err := harness.SetAccessModeFor(owner, req.AccessMode); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"access_mode": harness.CurrentAccessModeFor(owner)})
	})
	mux.HandleFunc("GET /api/approvals", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		records, err := harness.ApprovalStore{Owner: owner}.List()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"approvals": records})
	})
	mux.HandleFunc("POST /api/approvals/", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		id, action := splitApprovalPath(r.URL.Path)
		status := harness.ApprovalRejected
		if action == "approve" {
			status = harness.ApprovalApproved
		} else if action != "reject" {
			writeError(w, http.ErrNotSupported)
			return
		}
		record, err := harness.ApprovalStore{Owner: owner}.SetStatus(id, status)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"approval": record})
	})
	mux.HandleFunc("GET /api/mcps", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		servers, err := harness.LoadMCPServersFor(owner)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"servers": servers})
	})
	mux.HandleFunc("POST /api/mcps", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var config harness.MCPServerConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			writeError(w, err)
			return
		}
		if err := harness.AddMCPServerFor(owner, config); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"server": config})
	})
	mux.HandleFunc("DELETE /api/mcps/", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		id := r.URL.Path[len("/api/mcps/"):]
		if err := harness.DeleteMCPServerFor(owner, id); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"removed": id})
	})
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireOwner(w, r); !ok {
			return
		}
		writeJSON(w, map[string]any{"skills": harness.NewSkillRegistry().List()})
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		store, err := harness.DefaultStoreFor(owner)
		if err != nil {
			writeError(w, err)
			return
		}
		sessions, err := store.ListSessions(r.URL.Query().Get("project_id"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"sessions": sessions})
	})
	mux.HandleFunc("GET /api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		id := r.URL.Path[len("/api/sessions/"):]
		store, err := harness.DefaultStoreFor(owner)
		if err != nil {
			writeError(w, err)
			return
		}
		session, turns, err := store.GetSession(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"session": session, "turns": turns})
	})
	mux.HandleFunc("GET /api/tool-calls", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		store, err := harness.DefaultStoreFor(owner)
		if err != nil {
			writeError(w, err)
			return
		}
		calls, err := store.ListToolCalls(r.URL.Query().Get("session_id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"tool_calls": calls})
	})
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := harness.ListHistoryEventsFor(owner, r.URL.Query().Get("project_id"), r.URL.Query().Get("session_id"), limit, r.URL.Query().Get("include_diff") == "true")
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"events": events})
	})
	mux.HandleFunc("GET /api/history/", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		id := r.URL.Path[len("/api/history/"):]
		event, err := harness.GetHistoryEventFor(owner, id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"event": event})
	})
	mux.HandleFunc("POST /api/history/restore-preview", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			VersionID string `json:"version_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		version, err := harness.LoadWorkspaceVersionFor(owner, req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		preview, diff, truncated, err := harness.PreviewRestoreWorkspaceVersionFor(owner, version.WorkspaceRoot, req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"version": preview, "diff": diff, "diff_truncated": truncated})
	})
	mux.HandleFunc("POST /api/history/restore", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := requireOwner(w, r)
		if !ok {
			return
		}
		var req struct {
			VersionID string `json:"version_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		version, err := harness.LoadWorkspaceVersionFor(owner, req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		restored, diff, truncated, err := harness.RestoreWorkspaceVersionFor(owner, version.WorkspaceRoot, req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"version": restored, "diff": diff, "diff_truncated": truncated})
	})
	return mux
}

func splitApprovalPath(path string) (string, string) {
	rest := path[len("/api/approvals/"):]
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == '/' {
			return rest[:i], rest[i+1:]
		}
	}
	return rest, ""
}

func cloneArgs(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func stringSliceToAny(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func approvalRecordFromResult(value any) (harness.ApprovalRecord, bool) {
	result, ok := value.(map[string]any)
	if !ok {
		return harness.ApprovalRecord{}, false
	}
	record, ok := result["approval"].(harness.ApprovalRecord)
	if !ok {
		return harness.ApprovalRecord{}, false
	}
	return record, true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}

const indexHTML = `<!doctype html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>mcp-harness</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/styles/github-dark.min.css">
  <script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/highlight.min.js"></script>
  <style>
    :root { --bg:#eef2f6; --panel:#fbfcfe; --panel-strong:#ffffff; --line:#d7dce5; --line-strong:#b8c2d1; --text:#172033; --muted:#667085; --accent:#0f6c5c; --accent-strong:#0a4b40; --bad:#b42318; --ok:#067647; --ink:#0f1728; --soft:#f2f5f9; }
    * { box-sizing:border-box; }
    body { margin:0; background:radial-gradient(circle at top left, #f7fbff 0, var(--bg) 42%, #e6ecf3 100%); color:var(--text); font:14px/1.45 "Segoe UI",system-ui,-apple-system,BlinkMacSystemFont,sans-serif; }
    .shell { min-height:100vh; display:grid; grid-template-columns:300px minmax(520px,1fr) 440px; }
    aside, main, section { padding:18px; border-right:1px solid rgba(184,194,209,.7); overflow:auto; max-height:100vh; }
    section { border-right:0; }
    aside { background:linear-gradient(180deg, rgba(255,255,255,.92), rgba(245,248,252,.88)); backdrop-filter:blur(10px); }
    main { background:rgba(251,252,254,.72); }
    section { background:rgba(247,250,253,.78); }
    h1, h2 { font-size:16px; margin:0 0 12px; letter-spacing:.01em; }
    h3 { font-size:12px; margin:14px 0 8px; color:var(--muted); text-transform:uppercase; letter-spacing:.09em; }
    label { display:block; margin:10px 0 5px; color:var(--muted); font-size:12px; }
    input, select, textarea, button { width:100%; border:1px solid var(--line); border-radius:10px; padding:9px 11px; font:inherit; background:#fff; color:var(--text); }
    input[type="checkbox"] { width:auto; padding:0; }
    textarea { min-height:92px; resize:vertical; }
    button { margin-top:8px; color:#fff; background:linear-gradient(180deg, var(--accent), var(--accent-strong)); border-color:var(--accent-strong); cursor:pointer; font-weight:600; box-shadow:0 8px 20px rgba(15,108,92,.16); }
    button.secondary { color:var(--text); background:#fff; border-color:var(--line); box-shadow:none; }
    button.danger { background:var(--bad); border-color:var(--bad); }
    button:disabled { opacity:.5; cursor:not-allowed; box-shadow:none; }
    .card { background:linear-gradient(180deg, var(--panel-strong), var(--panel)); border:1px solid rgba(184,194,209,.85); border-radius:14px; padding:12px; margin-bottom:12px; box-shadow:0 10px 28px rgba(15,23,40,.05); }
    .card strong { display:block; }
    .card small { display:block; color:var(--muted); word-break:break-all; }
    .selected { border-color:var(--accent); box-shadow:0 0 0 1px var(--accent) inset, 0 12px 28px rgba(15,108,92,.12); }
    .grid2 { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
    .grid3 { display:grid; grid-template-columns:repeat(3, minmax(0, 1fr)); gap:8px; }
    .pill { display:inline-block; border:1px solid var(--line); border-radius:999px; padding:2px 7px; color:var(--muted); font-size:12px; margin-right:4px; background:#fff; }
    pre { white-space:pre-wrap; word-break:break-word; background:#101828; color:#f9fafb; border-radius:8px; padding:12px; max-height:420px; overflow:auto; font-size:12px; }
    .muted { color:var(--muted); }
    .ok { color:var(--ok); }
    .bad { color:var(--bad); }
    .hero { padding:16px; border-radius:18px; background:
      linear-gradient(135deg, rgba(15,108,92,.10), rgba(255,255,255,.92) 34%, rgba(15,23,40,.02) 100%);
      border:1px solid rgba(15,108,92,.18);
      box-shadow:0 16px 40px rgba(15,23,40,.08);
    }
    .hero-title { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; }
    .hero-title h2 { margin-bottom:4px; font-size:22px; }
    .hero-meta { display:flex; flex-wrap:wrap; gap:8px; margin:8px 0 0; }
    .hero-note { margin-top:10px; padding:10px 12px; border-radius:12px; background:rgba(255,255,255,.78); border:1px solid rgba(184,194,209,.7); }
    .git-console { margin-top:14px; display:grid; grid-template-columns:minmax(0, 1.05fr) minmax(0, .95fr); gap:12px; align-items:start; }
    .git-panel { min-height:100%; }
    .git-summary-card { grid-column:1 / -1; }
    .git-branch-card { grid-column:1; }
    .git-stage-card { grid-column:2; }
    .git-commit-card { grid-column:1 / -1; }
    .git-summary { padding:12px; border-radius:14px; background:#0f1728; color:#eff5ff; }
    .git-summary strong { display:inline; }
    .git-summary-grid { display:grid; grid-template-columns:repeat(4, minmax(0, 1fr)); gap:8px; margin-top:10px; }
    .metric { padding:8px 10px; border-radius:12px; background:rgba(255,255,255,.08); }
    .metric b { display:block; font-size:16px; color:#fff; }
    .metric span { font-size:11px; color:rgba(239,245,255,.68); text-transform:uppercase; letter-spacing:.08em; }
    .toolbar-card { background:rgba(255,255,255,.84); border:1px solid rgba(184,194,209,.75); border-radius:14px; padding:12px; }
    .toolbar-row { display:grid; grid-template-columns:1.2fr .8fr; gap:8px; align-items:end; }
    .toolbar-row-3 { display:grid; grid-template-columns:1fr 1fr auto; gap:8px; align-items:end; }
    .toolbar-actions { display:flex; gap:8px; align-items:center; }
    .toolbar-actions.wrap { flex-wrap:wrap; }
    .toolbar-actions button { margin-top:0; }
    .git-busy-note { display:inline-flex; align-items:center; gap:6px; color:var(--muted); font-size:12px; }
    .git-busy-note:before { content:""; width:10px; height:10px; border-radius:999px; border:2px solid rgba(15,108,92,.25); border-top-color:var(--accent); animation:spin .8s linear infinite; }
    .status-list { display:flex; flex-direction:column; gap:8px; max-height:210px; overflow:auto; margin-top:10px; }
    .status-row { display:grid; grid-template-columns:auto 1fr auto; gap:8px; align-items:flex-start; padding:8px 10px; border-radius:12px; border:1px solid rgba(184,194,209,.7); background:rgba(255,255,255,.86); }
    .status-row code { font:12px/1.4 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; color:var(--ink); word-break:break-word; }
    .status-tag { min-width:42px; text-align:center; padding:2px 6px; border-radius:999px; background:#eef2f6; color:var(--muted); font-size:11px; font-weight:600; }
    .status-row.staged { border-color:rgba(6,118,71,.26); }
    .status-row.untracked { border-color:rgba(15,108,92,.26); }
    .status-row.modified { border-color:rgba(180,35,24,.18); }
    .notice { border-radius:12px; padding:10px 12px; border:1px solid rgba(184,194,209,.8); background:#fff; }
    .notice.ok { background:rgba(6,118,71,.08); border-color:rgba(6,118,71,.18); }
    .notice.bad { background:rgba(180,35,24,.07); border-color:rgba(180,35,24,.18); }
    .diff { border:1px solid var(--line); border-radius:8px; overflow:hidden; margin-top:6px; background:#0d1117; }
    .diff-file { border-top:1px solid #21262d; }
    .diff-file:first-child { border-top:0; }
    .diff-file-head { background:#161b22; color:#c9d1d9; padding:5px 10px; font:12px/1.4 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; word-break:break-all; }
    .diff-table { width:100%; border-collapse:collapse; table-layout:fixed; font:12px/1.5 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; }
    .diff-table td { vertical-align:top; padding:0; }
    .diff-table td.ln { width:4ch; text-align:right; padding:0 4px; color:#6e7681; background:#0d1117; user-select:none; border-right:1px solid #21262d; white-space:nowrap; }
    .diff-table td.code { padding:0 8px; white-space:pre-wrap; word-break:break-word; color:#c9d1d9; }
    .diff-table td.code .hljs { background:transparent; padding:0; display:inline; }
    .diff-table tr.ctx td.code { color:#9da7b3; }
    .diff-table td.del { background:#3d1c20; }
    .diff-table td.del.ln { background:#48181d; color:#c08a8a; }
    .diff-table td.add { background:#15311f; }
    .diff-table td.add.ln { background:#10391f; color:#7fb98f; }
    .diff-table td.hunk { background:#161b22; color:#6e7681; padding:1px 8px; }
    .diff-meta { color:var(--muted); font-size:12px; margin:2px 0 6px; }
    .gitline { margin-top:4px; font:12px/1.4 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; }
    .gitline:empty { display:none; }
    .gitline .branch { color:var(--text); }
    .card-head { display:flex; justify-content:space-between; gap:10px; align-items:center; margin-bottom:8px; }
    .stack { display:flex; flex-direction:column; gap:12px; }
    .tiny { font-size:11px; color:var(--muted); }
    .side-tabs { display:flex; gap:8px; flex-wrap:wrap; margin:0 0 12px; }
    .side-tab { width:auto; margin-top:0; padding:8px 12px; border-radius:999px; background:rgba(255,255,255,.88); color:var(--muted); border:1px solid rgba(184,194,209,.9); box-shadow:none; }
    .side-tab.active { background:linear-gradient(180deg, var(--accent), var(--accent-strong)); color:#fff; border-color:var(--accent-strong); box-shadow:0 8px 20px rgba(15,108,92,.16); }
    .side-pane { display:none; }
    .side-pane.active { display:block; }
    @keyframes spin { to { transform:rotate(360deg); } }
    #terminal .hljs { background:transparent; padding:0; }
    @media (max-width: 1200px) {
      .shell { grid-template-columns:280px minmax(0,1fr); }
      section { grid-column:1 / -1; border-top:1px solid rgba(184,194,209,.7); max-height:none; }
      .git-console { grid-template-columns:1fr; }
      .git-summary-card, .git-branch-card, .git-stage-card, .git-commit-card { grid-column:auto; }
    }
    @media (max-width: 900px) {
      .shell { grid-template-columns:1fr; }
      aside, main, section { max-height:none; border-right:0; border-bottom:1px solid rgba(184,194,209,.7); }
      .hero-title { flex-direction:column; }
      .toolbar-row, .toolbar-row-3, .grid2, .grid3, .git-summary-grid { grid-template-columns:1fr; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <aside>
      <h1>mcp-harness</h1>
      <div id="authbar" class="muted" style="font-size:12px;margin-bottom:8px"></div>
      <div id="github" style="font-size:12px;margin-bottom:8px"></div>
      <h3>Projects</h3>
      <div id="projects"></div>
      <h3>Add Project</h3>
      <label>Name</label><input id="newName" placeholder="Optional display name">
      <label>Path</label><input id="newPath" placeholder="C:\\Users\\...\\repo">
      <label>Allowed toolsets</label><input id="newAllowedToolsets" placeholder="workspace,git,terminal">
      <button onclick="addProject()">Add project</button>
    </aside>
    <main>
      <div class="hero">
        <div class="hero-title">
          <div>
            <h2 id="projectTitle">Default sandbox</h2>
            <div id="projectMeta" class="hero-meta">
              <span class="pill">No project selected</span>
            </div>
          </div>
          <div class="toolbar-actions">
            <div id="gitBusyNote" class="git-busy-note" style="display:none">Git action in progress</div>
            <button id="refreshGitButton" data-git-control data-idle-label="Refresh Git" class="secondary" style="width:auto" onclick="manualRefreshGit(this)">Refresh Git</button>
          </div>
        </div>
        <div id="actionStatus" class="hero-note muted">Select a project to use branch, commit, and push controls.</div>
        <div class="git-console">
          <div class="git-panel git-summary-card">
            <div id="gitSummary" class="git-summary">
              <strong>Git snapshot unavailable</strong>
              <div class="tiny" style="color:rgba(239,245,255,.7);margin-top:6px">This workspace is not a git repository, or no project is selected.</div>
            </div>
          </div>
          <div class="toolbar-card git-branch-card">
            <div class="card-head">
              <strong>Switch Branch</strong>
              <span class="tiny">Manual branch control</span>
            </div>
            <label>Existing branch</label>
            <select id="checkoutBranch" data-git-control></select>
            <button data-git-control data-idle-label="Checkout selected branch" data-busy-label="Checking out..." class="secondary" onclick="checkoutSelectedBranch(this)">Checkout selected branch</button>
            <label>New branch</label>
            <div class="toolbar-row">
              <input id="newBranch" data-git-control placeholder="feature/web-console">
              <button data-git-control data-idle-label="Create + checkout" data-busy-label="Creating branch..." onclick="createBranch(this)">Create + checkout</button>
            </div>
          </div>
          <div class="toolbar-card git-stage-card">
            <div class="card-head">
              <strong>Stage And Sync</strong>
              <div class="toolbar-actions wrap">
                <button data-git-control data-idle-label="Fetch" data-busy-label="Fetching..." class="secondary" style="width:auto" onclick="fetchChanges(this)">Fetch</button>
                <button data-git-control data-idle-label="Pull (ff-only)" data-busy-label="Pulling..." class="secondary" style="width:auto" onclick="pullChanges(this)">Pull (ff-only)</button>
              </div>
            </div>
            <div class="toolbar-actions wrap" style="margin-top:8px">
              <button data-git-control data-idle-label="Stage all" data-busy-label="Staging..." class="secondary" style="width:auto" onclick="stageAllChanges(this)">Stage all</button>
              <button data-git-control data-idle-label="Stage selected" data-busy-label="Staging..." class="secondary" style="width:auto" onclick="stageSelectedChanges(this)">Stage selected</button>
            </div>
            <div id="statusEntries" class="status-list"></div>
          </div>
          <div class="toolbar-card git-commit-card">
            <div class="card-head">
              <strong>Commit And Push</strong>
              <span class="tiny">Operator actions</span>
            </div>
            <label>Commit message</label>
            <textarea id="commitMessage" data-git-control placeholder="Describe this change"></textarea>
            <button data-git-control data-idle-label="Commit staged changes" data-busy-label="Committing..." onclick="commitChanges(this)">Commit staged changes</button>
            <label>Push remote / branch</label>
            <div class="toolbar-row-3">
              <input id="pushRemote" data-git-control value="origin" placeholder="origin">
              <input id="pushBranch" data-git-control placeholder="current branch">
              <button data-git-control data-idle-label="Push" data-busy-label="Pushing..." onclick="pushChanges(this)">Push</button>
            </div>
          </div>
        </div>
      </div>
      <div class="grid2">
        <div>
          <h3>Sessions</h3>
          <div id="sessions"></div>
        </div>
        <div>
          <h3>Tool Calls</h3>
          <div id="toolCalls"></div>
        </div>
      </div>
      <h3>History</h3>
      <div id="history"></div>
    </main>
    <section>
      <h2>Details</h2>
      <div class="side-tabs">
        <button id="sideTab-inspect" class="side-tab active" onclick="switchSideTab('inspect')">Inspect</button>
        <button id="sideTab-ops" class="side-tab" onclick="switchSideTab('ops')">Ops</button>
        <button id="sideTab-catalog" class="side-tab" onclick="switchSideTab('catalog')">Catalog</button>
      </div>
      <div id="sidePane-inspect" class="side-pane active">
        <div id="detail" class="card muted">Select a session, tool call, history event, or approval.</div>
        <h3>MCP Activity <span id="liveDot" class="pill">offline</span></h3>
        <div id="activity" class="card muted">No MCP calls yet.</div>
        <h3>Live Terminal</h3>
        <div class="card">
          <small id="terminalHeader" class="muted">Waiting for terminal_run output…</small>
          <pre id="terminal"></pre>
        </div>
      </div>
      <div id="sidePane-ops" class="side-pane">
        <h3>Access Policy</h3>
        <div class="card">
          <small class="muted">Operator-controlled. Agents cannot change this.</small>
          <label>Mode</label>
          <select id="accessMode" onchange="setAccessMode()">
            <option value="default">default (queue high-risk ops for approval)</option>
            <option value="full_access">full_access (run high-risk ops directly)</option>
          </select>
        </div>
        <h3>Approvals</h3>
        <div id="approvals"></div>
      </div>
      <div id="sidePane-catalog" class="side-pane">
        <h3>MCP Servers</h3>
        <div id="mcps"></div>
        <h3>Skills</h3>
        <div id="skills"></div>
      </div>
    </section>
  </div>
  <script>
    let selectedProject = '';
    let selectedProjectName = 'Default sandbox';
    let selectedSession = '';
    let currentSideTab = 'inspect';
    let gitBusy = false;
    let currentGit = null;
    let currentBranches = [];
    let currentStatusEntries = [];
    const escapeHTML = (text) => String(text).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    function switchSideTab(tab) {
      currentSideTab = tab;
      for (const pane of document.querySelectorAll('.side-pane')) pane.classList.remove('active');
      for (const button of document.querySelectorAll('.side-tab')) button.classList.remove('active');
      const pane = document.getElementById('sidePane-' + tab);
      const button = document.getElementById('sideTab-' + tab);
      if (pane) pane.classList.add('active');
      if (button) button.classList.add('active');
    }
    function setDetail(value) {
      switchSideTab('inspect');
      const detail = document.getElementById('detail');
      detail.className = 'card';
      detail.innerHTML = '<pre>' + escapeHTML(typeof value === 'string' ? value : JSON.stringify(value, null, 2)) + '</pre>';
    }
    function setActionStatus(message, tone='muted') {
      const box = document.getElementById('actionStatus');
      box.className = 'hero-note notice ' + tone;
      box.textContent = message;
    }
    function setGitBusyState(busy, activeButton=null, note='Git action in progress') {
      gitBusy = busy;
      const busyNote = document.getElementById('gitBusyNote');
      if (busyNote) {
        busyNote.textContent = note;
        busyNote.style.display = busy ? 'inline-flex' : 'none';
      }
      for (const node of document.querySelectorAll('[data-git-control]')) {
        node.disabled = busy;
        if (node.tagName === 'BUTTON') {
          if (!node.dataset.idleLabel) node.dataset.idleLabel = node.textContent.trim();
          const busyLabel = node.dataset.busyLabel || node.dataset.idleLabel;
          node.textContent = busy && node === activeButton ? busyLabel : node.dataset.idleLabel;
        }
      }
      if (activeButton && busy) activeButton.disabled = true;
    }
    function renderProjectMeta(git) {
      const meta = document.getElementById('projectMeta');
      const pills = ['<span class="pill">' + escapeHTML(selectedProjectName || 'Default sandbox') + '</span>'];
      if (selectedProject) pills.push('<span class="pill">project</span>');
      else pills.push('<span class="pill">sandbox</span>');
      if (git && git.is_repo) {
        pills.push('<span class="pill">branch ' + escapeHTML(git.branch || '?') + '</span>');
        if (git.upstream) pills.push('<span class="pill">upstream ' + escapeHTML(git.upstream) + '</span>');
      }
      meta.innerHTML = pills.join('');
    }
    function renderGitSummary(git) {
      const box = document.getElementById('gitSummary');
      if (!git || !git.is_repo) {
        box.innerHTML = '<strong>Git snapshot unavailable</strong><div class="tiny" style="color:rgba(239,245,255,.7);margin-top:6px">This workspace is not a git repository, or no project is selected.</div>';
        return;
      }
      const branch = git.branch || '?';
      const upstream = git.upstream || 'no upstream';
      box.innerHTML =
        '<strong>⎇ ' + escapeHTML(branch) + '</strong>'
        + '<div class="tiny" style="color:rgba(239,245,255,.7);margin-top:6px">upstream: ' + escapeHTML(upstream) + '</div>'
        + '<div class="git-summary-grid">'
        + '<div class="metric"><span>Changed files</span><b>' + (git.files_changed || 0) + '</b></div>'
        + '<div class="metric"><span>Ahead</span><b>' + (git.ahead || 0) + '</b></div>'
        + '<div class="metric"><span>Behind</span><b>' + (git.behind || 0) + '</b></div>'
        + '<div class="metric"><span>Line delta</span><b>+' + (git.added || 0) + ' / -' + (git.removed || 0) + '</b></div>'
        + '</div>';
    }
    function populateBranchSelect(branches, git) {
      const select = document.getElementById('checkoutBranch');
      select.innerHTML = '';
      const locals = (branches || []).filter(b => !b.remote);
      if (!locals.length) {
        const option = document.createElement('option');
        option.value = '';
        option.textContent = 'No local branches';
        select.appendChild(option);
        return;
      }
      for (const branch of locals) {
        const option = document.createElement('option');
        option.value = branch.name;
        option.textContent = branch.current ? branch.name + ' (current)' : branch.name;
        if (git && git.branch === branch.name) option.selected = true;
        select.appendChild(option);
      }
    }
    function renderStatusEntries(entries) {
      const box = document.getElementById('statusEntries');
      box.innerHTML = '';
      const items = entries || [];
      if (!items.length) {
        box.innerHTML = '<div class="tiny">No changed files.</div>';
        return;
      }
      for (const entry of items) {
        const row = document.createElement('label');
        const tone = entry.untracked ? ' untracked' : (entry.staged ? ' staged' : ' modified');
        row.className = 'status-row' + tone;
        const status = entry.untracked ? 'new' : ((entry.index_status || ' ') + (entry.worktree_status || ' ')).trim() || 'mod';
        row.innerHTML =
          '<input type="checkbox" data-path="' + escapeHTML(entry.path) + '">'
          + '<div><code>' + escapeHTML(entry.path) + '</code>'
          + (entry.original_path ? '<div class="tiny">from ' + escapeHTML(entry.original_path) + '</div>' : '')
          + '</div>'
          + '<span class="status-tag">' + escapeHTML(status) + '</span>';
        box.appendChild(row);
      }
    }
    function selectedStatusPaths() {
      return Array.from(document.querySelectorAll('#statusEntries input[type="checkbox"]:checked'))
        .map(node => node.dataset.path)
        .filter(Boolean);
    }
    async function refreshGitConsole() {
      try {
        const qs = new URLSearchParams({ project: selectedProject, branches: 'true', status_entries: 'true' });
        const res = await fetch('/api/git?' + qs.toString());
        const data = await res.json();
        currentGit = data.git || null;
        currentBranches = data.branches || [];
        currentStatusEntries = data.status_entries || [];
        renderProjectMeta(currentGit);
        renderGitSummary(currentGit);
        populateBranchSelect(currentBranches, currentGit);
        renderStatusEntries(currentStatusEntries);
        document.getElementById('pushRemote').value = (currentGit && currentGit.remote) ? 'origin' : 'origin';
        document.getElementById('pushBranch').value = (currentGit && currentGit.branch) ? currentGit.branch : '';
        if (currentGit && currentGit.is_repo) {
          setActionStatus('Git controls ready for ' + (currentGit.branch || 'current branch') + '.', 'ok');
        } else {
          setActionStatus('Select a git-backed project to use branch, commit, and push controls.', 'muted');
        }
      } catch (e) {
        currentGit = null;
        currentBranches = [];
        currentStatusEntries = [];
        renderProjectMeta(null);
        renderGitSummary(null);
        populateBranchSelect([], null);
        renderStatusEntries([]);
        setActionStatus('Could not load git info: ' + e, 'bad');
      }
    }
    async function manualRefreshGit(button) {
      if (gitBusy) return;
      setGitBusyState(true, button, 'Refreshing git status');
      setActionStatus('Refreshing git status...', 'muted');
      try {
        await refreshGitConsole();
      } finally {
        setGitBusyState(false);
      }
    }
    async function refreshProjects() {
      const res = await fetch('/api/projects');
      const data = await res.json();
      const projects = data.projects || [];
      if (selectedProject && !projects.some(p => p.id === selectedProject)) {
        selectedProject = '';
        selectedProjectName = 'Default sandbox';
        selectedSession = '';
        document.getElementById('projectTitle').textContent = 'Default sandbox';
      }
      const list = document.getElementById('projects');
      list.innerHTML = '';
      const sandbox = document.createElement('div');
      sandbox.className = 'card' + (selectedProject === '' ? ' selected' : '');
      sandbox.innerHTML = '<strong>Default sandbox</strong><small>Transient workspace</small><small class="gitline" data-git=""></small>';
      sandbox.onclick = () => selectProject('', 'Default sandbox');
      list.appendChild(sandbox);
      for (const p of projects) {
        const div = document.createElement('div');
        div.className = 'card' + (selectedProject === p.id ? ' selected' : '');
        div.innerHTML = '<strong>' + escapeHTML(p.name) + '</strong><small>' + escapeHTML(p.path) + '</small><small class="gitline" data-git="' + escapeHTML(p.id) + '"></small>';
        div.onclick = () => selectProject(p.id, p.name);
        list.appendChild(div);
      }
      await updateGitBadges();
      await refreshGitConsole();
      await refreshSessions();
      await refreshHistory();
    }
    async function updateGitBadges() {
      for (const node of document.querySelectorAll('#projects [data-git]')) {
        const project = node.getAttribute('data-git');
        try {
          const res = await fetch('/api/git?project=' + encodeURIComponent(project));
          const g = (await res.json()).git || {};
          if (!g.is_repo) { node.textContent = ''; continue; }
          let html = '<span class="branch">⎇ ' + escapeHTML(g.branch || '?') + '</span>'
            + ' <span class="ok">+' + (g.added || 0) + '</span> <span class="bad">-' + (g.removed || 0) + '</span>';
          if (g.files_changed) html += ' <span class="muted">' + g.files_changed + ' files</span>';
          if (g.ahead) html += ' <span class="muted">↑' + g.ahead + '</span>';
          if (g.behind) html += ' <span class="muted">↓' + g.behind + '</span>';
          node.innerHTML = html;
        } catch (e) { node.textContent = ''; }
      }
    }
    async function selectProject(id, name) {
      selectedProject = id;
      selectedProjectName = name;
      selectedSession = '';
      document.getElementById('projectTitle').textContent = name;
      await refreshProjects();
    }
    async function refreshSessions() {
      const qs = new URLSearchParams({limit:'50'});
      if (selectedProject) qs.set('project_id', selectedProject);
      const res = await fetch('/api/sessions?' + qs.toString());
      const data = await res.json();
      const list = document.getElementById('sessions');
      list.innerHTML = '';
      for (const s of data.sessions || []) {
        const div = document.createElement('div');
        div.className = 'card' + (selectedSession === s.id ? ' selected' : '');
        div.innerHTML = '<strong>' + escapeHTML(s.id) + '</strong><small>' + escapeHTML(s.updated_at || '') + '</small><span class="pill">' + (s.turn_count || 0) + ' turns</span>';
        div.onclick = () => selectSession(s.id);
        list.appendChild(div);
      }
      if (!selectedSession) document.getElementById('toolCalls').innerHTML = '';
    }
    async function selectSession(id) {
      selectedSession = id;
      const sessionRes = await fetch('/api/sessions/' + encodeURIComponent(id));
      setDetail(await sessionRes.json());
      await refreshToolCalls();
      await refreshSessions();
    }
    async function refreshToolCalls() {
      if (!selectedSession) return;
      const res = await fetch('/api/tool-calls?session_id=' + encodeURIComponent(selectedSession));
      const data = await res.json();
      const list = document.getElementById('toolCalls');
      list.innerHTML = '';
      for (const c of data.tool_calls || []) {
        const div = document.createElement('div');
        div.className = 'card';
        div.innerHTML = '<strong>' + escapeHTML(c.tool) + '</strong><small>' + escapeHTML(c.status) + (c.error ? ' - ' + escapeHTML(c.error) : '') + '</small>';
        div.onclick = () => setDetail(c);
        list.appendChild(div);
      }
    }
    async function refreshHistory() {
      const qs = new URLSearchParams({limit:'40', include_diff:'true'});
      if (selectedProject) qs.set('project_id', selectedProject);
      const res = await fetch('/api/history?' + qs.toString());
      const data = await res.json();
      const list = document.getElementById('history');
      list.innerHTML = '';
      for (const h of data.events || []) {
        const div = document.createElement('div');
        div.className = 'card';
        div.innerHTML = '<strong>' + escapeHTML(h.tool) + ' <span class="' + (h.status === 'ok' ? 'ok' : 'bad') + '">' + escapeHTML(h.status) + '</span></strong><small>step ' + h.step + ' - ' + escapeHTML(h.timestamp) + (h.diff_truncated ? ' - diff truncated' : '') + '</small>' + renderDiffHTML(h.diff) + '<div class="grid2"><button class="secondary" data-version="' + h.before_version + '">Before</button><button class="secondary" data-version="' + h.after_version + '">After</button></div>';
        div.onclick = (event) => {
          if (event.target.dataset.version) return viewVersion(event.target.dataset.version);
          setDetail(h);
        };
        list.appendChild(div);
      }
    }
    const DIFF_LANG = { go:'go', js:'javascript', mjs:'javascript', cjs:'javascript', ts:'typescript', tsx:'typescript', jsx:'javascript', py:'python', rb:'ruby', rs:'rust', java:'java', c:'c', h:'c', cpp:'cpp', cc:'cpp', hpp:'cpp', cs:'csharp', php:'php', sh:'bash', bash:'bash', zsh:'bash', json:'json', yml:'yaml', yaml:'yaml', toml:'ini', ini:'ini', md:'markdown', html:'xml', htm:'xml', xml:'xml', svg:'xml', css:'css', scss:'scss', less:'less', sql:'sql', kt:'kotlin', swift:'swift', lua:'lua', dockerfile:'dockerfile' };
    function diffLang(path) {
      const base = (path || '').split('/').pop().toLowerCase();
      if (base === 'dockerfile') return 'dockerfile';
      const ext = base.includes('.') ? base.split('.').pop() : '';
      return DIFF_LANG[ext] || null;
    }
    function hl(code, lang) {
      if (window.hljs && lang && hljs.getLanguage(lang)) {
        try { return hljs.highlight(code, { language: lang, ignoreIllegal: true }).value; } catch (e) {}
      }
      return escapeHTML(code);
    }
    function diffCell(kind, ln, codeHTML) {
      const k = kind ? ' ' + kind : '';
      return '<td class="ln' + k + '">' + (ln === null ? '' : ln) + '</td>'
        + '<td class="code' + k + '">' + (codeHTML === null ? '' : codeHTML) + '</td>';
    }
    function renderDiffHTML(diffText) {
      if (!diffText || !diffText.trim()) return '<div class="diff-meta">No file diff for this step.</div>';
      const lines = diffText.split('\n');
      let html = '<div class="diff">', open = false, lang = null, oldNo = 0, newNo = 0;
      let dels = [], adds = [];
      function flush() {
        const n = Math.max(dels.length, adds.length);
        for (let k = 0; k < n; k++) {
          const d = k < dels.length ? dels[k] : null;
          const a = k < adds.length ? adds[k] : null;
          html += '<tr class="chg">'
            + diffCell(d !== null ? 'del' : '', d !== null ? ++oldNo : null, d !== null ? hl(d, lang) : null)
            + diffCell(a !== null ? 'add' : '', a !== null ? ++newNo : null, a !== null ? hl(a, lang) : null)
            + '</tr>';
        }
        dels = []; adds = [];
      }
      function closeFile() { if (open) { flush(); html += '</table></div>'; open = false; } }
      for (const line of lines) {
        if (line.startsWith('diff --git ')) {
          closeFile();
          const m = line.match(/^diff --git a\/(.*) b\/(.*)$/);
          const path = m ? m[2] : line.slice('diff --git '.length);
          lang = diffLang(path);
          html += '<div class="diff-file"><div class="diff-file-head">' + escapeHTML(path) + '</div><table class="diff-table">';
          open = true;
        } else if (!open) {
          continue;
        } else if (line.startsWith('--- ') || line.startsWith('+++ ') || line.startsWith('new file') || line.startsWith('deleted file')) {
          continue;
        } else if (line.startsWith('@@')) {
          flush();
          const m = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
          if (m) { oldNo = parseInt(m[1], 10) - 1; newNo = parseInt(m[2], 10) - 1; }
          html += '<tr><td class="hunk" colspan="4">' + escapeHTML(line) + '</td></tr>';
        } else if (line.startsWith('#')) {
          flush();
          html += '<tr><td class="hunk" colspan="4">' + escapeHTML(line.replace(/^#\s*/, '')) + '</td></tr>';
        } else if (line.startsWith('-')) {
          dels.push(line.slice(1));
        } else if (line.startsWith('+')) {
          adds.push(line.slice(1));
        } else if (line.startsWith(' ')) {
          flush();
          const text = hl(line.slice(1), lang);
          html += '<tr class="ctx">' + diffCell('', ++oldNo, text) + diffCell('', ++newNo, text) + '</tr>';
        }
      }
      closeFile();
      html += '</div>';
      return html;
    }
    async function viewVersion(versionID) {
      const res = await fetch('/api/history/restore-preview', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({version_id:versionID}) });
      const data = await res.json();
      if (data.error) { setDetail(data); return; }
      const detail = document.getElementById('detail');
      detail.className = 'card';
      let html = '<h3 style="margin-top:0">Version snapshot</h3>';
      html += '<small class="muted">' + escapeHTML(data.version?.id || versionID) + ' — ' + escapeHTML(data.version?.label || '') + '</small>';
      if (data.diff) html += '<h3 style="margin-top:10px">Diff vs current workspace</h3>' + renderDiffHTML(data.diff);
      else html += '<p class="muted">No differences from current workspace.</p>';
      html += '<button style="margin-top:12px" onclick="restoreVersion(\'' + escapeHTML(versionID) + '\')">Restore workspace to this version</button>';
      detail.innerHTML = html;
    }
    async function restoreVersion(versionID) {
      if (!confirm('This will overwrite current workspace files. Continue?')) return;
      const res = await fetch('/api/history/restore', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({version_id:versionID}) });
      const data = await res.json();
      setDetail(data);
      await refreshHistory();
    }
    async function actApproval(id, action) {
      try {
        const res = await fetch('/api/approvals/' + encodeURIComponent(id) + '/' + action, { method:'POST' });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          setDetail('Approval ' + action + ' failed (' + res.status + '): ' + (data.error || 'unknown'));
        } else {
          setDetail(data.approval || data);
        }
      } catch (e) {
        setDetail('Approval ' + action + ' error: ' + e);
      }
      refreshApprovals();
      refreshHistory();
    }
    async function refreshApprovals() {
      const res = await fetch('/api/approvals');
      const data = await res.json();
      const list = document.getElementById('approvals');
      list.innerHTML = '';
      for (const a of data.approvals || []) {
        const div = document.createElement('div');
        div.className = 'card';
        div.innerHTML = '<strong>' + escapeHTML(a.tool) + '</strong><small>' + escapeHTML(a.status) + ' - ' + escapeHTML(a.reason) + '</small>';
        div.onclick = () => setDetail(a);
        if (a.status === 'pending') {
          const approve = document.createElement('button');
          approve.textContent = 'Approve';
          approve.onclick = (event) => { event.stopPropagation(); actApproval(a.id, 'approve'); };
          div.appendChild(approve);
          const reject = document.createElement('button');
          reject.className = 'danger';
          reject.textContent = 'Reject';
          reject.onclick = (event) => { event.stopPropagation(); actApproval(a.id, 'reject'); };
          div.appendChild(reject);
        }
        list.appendChild(div);
      }
    }
    async function refreshMCPs() {
      const res = await fetch('/api/mcps');
      const data = await res.json();
      const list = document.getElementById('mcps');
      list.innerHTML = '';
      for (const m of data.servers || []) {
        const div = document.createElement('div');
        div.className = 'card';
        div.innerHTML = '<strong>' + escapeHTML(m.name) + '</strong><small>' + escapeHTML(m.id) + ' - ' + escapeHTML(m.transport) + (m.trusted ? ' - trusted' : '') + '</small>';
        div.onclick = () => setDetail(m);
        list.appendChild(div);
      }
    }
    async function refreshSkills() {
      const res = await fetch('/api/skills');
      const data = await res.json();
      const list = document.getElementById('skills');
      list.innerHTML = '';
      for (const s of data.skills || []) {
        const div = document.createElement('div');
        div.className = 'card';
        div.innerHTML = '<strong>' + escapeHTML(s.name) + '</strong><small>' + escapeHTML(s.description || '') + '</small>';
        div.onclick = () => setDetail(s);
        list.appendChild(div);
      }
    }
    async function addProject() {
      const allowed = document.getElementById('newAllowedToolsets').value.split(',').map(v => v.trim()).filter(Boolean);
      const payload = { name: document.getElementById('newName').value || '', path: document.getElementById('newPath').value, allowed_toolsets: allowed };
      const res = await fetch('/api/projects', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
      setDetail(await res.json());
      await refreshProjects();
    }
    async function performGitAction(url, payload, successMessage, button=null, busyNote='Git action in progress') {
      if (gitBusy) return;
      setGitBusyState(true, button, busyNote);
      setActionStatus(busyNote + '...', 'muted');
      try {
        const res = await fetch(url, { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
        const data = await res.json();
        setDetail(data);
        if (!res.ok || data.error || data.status === 'error') {
          setActionStatus((data.error || data.status || 'Git action failed') + (data.error ? '' : ''), 'bad');
          return;
        }
        if (data.session_id) {
          selectedSession = data.session_id;
          await refreshSessions();
          await refreshToolCalls();
        }
        await refreshGitConsole();
        await refreshHistory();
        setActionStatus(successMessage, 'ok');
      } catch (e) {
        setActionStatus('Git action failed: ' + e, 'bad');
      } finally {
        setGitBusyState(false);
      }
    }
    async function checkoutSelectedBranch(button) {
      const ref = document.getElementById('checkoutBranch').value;
      if (!ref) {
        setActionStatus('Choose a branch to checkout.', 'bad');
        return;
      }
      await performGitAction('/api/git/checkout', { project: selectedProject, ref, create: false }, 'Checked out ' + ref + '.', button, 'Checking out branch');
    }
    async function createBranch(button) {
      const ref = document.getElementById('newBranch').value.trim();
      if (!ref) {
        setActionStatus('Enter a branch name to create.', 'bad');
        return;
      }
      await performGitAction('/api/git/checkout', { project: selectedProject, ref, create: true }, 'Created and checked out ' + ref + '.', button, 'Creating branch');
      document.getElementById('newBranch').value = '';
    }
    async function stageAllChanges(button) {
      const paths = (currentStatusEntries || []).map(entry => entry.path).filter(Boolean);
      if (!paths.length) {
        setActionStatus('No changed files to stage.', 'bad');
        return;
      }
      await performGitAction('/api/git/add', { project: selectedProject, paths }, 'Staged all changed files.', button, 'Staging changed files');
    }
    async function stageSelectedChanges(button) {
      const paths = selectedStatusPaths();
      if (!paths.length) {
        setActionStatus('Select at least one file to stage.', 'bad');
        return;
      }
      await performGitAction('/api/git/add', { project: selectedProject, paths }, 'Staged selected files.', button, 'Staging selected files');
    }
    async function fetchChanges(button) {
      const remote = document.getElementById('pushRemote').value.trim() || 'origin';
      await performGitAction('/api/git/fetch', { project: selectedProject, remote }, 'Fetched from ' + remote + '.', button, 'Fetching remote updates');
    }
    async function pullChanges(button) {
      const remote = document.getElementById('pushRemote').value.trim() || 'origin';
      const branch = document.getElementById('pushBranch').value.trim() || (currentGit && currentGit.branch) || '';
      await performGitAction('/api/git/pull', { project: selectedProject, remote, branch, ff_only: true }, 'Pulled latest changes (ff-only).', button, 'Pulling latest changes');
    }
    async function commitChanges(button) {
      const message = document.getElementById('commitMessage').value.trim();
      if (!message) {
        setActionStatus('Commit message is required.', 'bad');
        return;
      }
      await performGitAction('/api/git/commit', { project: selectedProject, message, all: false }, 'Commit created.', button, 'Creating commit');
      document.getElementById('commitMessage').value = '';
    }
    async function pushChanges(button) {
      const remote = document.getElementById('pushRemote').value.trim() || 'origin';
      const branch = document.getElementById('pushBranch').value.trim() || (currentGit && currentGit.branch) || '';
      if (!branch) {
        setActionStatus('Branch name is required before push.', 'bad');
        return;
      }
      await performGitAction('/api/git/push', { project: selectedProject, remote, branch, set_upstream: true, force: false }, 'Pushed ' + branch + ' to ' + remote + '.', button, 'Pushing branch');
    }
    async function refreshAccessMode() {
      const res = await fetch('/api/settings/access-mode');
      const data = await res.json();
      if (data.access_mode) document.getElementById('accessMode').value = data.access_mode;
    }
    async function setAccessMode() {
      const value = document.getElementById('accessMode').value;
      const res = await fetch('/api/settings/access-mode', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({access_mode:value}) });
      const data = await res.json();
      if (data.access_mode) document.getElementById('accessMode').value = data.access_mode;
    }
    let currentCall = '';
    function nowTime() { return new Date().toLocaleTimeString(); }
    function addActivity(id, tool) {
      const box = document.getElementById('activity');
      if (box.classList.contains('muted')) { box.classList.remove('muted'); box.textContent = ''; }
      const row = document.createElement('div');
      row.dataset.act = id;
      row.innerHTML = '<span class="pill">running</span> <strong style="display:inline">' + escapeHTML(tool) + '</strong> <small style="display:inline" class="muted">' + nowTime() + '</small>';
      box.prepend(row);
      while (box.children.length > 40) box.removeChild(box.lastChild);
    }
    function updateActivity(id, status, error) {
      const row = document.querySelector('#activity [data-act="' + id + '"]');
      if (!row) return;
      const pill = row.querySelector('.pill');
      pill.textContent = status;
      pill.className = 'pill ' + (status === 'error' ? 'bad' : 'ok');
      if (error) row.title = error;
    }
    function appendTerminal(text) {
      const pre = document.getElementById('terminal');
      pre.textContent = (pre.textContent + text).slice(-20000);
      pre.scrollTop = pre.scrollHeight;
    }
    function handleEvent(ev) {
      if (ev.type === 'activity') {
        if (ev.data === 'start') addActivity(ev.call_id, ev.tool);
        else updateActivity(ev.call_id, ev.status, ev.error);
        return;
      }
      if (ev.type === 'tool_start' && ev.tool === 'terminal.run') {
        currentCall = ev.call_id;
        document.getElementById('terminal').textContent = '';
        document.getElementById('terminalHeader').textContent = '$ ' + (ev.command || '') + (ev.project_id ? '  (' + ev.project_id + ')' : '');
      } else if (ev.type === 'terminal_output') {
        if (ev.call_id !== currentCall) {
          currentCall = ev.call_id;
          document.getElementById('terminal').textContent = '';
          document.getElementById('terminalHeader').textContent = '$ ' + (ev.command || '');
        }
        appendTerminal(ev.data || '');
      } else if (ev.type === 'tool_end') {
        if (ev.tool === 'terminal.run' && ev.call_id === currentCall) {
          appendTerminal('\n[exit: ' + (ev.status || '') + (ev.error ? ' ' + ev.error : '') + ']\n');
        }
        refreshHistory();
        if (selectedSession) refreshToolCalls();
        refreshSessions();
        updateGitBadges();
        refreshGitConsole();
      } else if (ev.type === 'history') {
        refreshHistory();
        updateGitBadges();
        refreshGitConsole();
      } else if (ev.type === 'approval') {
        refreshApprovals();
      } else if (ev.type === 'project') {
        refreshProjects();
      }
    }
    function connectEvents() {
      const dot = document.getElementById('liveDot');
      const es = new EventSource('/api/events');
      es.onopen = () => { dot.textContent = 'live'; dot.className = 'pill ok'; };
      es.onerror = () => { dot.textContent = 'reconnecting'; dot.className = 'pill bad'; };
      es.onmessage = (e) => { try { handleEvent(JSON.parse(e.data)); } catch (err) {} };
    }
    refreshProjects();
    refreshApprovals();
    refreshMCPs();
    refreshSkills();
    async function refreshAuth() {
      try {
        const data = await (await fetch('/api/auth')).json();
        const bar = document.getElementById('authbar');
        if (!data.enabled) { bar.textContent = ''; return; }
        bar.innerHTML = 'Signed in as <strong style="display:inline">' + escapeHTML(data.owner || '?') + '</strong> · <a href="#" id="logout">Logout</a>';
        document.getElementById('logout').onclick = (e) => { e.preventDefault(); location.href = '/auth/logout'; };
      } catch (e) {}
    }
    async function refreshGitHub() {
      const box = document.getElementById('github');
      try {
        const data = await (await fetch('/api/github')).json();
        if (!data.enabled) { box.textContent = ''; return; }
        if (data.connected) {
          box.innerHTML = 'GitHub: <strong style="display:inline">' + escapeHTML(data.login || '?') + '</strong> · <a href="#" id="ghDisconnect">Disconnect</a>';
          document.getElementById('ghDisconnect').onclick = async (e) => { e.preventDefault(); await fetch('/auth/github/disconnect', {method:'POST'}); refreshGitHub(); };
        } else {
          box.innerHTML = '<a href="/auth/github/login">Connect GitHub</a> <span class="muted">(for private repos)</span>';
        }
      } catch (e) { box.textContent = ''; }
    }
    refreshAccessMode();
    refreshAuth();
    refreshGitHub();
    connectEvents();
  </script>
</body>
</html>`
