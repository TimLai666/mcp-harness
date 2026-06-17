package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedProject(t *testing.T, id string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (ProjectRegistry{}).Add(root, id, id, "", ModeWork); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestProjectRenameRelocateEmitEvents(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	seedProject(t, "demo")
	newRoot := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	events, cancel := DefaultBroker().Subscribe()
	defer cancel()
	rt := NewRuntime()

	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:    "project.rename",
		Project: "demo",
		Args:    map[string]any{"name": "Renamed Demo"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("rename failed: %#v err=%v", res, err)
	}
	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:    "project.relocate",
		Project: "demo",
		Args:    map[string]any{"path": newRoot},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("relocate failed: %#v err=%v", res, err)
	}

	projects, err := (ProjectRegistry{}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Renamed Demo" || filepath.Clean(projects[0].Path) != filepath.Clean(newRoot) {
		t.Fatalf("expected renamed and relocated project, got %#v", projects)
	}

	if !waitForProjectEvents(events, map[string]bool{"renamed": false, "relocated": false}) {
		t.Fatal("expected renamed and relocated project events")
	}
}

func TestProjectRemoveDeletesManagedWorkspaceOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))

	// External directory: remove must unregister but refuse to delete files.
	external := seedProject(t, "external")
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:    "project.remove",
		Project: "external",
		Args:    map[string]any{"delete_files": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" {
		t.Fatalf("expected refusal to delete files outside managed workspaces, got %#v", res)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external files must not be deleted: %v", err)
	}

	// Managed workspace: create then remove with delete_files should delete it.
	create, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "project.create",
		Args: map[string]any{"name": "Managed", "project_id": "managed"},
	})
	if err != nil || create.Status != "ok" {
		t.Fatalf("create failed: %#v err=%v", create, err)
	}
	managedPath := filepath.Join(home, "workspaces", "managed")
	if _, err := os.Stat(managedPath); err != nil {
		t.Fatalf("expected managed workspace dir: %v", err)
	}
	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:    "project.remove",
		Project: "managed",
		Args:    map[string]any{"delete_files": true},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("managed remove failed: %#v err=%v", res, err)
	}
	if _, err := os.Stat(managedPath); !os.IsNotExist(err) {
		t.Fatalf("expected managed workspace dir to be deleted, stat err=%v", err)
	}
	projects, err := (ProjectRegistry{}).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.ID == "managed" {
			t.Fatalf("managed project should be unregistered, got %#v", projects)
		}
	}
}

func waitForProjectEvents(events <-chan Event, want map[string]bool) bool {
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == EventProject {
				if _, ok := want[ev.Status]; ok {
					want[ev.Status] = true
				}
			}
			all := true
			for _, seen := range want {
				if !seen {
					all = false
				}
			}
			if all {
				return true
			}
		case <-timeout:
			return false
		}
	}
}
