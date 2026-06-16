package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		`Sessions`,
		`Tool Calls`,
		`Approvals`,
	} {
		if !strings.Contains(indexHTML, required) {
			t.Fatalf("console HTML should contain %q", required)
		}
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
