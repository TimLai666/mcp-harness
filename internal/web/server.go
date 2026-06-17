package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/mcpserver"
)

const mcpEndpoint = "/mcp"

func ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, NewHandler())
}

func NewHandler() http.Handler {
	rt := harness.NewRuntime()
	projects := harness.ProjectRegistry{}
	mux := http.NewServeMux()
	mcpHandler := mcpserver.StreamableHTTPHandler(rt, os.Getenv("MCP_HARNESS_MCP_BEARER_TOKEN"))
	mux.Handle(mcpEndpoint, mcpHandler)
	mux.Handle(mcpEndpoint+"/", mcpHandler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ok":            true,
			"mcp_endpoint":  mcpEndpoint,
			"mcp_transport": "streamable_http",
		})
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		list, err := projects.List()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"projects": list})
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
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
		project, err := projects.AddWithAllowedToolsets(req.Path, req.Name, req.ProjectID, req.Description, req.DefaultMode, req.AllowedToolsets)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"project": project})
	})
	mux.HandleFunc("GET /api/settings/access-mode", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"access_mode": harness.CurrentAccessMode()})
	})
	mux.HandleFunc("POST /api/settings/access-mode", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AccessMode harness.AccessMode `json:"access_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		if err := harness.SetAccessMode(req.AccessMode); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"access_mode": harness.CurrentAccessMode()})
	})
	mux.HandleFunc("GET /api/approvals", func(w http.ResponseWriter, r *http.Request) {
		records, err := (harness.ApprovalStore{}).List()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"approvals": records})
	})
	mux.HandleFunc("POST /api/approvals/", func(w http.ResponseWriter, r *http.Request) {
		id, action := splitApprovalPath(r.URL.Path)
		status := harness.ApprovalRejected
		if action == "approve" {
			status = harness.ApprovalApproved
		} else if action != "reject" {
			writeError(w, http.ErrNotSupported)
			return
		}
		record, err := (harness.ApprovalStore{}).SetStatus(id, status)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"approval": record})
	})
	mux.HandleFunc("GET /api/mcps", func(w http.ResponseWriter, r *http.Request) {
		servers, err := harness.LoadMCPServers()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"servers": servers})
	})
	mux.HandleFunc("POST /api/mcps", func(w http.ResponseWriter, r *http.Request) {
		var config harness.MCPServerConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			writeError(w, err)
			return
		}
		if err := harness.AddMCPServer(config); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"server": config})
	})
	mux.HandleFunc("DELETE /api/mcps/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/mcps/"):]
		if err := harness.DeleteMCPServer(id); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"removed": id})
	})
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"skills": harness.NewSkillRegistry().List()})
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		store, err := harness.DefaultStore()
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
		id := r.URL.Path[len("/api/sessions/"):]
		store, err := harness.DefaultStore()
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
		store, err := harness.DefaultStore()
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
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := harness.ListHistoryEvents(r.URL.Query().Get("project_id"), r.URL.Query().Get("session_id"), limit, r.URL.Query().Get("include_diff") == "true")
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"events": events})
	})
	mux.HandleFunc("GET /api/history/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/history/"):]
		event, err := harness.GetHistoryEvent(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"event": event})
	})
	mux.HandleFunc("POST /api/history/restore-preview", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VersionID string `json:"version_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		version, err := harness.LoadWorkspaceVersion(req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		preview, diff, truncated, err := harness.PreviewRestoreWorkspaceVersion(version.WorkspaceRoot, req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"version": preview, "diff": diff, "diff_truncated": truncated})
	})
	mux.HandleFunc("POST /api/history/restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VersionID string `json:"version_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		version, err := harness.LoadWorkspaceVersion(req.VersionID)
		if err != nil {
			writeError(w, err)
			return
		}
		restored, diff, truncated, err := harness.RestoreWorkspaceVersion(version.WorkspaceRoot, req.VersionID)
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
  </style>
</head>
<body>
  <div class="shell">
    <aside>
      <h1>mcp-harness</h1>
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
      const list = document.getElementById('projects');
      list.innerHTML = '';
      const sandbox = document.createElement('div');
      sandbox.className = 'card' + (selectedProject === '' ? ' selected' : '');
      sandbox.innerHTML = '<strong>Default sandbox</strong><small>Transient workspace</small>';
      sandbox.onclick = () => selectProject('', 'Default sandbox');
      list.appendChild(sandbox);
      for (const p of data.projects || []) {
        const div = document.createElement('div');
        div.className = 'card' + (selectedProject === p.id ? ' selected' : '');
        div.innerHTML = '<strong>' + escapeHTML(p.name) + '</strong><small>' + escapeHTML(p.path) + '</small>';
        div.onclick = () => selectProject(p.id, p.name);
        list.appendChild(div);
      }
      await refreshSessions();
      await refreshHistory();
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
        div.innerHTML = '<strong>' + escapeHTML(h.tool) + ' <span class="' + (h.status === 'ok' ? 'ok' : 'bad') + '">' + escapeHTML(h.status) + '</span></strong><small>step ' + h.step + ' - ' + escapeHTML(h.timestamp) + '</small><pre>' + escapeHTML(h.diff || 'No file diff for this step.') + '</pre><div class="grid2"><button class="secondary" data-version="' + h.before_version + '">Preview before</button><button class="secondary" data-version="' + h.after_version + '">Preview after</button></div>';
        div.onclick = (event) => {
          if (event.target.dataset.version) return previewRestore(event.target.dataset.version);
          setDetail(h);
        };
        list.appendChild(div);
      }
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
    refreshProjects();
    refreshApprovals();
    refreshMCPs();
    refreshSkills();
    refreshAccessMode();
  </script>
</body>
</html>`
