package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/TimLai666/mcp-harness/internal/harness"
)

func ListenAndServe(addr string) error {
	rt := harness.NewRuntime()
	projects := harness.ProjectRegistry{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
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
			Path        string       `json:"path"`
			Name        string       `json:"name"`
			ProjectID   string       `json:"project_id"`
			Description string       `json:"description"`
			DefaultMode harness.Mode `json:"default_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		project, err := projects.Add(req.Path, req.Name, req.ProjectID, req.Description, req.DefaultMode)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"project": project})
	})
	mux.HandleFunc("POST /api/harness", func(w http.ResponseWriter, r *http.Request) {
		var req harness.RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err)
			return
		}
		res, err := rt.Run(context.Background(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, res)
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
	return http.ListenAndServe(addr, mux)
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
    :root { --bg:#f7f8fb; --panel:#fff; --line:#d9dee7; --text:#172033; --muted:#667085; --accent:#2563eb; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:14px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    .shell { min-height:100vh; display:grid; grid-template-columns:280px 1fr 460px; }
    aside, main, section { padding:18px; border-right:1px solid var(--line); }
    section { border-right:0; }
    h1, h2 { font-size:16px; margin:0 0 14px; }
    label { display:block; margin:12px 0 6px; color:var(--muted); font-size:12px; }
    input, textarea, select, button { width:100%; border:1px solid var(--line); border-radius:6px; padding:9px 10px; font:inherit; background:#fff; color:var(--text); }
    textarea { min-height:280px; resize:vertical; font-family:ui-monospace,SFMono-Regular,Consolas,monospace; }
    button { margin-top:12px; color:#fff; background:var(--accent); border-color:var(--accent); cursor:pointer; }
    .project, .approval, .mcp, .history { background:var(--panel); border:1px solid var(--line); border-radius:8px; padding:10px; margin-bottom:10px; }
    .project strong, .history strong { display:block; }
    .project small, .history small, .approval small, .mcp small { display:block; color:var(--muted); word-break:break-all; }
    pre { white-space:pre-wrap; word-break:break-word; background:#101828; color:#f9fafb; border-radius:8px; padding:12px; max-height:72vh; overflow:auto; }
    .diff { max-height:240px; margin:10px 0 0; font-size:12px; }
    .row { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
    .muted { color:var(--muted); }
  </style>
</head>
<body>
  <div class="shell">
    <aside>
      <h1>mcp-harness</h1>
      <p class="muted">Projects</p>
      <div id="projects"></div>
      <h2>Add Project</h2>
      <label>Name</label><input id="newName" placeholder="Optional display name">
      <label>Path</label><input id="newPath" placeholder="C:\\Users\\...\\repo">
      <button onclick="addProject()">Add project</button>
    </aside>
    <main>
      <h2>Harness Turn</h2>
      <label>Project</label><select id="projectSelect" onchange="refreshHistory()"><option value="">Default sandbox</option></select>
      <label>Mode</label><select id="mode"><option value="inspect">inspect</option><option value="work">work</option></select>
      <label>Access</label><select id="accessMode"><option value="default">default</option><option value="auto">auto</option><option value="full_access">full_access</option></select>
      <label>Message</label><textarea id="message">Read @README.md and summarize the harness status.</textarea>
      <button onclick="runHarness()">Run harness</button>
      <h2>Approvals</h2>
      <div id="approvals"></div>
      <h2>MCP Servers</h2>
      <div id="mcps"></div>
    </main>
    <section>
      <h2>Result</h2>
      <pre id="result">No run yet.</pre>
      <h2>Tool History</h2>
      <div id="history"></div>
    </section>
  </div>
  <script>
    async function refreshProjects() {
      const res = await fetch('/api/projects');
      const data = await res.json();
      const list = document.getElementById('projects');
      const select = document.getElementById('projectSelect');
      list.innerHTML = '';
      select.innerHTML = '<option value="">Default sandbox</option>';
      for (const p of data.projects || []) {
        const div = document.createElement('div');
        div.className = 'project';
        div.innerHTML = '<strong>' + p.name + '</strong><small>' + p.path + '</small>';
        list.appendChild(div);
        const option = document.createElement('option');
        option.value = p.id;
        option.textContent = p.name + ' (' + p.id + ')';
        select.appendChild(option);
      }
      await refreshHistory();
    }
    async function refreshApprovals() {
      const res = await fetch('/api/approvals');
      const data = await res.json();
      const list = document.getElementById('approvals');
      list.innerHTML = '';
      for (const a of data.approvals || []) {
        const div = document.createElement('div');
        div.className = 'approval';
        div.innerHTML = '<strong>' + a.tool + '</strong><small>' + a.status + ' · ' + a.reason + '</small>';
        if (a.status === 'pending') {
          const approve = document.createElement('button');
          approve.textContent = 'Approve';
          approve.onclick = async () => { await fetch('/api/approvals/' + a.id + '/approve', {method:'POST'}); refreshApprovals(); };
          div.appendChild(approve);
          const reject = document.createElement('button');
          reject.textContent = 'Reject';
          reject.onclick = async () => { await fetch('/api/approvals/' + a.id + '/reject', {method:'POST'}); refreshApprovals(); };
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
        div.className = 'mcp';
        div.innerHTML = '<strong>' + m.name + '</strong><small>' + m.id + ' · ' + m.transport + (m.trusted ? ' · trusted' : '') + '</small>';
        list.appendChild(div);
      }
    }
    async function refreshHistory() {
      const project = document.getElementById('projectSelect').value || '';
      const qs = new URLSearchParams({limit:'30', include_diff:'true'});
      if (project) qs.set('project_id', project);
      const res = await fetch('/api/history?' + qs.toString());
      const data = await res.json();
      const list = document.getElementById('history');
      list.innerHTML = '';
      for (const h of data.events || []) {
        const div = document.createElement('div');
        div.className = 'history';
        const title = document.createElement('strong');
        title.textContent = h.tool + ' · ' + h.status;
        const meta = document.createElement('small');
        meta.textContent = (h.project_name || 'Default sandbox') + ' · step ' + h.step + ' · ' + h.timestamp;
        const diff = document.createElement('pre');
        diff.className = 'diff';
        diff.textContent = h.diff || 'No file diff for this step.';
        const buttons = document.createElement('div');
        buttons.className = 'row';
        const before = document.createElement('button');
        before.textContent = 'Restore before';
        before.onclick = () => restoreVersion(h.before_version);
        const after = document.createElement('button');
        after.textContent = 'Restore after';
        after.onclick = () => restoreVersion(h.after_version);
        buttons.appendChild(before);
        buttons.appendChild(after);
        div.appendChild(title);
        div.appendChild(meta);
        div.appendChild(diff);
        div.appendChild(buttons);
        list.appendChild(div);
      }
    }
    async function restoreVersion(versionID) {
      if (!versionID || !confirm('Restore workspace files to this version?')) return;
      const res = await fetch('/api/history/restore', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({version_id:versionID}) });
      document.getElementById('result').textContent = JSON.stringify(await res.json(), null, 2);
      await refreshHistory();
    }
    async function addProject() {
      const payload = { name: document.getElementById('newName').value || '', path: document.getElementById('newPath').value };
      const res = await fetch('/api/projects', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
      document.getElementById('result').textContent = JSON.stringify(await res.json(), null, 2);
      await refreshProjects();
    }
    async function runHarness() {
      const payload = { project: document.getElementById('projectSelect').value || '', mode: document.getElementById('mode').value, access_mode: document.getElementById('accessMode').value, message: document.getElementById('message').value };
      const res = await fetch('/api/harness', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
      const data = await res.json();
      if (data.system_prompt) data.system_prompt = data.system_prompt.slice(0, 4000) + "\n...";
      document.getElementById('result').textContent = JSON.stringify(data, null, 2);
      await refreshApprovals();
      await refreshHistory();
    }
    refreshProjects();
    refreshApprovals();
    refreshMCPs();
  </script>
</body>
</html>`
