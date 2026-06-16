package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteStoreMigratesEmptyDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected no projects, got %#v", projects)
	}
	if _, err := os.Stat(filepath.Join(home, "harness.db")); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStoreImportsLegacyFilesOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	legacyProjects := projectsFile{Projects: []Project{{
		ID:          "legacy",
		Name:        "Legacy",
		Path:        home,
		DefaultMode: ModeInspect,
	}}}
	writeJSONFile(t, filepath.Join(home, "projects.json"), legacyProjects)
	writeJSONFile(t, filepath.Join(home, "mcps.json"), mcpConfigFile{Servers: []MCPServerConfig{{
		ID:        "legacy-mcp",
		Name:      "Legacy MCP",
		Transport: "stdio",
		Command:   "noop",
	}}})

	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != "legacy" {
		t.Fatalf("expected imported project, got %#v", projects)
	}
	store, err = DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	servers, err := store.ListMCPServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ID != "legacy-mcp" {
		t.Fatalf("expected imported server, got %#v", servers)
	}

	legacyProjects.Projects = append(legacyProjects.Projects, Project{ID: "late", Name: "Late", Path: home, DefaultMode: ModeInspect})
	writeJSONFile(t, filepath.Join(home, "projects.json"), legacyProjects)
	store, err = DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	projects, err = store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("legacy import should not rerun after DB exists, got %#v", projects)
	}
}

func TestSQLiteStorePersistsProjectApprovalHistoryAndSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	if _, err := (ProjectRegistry{}).Add(home, "Home", "home", "", ModeWork); err != nil {
		t.Fatal(err)
	}
	res, err := NewRuntime().Run(context.Background(), RunRequest{
		Project:    "home",
		Mode:       ModeWork,
		AccessMode: AccessFullAccess,
		SessionID:  "sqlite-session",
		Message: `<harness_tool_call>
{"tool":"workspace.write_file","args":{"path":"note.txt","content":"hello"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.HistoryEvents) != 1 {
		t.Fatalf("expected history event, got %#v", res.HistoryEvents)
	}
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions("home", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sqlite-session" || sessions[0].TurnCount != 1 {
		t.Fatalf("expected persisted session, got %#v", sessions)
	}
	store, err = DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	calls, err := store.ListToolCalls("sqlite-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Tool != "workspace.write_file" || calls[0].Status != "ok" {
		t.Fatalf("expected persisted tool call, got %#v", calls)
	}
	store, err = DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListHistoryEvents("home", "", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Tool != "workspace.write_file" {
		t.Fatalf("expected persisted history, got %#v", events)
	}
	blobEntries, err := os.ReadDir(filepath.Join(home, "history", "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blobEntries) == 0 {
		t.Fatal("expected workspace versions to be stored as snapshot blobs")
	}
	if filepath.Ext(blobEntries[0].Name()) != ".gz" {
		t.Fatalf("expected compressed snapshot blob, got %s", blobEntries[0].Name())
	}
	store, err = DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.LoadWorkspaceVersion(events[0].AfterVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got := version.Snapshot.Files["note.txt"].Content; got != "hello" {
		t.Fatalf("expected loaded snapshot content, got %q", got)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
