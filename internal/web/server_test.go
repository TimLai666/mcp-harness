package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConsoleHTMLHasNoChatHarnessRunner(t *testing.T) {
	for _, forbidden := range []string{
		`<textarea id="message"`,
		`runHarness()`,
		`Harness Turn`,
	} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("console HTML should not contain %q", forbidden)
		}
	}
	for _, required := range []string{
		`/api/sessions`,
		`/api/tool-calls`,
		`/api/history/restore-preview`,
		`/api/events`,
		`Sessions`,
		`Tool Calls`,
		`Approvals`,
		`renderDiffHTML`,
		`new EventSource`,
		`diff-table`,
		`Live Terminal`,
		`MCP Activity`,
		`addActivity`,
		`/api/git`,
		`updateGitBadges`,
		`/api/github`,
		`refreshGitHub`,
		`/api/git/checkout`,
		`/api/git/add`,
		`/api/git/fetch`,
		`/api/git/pull`,
		`/api/git/commit`,
		`/api/git/push`,
		`Switch Branch`,
		`Stage And Sync`,
		`Commit And Push`,
		`refreshGitConsole`,
		`checkoutSelectedBranch`,
		`stageAllChanges`,
		`stageSelectedChanges`,
		`fetchChanges`,
		`pullChanges`,
		`commitChanges`,
		`pushChanges`,
	} {
		if !strings.Contains(indexHTML, required) {
			t.Fatalf("console HTML should contain %q", required)
		}
	}
}

func TestOIDCEnabledGatesAPIAndPublishesMetadata(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	t.Setenv("MCP_HARNESS_OIDC_ISSUER", "https://example.logto.app/oidc")
	t.Setenv("MCP_HARNESS_OIDC_AUDIENCE", "mcp-harness")
	t.Setenv("MCP_HARNESS_OIDC_CLIENT_ID", "client-123")
	t.Setenv("MCP_HARNESS_PUBLIC_URL", "https://mcp.example.com")

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	// Protected-resource metadata must advertise the Logto authorization server.
	res, err := http.Get(server.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("metadata returned %d", res.StatusCode)
	}
	var meta struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(res.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.Resource != "https://mcp.example.com/mcp" || len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != "https://example.logto.app/oidc" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}

	// Without a session cookie, the dashboard API must require login.
	res2, err := http.Get(server.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/projects, got %d", res2.StatusCode)
	}
}

func TestEventsEndpointStreams(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	server := httptest.NewServer(NewHandler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/events returned %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
	buf := make([]byte, 64)
	n, _ := res.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "connected") {
		t.Fatalf("expected initial SSE frame, got %q", string(buf[:n]))
	}
}

func TestConsoleAPISmoke(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	for _, path := range []string{
		"/api/health",
		"/api/projects",
		"/api/sessions",
		"/api/history?limit=5",
		"/api/approvals",
	} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if res.StatusCode != http.StatusOK {
			_ = res.Body.Close()
			t.Fatalf("GET %s returned %d", path, res.StatusCode)
		}
		_ = res.Body.Close()
	}

	res, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / returned %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("MCP_HARNESS_HOME"), "harness.db")); err != nil {
		t.Fatalf("expected SQLite DB to be created: %v", err)
	}
}

func TestRemoteMCPEndpointListsAndCallsTools(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "web-test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp",
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	gotTools := map[string]bool{}
	for _, tool := range tools.Tools {
		gotTools[tool.Name] = true
	}
	if !gotTools["harness"] || !gotTools["project_list"] {
		t.Fatalf("expected remote MCP tools, got %#v", gotTools)
	}

	guide, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "harness", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	sid := sessionIDFromResult(t, guide)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "project_list",
		Arguments: map[string]any{"session_id": sid},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(t, result, "projects") {
		t.Fatalf("expected project_list structured result, got %#v", result)
	}
}

func sessionIDFromResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			var payload struct {
				SessionID string `json:"session_id"`
			}
			if json.Unmarshal([]byte(text.Text), &payload) == nil && payload.SessionID != "" {
				return payload.SessionID
			}
		}
	}
	t.Fatal("no session_id in harness guide result")
	return ""
}

func TestRemoteMCPEndpointCanRequireBearerToken(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	t.Setenv("MCP_HARNESS_MCP_BEARER_TOKEN", "secret")

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	res, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		_ = res.Body.Close()
		t.Fatalf("expected unauthorized without token, got %d", res.StatusCode)
	}
	_ = res.Body.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "web-auth-test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp",
		HTTPClient:           bearerHTTPClient("secret"),
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
}

