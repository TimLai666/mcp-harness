package web

import (
	"context"
	"encoding/json"
	"net/http"

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
	return http.ListenAndServe(addr, mux)
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
    .shell { min-height:100vh; display:grid; grid-template-columns:280px 1fr 430px; }
    aside, main, section { padding:18px; border-right:1px solid var(--line); }
    section { border-right:0; }
    h1, h2 { font-size:16px; margin:0 0 14px; }
    label { display:block; margin:12px 0 6px; color:var(--muted); font-size:12px; }
    input, textarea, select, button { width:100%; border:1px solid var(--line); border-radius:6px; padding:9px 10px; font:inherit; background:#fff; color:var(--text); }
    textarea { min-height:280px; resize:vertical; font-family:ui-monospace,SFMono-Regular,Consolas,monospace; }
    button { margin-top:12px; color:#fff; background:var(--accent); border-color:var(--accent); cursor:pointer; }
    .project { background:var(--panel); border:1px solid var(--line); border-radius:8px; padding:10px; margin-bottom:10px; }
    .project strong { display:block; }
    .project small { color:var(--muted); word-break:break-all; }
    pre { white-space:pre-wrap; word-break:break-word; background:#101828; color:#f9fafb; border-radius:8px; padding:12px; max-height:72vh; overflow:auto; }
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
      <label>Project</label><select id="projectSelect"><option value="">Default sandbox</option></select>
      <label>Mode</label><select id="mode"><option value="inspect">inspect</option><option value="work">work</option></select>
      <label>Message</label><textarea id="message">Read @README.md and summarize the harness status.</textarea>
      <button onclick="runHarness()">Run harness</button>
    </main>
    <section>
      <h2>Result</h2>
      <pre id="result">No run yet.</pre>
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
    }
    async function addProject() {
      const payload = { name: document.getElementById('newName').value || '', path: document.getElementById('newPath').value };
      const res = await fetch('/api/projects', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
      document.getElementById('result').textContent = JSON.stringify(await res.json(), null, 2);
      await refreshProjects();
    }
    async function runHarness() {
      const payload = { project: document.getElementById('projectSelect').value || '', mode: document.getElementById('mode').value, message: document.getElementById('message').value };
      const res = await fetch('/api/harness', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload) });
      const data = await res.json();
      if (data.system_prompt) data.system_prompt = data.system_prompt.slice(0, 4000) + "\n...";
      document.getElementById('result').textContent = JSON.stringify(data, null, 2);
    }
    refreshProjects();
  </script>
</body>
</html>`
