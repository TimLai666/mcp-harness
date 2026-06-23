package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceReadFileReturnsLineNumbers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "a.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.read_file",
		Args: map[string]any{"path": "a.txt"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("read failed: %#v err=%v", res, err)
	}
	result := res.Result.(map[string]any)
	numbered, _ := result["numbered_content"].(string)
	if !strings.Contains(numbered, "1\talpha") || !strings.Contains(numbered, "3\tgamma") {
		t.Fatalf("expected line-numbered content, got %q", numbered)
	}
}

func TestWorkspaceReplaceLinesEditsFragment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sandbox, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()

	// Replace lines 2-3 ("two","three") with a single line.
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.replace_lines",
		Args: map[string]any{"path": "a.txt", "start_line": 2, "end_line": 3, "content": "TWO-THREE"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("replace_lines failed: %#v err=%v", res, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "one\nTWO-THREE\nfour\n" {
		t.Fatalf("unexpected content after replace: %q", string(got))
	}

	// Insert before line 1 using an empty range (end_line = start_line-1).
	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.replace_lines",
		Args: map[string]any{"path": "a.txt", "start_line": 1, "end_line": 0, "content": "HEADER"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("insert failed: %#v err=%v", res, err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "HEADER\none\nTWO-THREE\nfour\n" {
		t.Fatalf("unexpected content after insert: %q", string(got))
	}
}

func TestWorkspaceMkdirMoveDelete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sandbox, "draft.txt")
	if err := os.WriteFile(source, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()

	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.mkdir",
		Args: map[string]any{"path": "notes/archive"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("mkdir failed: %#v err=%v", res, err)
	}
	if info, err := os.Stat(filepath.Join(sandbox, "notes", "archive")); err != nil || !info.IsDir() {
		t.Fatalf("expected created directory, stat err=%v info=%#v", err, info)
	}

	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.move",
		Args: map[string]any{"source_path": "draft.txt", "destination_path": "notes/archive/final.txt"},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("move failed: %#v err=%v", res, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source to move away, stat err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(sandbox, "notes", "archive", "final.txt")); err != nil || string(got) != "hello\n" {
		t.Fatalf("expected moved file content, err=%v got=%q", err, string(got))
	}

	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.delete",
		Args: map[string]any{"path": "notes", "recursive": true},
	})
	if err != nil || res.Status != "ok" {
		t.Fatalf("delete failed: %#v err=%v", res, err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "notes")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted directory, stat err=%v", err)
	}
}

func TestWorkspaceDeleteRefusesWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.delete",
		Args: map[string]any{"path": ".", "recursive": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" || !strings.Contains(res.Error, "workspace root") {
		t.Fatalf("expected root-delete refusal, got %#v", res)
	}
}

func TestListSessionsScopedToProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	if _, err := (ProjectRegistry{}).Add(home, "Repo", "repo", "", ModeWork); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	// A project session.
	if _, err := rt.ExecuteTool(context.Background(), ToolCallRequest{Tool: "workspace.write_file", Project: "repo", SessionID: "proj-sess", Args: map[string]any{"path": "p.txt", "content": "x"}}); err != nil {
		t.Fatal(err)
	}
	// A sandbox session.
	if _, err := rt.ExecuteTool(context.Background(), ToolCallRequest{Tool: "workspace.write_file", SessionID: "sandbox-sess", Args: map[string]any{"path": "s.txt", "content": "y"}}); err != nil {
		t.Fatal(err)
	}

	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := store.ListSessions("", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sandbox {
		if s.ProjectID != "" {
			t.Fatalf("sandbox session list must not include project sessions, got %#v", s)
		}
	}
	if !containsSession(sandbox, "sandbox-sess") || containsSession(sandbox, "proj-sess") {
		t.Fatalf("sandbox scope wrong: %#v", sandbox)
	}
	store, _ = DefaultStore()
	proj, err := store.ListSessions("repo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSession(proj, "proj-sess") || containsSession(proj, "sandbox-sess") {
		t.Fatalf("project scope wrong: %#v", proj)
	}
}

func containsSession(sessions []SessionRecord, id string) bool {
	for _, s := range sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}
