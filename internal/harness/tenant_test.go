package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTenantsAreIsolated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	rt := NewRuntime()

	// Two tenants each create a managed workspace and write a file in their sandbox.
	for _, owner := range []string{"alice", "bob"} {
		if _, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
			Owner: owner, Tool: "project.create",
			Args: map[string]any{"name": owner + "-proj", "project_id": owner + "proj"},
		}); err != nil {
			t.Fatalf("%s create: %v", owner, err)
		}
		if _, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
			Owner: owner, SessionID: owner + "-sess", Tool: "workspace.write_file",
			Args: map[string]any{"path": "note.txt", "content": owner},
		}); err != nil {
			t.Fatalf("%s write: %v", owner, err)
		}
	}

	// Each tenant only sees its own project.
	aliceProjects, _ := (ProjectRegistry{Owner: "alice"}).List()
	if len(aliceProjects) != 1 || aliceProjects[0].ID != "aliceproj" {
		t.Fatalf("alice should see only her project, got %#v", aliceProjects)
	}
	bobProjects, _ := (ProjectRegistry{Owner: "bob"}).List()
	if len(bobProjects) != 1 || bobProjects[0].ID != "bobproj" {
		t.Fatalf("bob should see only his project, got %#v", bobProjects)
	}

	// History is per-tenant: alice's DB must not contain bob's session, and
	// vice versa.
	aliceHist, _ := ListHistoryEventsFor("alice", "", "", 50, false)
	bobHist, _ := ListHistoryEventsFor("bob", "", "", 50, false)
	if len(aliceHist) == 0 || len(bobHist) == 0 {
		t.Fatalf("expected history for both tenants (alice=%d bob=%d)", len(aliceHist), len(bobHist))
	}
	for _, e := range aliceHist {
		if e.SessionID == "bob-sess" {
			t.Fatalf("alice history leaked bob's session")
		}
	}
	for _, e := range bobHist {
		if e.SessionID == "alice-sess" {
			t.Fatalf("bob history leaked alice's session")
		}
	}

	// Tenant data lives in separate directories.
	for _, owner := range []string{"alice", "bob"} {
		dbPath := filepath.Join(home, "tenants", owner, "harness.db")
		if _, err := os.Stat(dbPath); err != nil {
			t.Fatalf("expected isolated db for %s at %s: %v", owner, dbPath, err)
		}
	}
	// The default tenant's root DB must not contain tenant projects.
	rootProjects, _ := (ProjectRegistry{}).List()
	if len(rootProjects) != 0 {
		t.Fatalf("default tenant must not see alice/bob projects, got %#v", rootProjects)
	}
}