func TestGitControlEndpoints(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	repo := filepath.Join(t.TempDir(), "repo")
	remote := filepath.Join(t.TempDir(), "remote.git")
	peer := filepath.Join(t.TempDir(), "peer")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, repo, "init")
	mustRunGit(t, repo, "config", "user.email", "test@example.com")
	mustRunGit(t, repo, "config", "user.name", "Test User")
	mustRunGit(t, remote, "init", "--bare")
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, repo, "add", "note.txt")
	mustRunGit(t, repo, "commit", "-m", "init")
	mustRunGit(t, repo, "remote", "add", "origin", remote)
	current := strings.TrimSpace(runGitOutputWeb(t, repo, "branch", "--show-current"))
	mustRunGit(t, repo, "push", "-u", "origin", current)
	if _, err := (harness.ProjectRegistry{}).Add(repo, "Repo", "repo", "", harness.ModeWork); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	res, err := http.Get(server.URL + "/api/git?project=repo&branches=true")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("git info returned %d", res.StatusCode)
	}
	var gitPayload struct {
		Git           harness.GitInfo          `json:"git"`
		Branches      []harness.GitBranch      `json:"branches"`
		StatusEntries []harness.GitStatusEntry `json:"status_entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&gitPayload); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if !gitPayload.Git.IsRepo || len(gitPayload.Branches) == 0 {
		t.Fatalf("expected git info + branches, got %#v", gitPayload)
	}

	postJSON := func(path string, body any) map[string]any {
		t.Helper()
		buf, _ := json.Marshal(body)
		res, err := http.Post(server.URL+path, "application/json", bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			var payload map[string]any
			_ = json.NewDecoder(res.Body).Decode(&payload)
			t.Fatalf("POST %s returned %d: %#v", path, res.StatusCode, payload)
		}
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["status"] != "ok" {
			t.Fatalf("POST %s expected ok, got %#v", path, payload)
		}
		return payload
	}

	postJSON("/api/git/checkout", map[string]any{"project": "repo", "ref": "feature/web-ui", "create": true})
	if got := strings.TrimSpace(runGitOutputWeb(t, repo, "branch", "--show-current")); got != "feature/web-ui" {
		t.Fatalf("expected checked out feature/web-ui, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "draft.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = http.Get(server.URL + "/api/git?project=repo&status_entries=true")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("git status entries returned %d", res.StatusCode)
	}
	var statusPayload struct {
		StatusEntries []harness.GitStatusEntry `json:"status_entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&statusPayload); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	var sawTracked, sawUntracked bool
	for _, entry := range statusPayload.StatusEntries {
		if entry.Path == "note.txt" && entry.Unstaged {
			sawTracked = true
		}
		if entry.Path == "draft.txt" && entry.Untracked {
			sawUntracked = true
		}
	}
	if !sawTracked || !sawUntracked {
		t.Fatalf("expected tracked + untracked status entries, got %#v", statusPayload.StatusEntries)
	}

	postJSON("/api/git/add", map[string]any{"project": "repo", "paths": []string{"note.txt", "draft.txt"}})
	statusShort := runGitOutputWeb(t, repo, "status", "--short")
	if !strings.Contains(statusShort, "M  note.txt") || !strings.Contains(statusShort, "A  draft.txt") {
		t.Fatalf("expected staged files after git/add, got %q", statusShort)
	}
	postJSON("/api/git/commit", map[string]any{"project": "repo", "message": "web ui change", "all": false})
	postJSON("/api/git/push", map[string]any{"project": "repo", "remote": "origin", "branch": "feature/web-ui", "set_upstream": true})
	if out := runGitDirOutputWeb(t, remote, "show-ref", "--verify", "refs/heads/feature/web-ui"); !strings.Contains(out, "refs/heads/feature/web-ui") {
		t.Fatalf("expected pushed remote branch, got %q", out)
	}

	mustRunGit(t, "", "clone", remote, peer)
	mustRunGit(t, peer, "config", "user.email", "test@example.com")
	mustRunGit(t, peer, "config", "user.name", "Test User")
	mustRunGit(t, peer, "checkout", "feature/web-ui")
	if err := os.WriteFile(filepath.Join(peer, "note.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, peer, "add", "note.txt")
	mustRunGit(t, peer, "commit", "-m", "remote update")
	mustRunGit(t, peer, "push", "origin", "feature/web-ui")

	postJSON("/api/git/fetch", map[string]any{"project": "repo", "remote": "origin"})
	res, err = http.Get(server.URL + "/api/git?project=repo")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("git info after fetch returned %d", res.StatusCode)
	}
	var fetchedPayload struct {
		Git harness.GitInfo `json:"git"`
	}
	if err := json.NewDecoder(res.Body).Decode(&fetchedPayload); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if fetchedPayload.Git.Behind < 1 {
		t.Fatalf("expected branch to be behind after fetch, got %#v", fetchedPayload.Git)
	}

	postJSON("/api/git/pull", map[string]any{"project": "repo", "remote": "origin", "branch": "feature/web-ui", "ff_only": true})
	if content, err := os.ReadFile(filepath.Join(repo, "note.txt")); err != nil {
		t.Fatal(err)
	} else if strings.TrimSpace(string(content)) != "three" {
		t.Fatalf("expected pulled note.txt content, got %q", content)
	}
}

func bearerHTTPClient(token string) *http.Client {
	return &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}}
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func containsJSON(t *testing.T, value any, needle string) bool {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(data), needle)
}

func mustRunGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmdArgs := args
	if root != "" {
		cmdArgs = append([]string{"-C", root}, args...)
	}
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func runGitOutputWeb(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func runGitDirOutputWeb(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir", gitDir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git --git-dir %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
