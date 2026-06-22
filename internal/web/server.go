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
		writeJSON(w, map[string]any{"git": harness.WorkspaceGitInfo(workspace.Root)})
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
    :root { --bg:#f6f7f9; --panel:#fff; --line:#d7dce5; --text:#172033; --muted:#667085; --accent:#2563eb; --bad:#b42318; --ok:#067647; }
    * { box-sizing:border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:14px/1.45 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    .shell { min-height:100vh; display:grid; grid-template-columns:300px minmax(460px,1fr) 480px; }
    aside, main, section { padding:16px; border-right:1px solid var(--line); overflow:auto; max-height:100vh; }
    section { border-right:0; }
    h1, h2 { font-size:16px; margin:0 0 12px; }
    h3 { font-size:13px; margin:14px 0 8px; color:var(--muted); text-transform:uppercase; letter-spacing:.04em; }
    label { display:block; margin:10px 0 5px; color:var(--muted); font-size:12px; }
    input, select, button { width:100%; border:1px solid var(--line); border-radius:6px; padding:8px 9px; font:inherit; background:#fff; color:var(--text); }
    button { margin-top:8px; color:#fff; background:var(--accent); border-color:var(--accent); cursor:pointer; }
    button.secondary { color:var(--text); background:#fff; border-color:var(--line); }
    button.danger { background:var(--bad); border-color:var(--bad); }
    .card { background:var(--panel); border:1px solid var(--line); border-radius:8px; padding:10px; margin-bottom:10px; }
    .card strong { display:block; }
    .card small { display:block; color:var(--muted); word-break:break-all; }
    .selected { border-color:var(--accent); box-shadow:0 0 0 1px var(--accent) inset; }
    .grid2 { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
    .pill { display:inline-block; border:1px solid var(--line); border-radius:999px; padding:2px 7px; color:var(--muted); font-size:12px; margin-right:4px; }
    pre { white-space:pre-wrap; word-break:break-word; background:#101828; color:#f9fafb; border-radius:8px; padding:12px; max-height:420px; overflow:auto; font-size:12px; }
    .muted { color:var(--muted); }
    .ok { color:var(--ok); }
    .bad { color:var(--bad); }
    .diff { border:1px solid var(--line); border-radius:8px; overflow:hidden; margin-top:6px; background:#0d1117; }
    .diff-file { border-top:1px solid #21262d; }
    .diff-file:first-child { border-top:0; }
    .diff-file-head { background:#161b22; color:#c9d1d9; padding:5px 10px; font:12px/1.4 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; word-break:break-all; }
    .diff-table { width:100%; border-collapse:collapse; table-layout:fixed; font:12px/1.5 ui-monospace,SFMono-Regular,Consolas,Menlo,monospace; }
    .diff-table td { vertical-align:top; padding:0; }
    .diff-table td.ln { width:42px; text-align:right; padding:0 6px; color:#6e7681; background:#0d1117; user-select:none; border-right:1px solid #21262d; white-space:nowrap; }
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
    #terminal .hljs { background:transparent; padding:0; }
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
      <h2 id="projectTitle">Default sandbox</h2>
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
      <div id="detail" class="card muted">Select a session, tool call, history event, or approval.</div>
      <h3>MCP Activity <span id="liveDot" class="pill">offline</span></h3>
      <div id="activity" class="card muted">No MCP calls yet.</div>
      <h3>Live Terminal</h3>
      <div class="card">
        <small id="terminalHeader" class="muted">Waiting for terminal_run output…</small>
        <pre id="terminal"></pre>
      </div>
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
      <h3>MCP Servers</h3>
      <div id="mcps"></div>
      <h3>Skills</h3>
      <div id="skills"></div>
    </section>
  </div>
  <script>
    let selectedProject = '';
    let selectedSession = '';
    const escapeHTML = (text) => String(text).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    function setDetail(value) {
      const detail = document.getElementById('detail');
      detail.className = 'card';
      detail.innerHTML = '<pre>' + escapeHTML(typeof value === 'string' ? value : JSON.stringify(value, null, 2)) + '</pre>';
    }
    async function refreshProjects() {
      const res = await fetch('/api/projects');
      const data = await res.json();
      const projects = data.projects || [];
      if (selectedProject && !projects.some(p => p.id === selectedProject)) {
        selectedProject = '';
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
      updateGitBadges();
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
        div.innerHTML = '<strong>' + escapeHTML(h.tool) + ' <span class="' + (h.status === 'ok' ? 'ok' : 'bad') + '">' + escapeHTML(h.status) + '</span></strong><small>step ' + h.step + ' - ' + escapeHTML(h.timestamp) + (h.diff_truncated ? ' - diff truncated' : '') + '</small>' + renderDiffHTML(h.diff) + '<div class="grid2"><button class="secondary" data-version="' + h.before_version + '">Preview before</button><button class="secondary" data-version="' + h.after_version + '">Preview after</button></div>';
        div.onclick = (event) => {
          if (event.target.dataset.version) return previewRestore(event.target.dataset.version);
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
    async function previewRestore(versionID) {
      const res = await fetch('/api/history/restore-preview', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({version_id:versionID}) });
      const data = await res.json();
      setDetail(data);
      if (!data.error && confirm('Restore workspace files to this previewed version?')) {
        const restore = await fetch('/api/history/restore', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({version_id:versionID}) });
        setDetail(await restore.json());
        await refreshHistory();
      }
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
          approve.onclick = async (event) => { event.stopPropagation(); await fetch('/api/approvals/' + a.id + '/approve', {method:'POST'}); refreshApprovals(); };
          div.appendChild(approve);
          const reject = document.createElement('button');
          reject.className = 'danger';
          reject.textContent = 'Reject';
          reject.onclick = async (event) => { event.stopPropagation(); await fetch('/api/approvals/' + a.id + '/reject', {method:'POST'}); refreshApprovals(); };
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
      } else if (ev.type === 'history') {
        refreshHistory();
        updateGitBadges();
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
