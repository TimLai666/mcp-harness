package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	} {
		if !strings.Contains(indexHTML, required) {
			t.Fatalf("console HTML should contain %q", required)
		}
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

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "project_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(t, result, "projects") {
		t.Fatalf("expected project_list structured result, got %#v", result)
	}
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
