package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteToolReadsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.read_file",
		Args: map[string]any{"path": "note.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("unexpected status: %s (%s)", res.Status, res.Error)
	}
	result, ok := res.Result.(map[string]any)
	if !ok || result["content"] != "hello" {
		t.Fatalf("unexpected result: %#v", res.Result)
	}
}

func TestGuideInjectsProjectInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("Use repo-specific rules."), 0o644); err != nil {
		t.Fatal(err)
	}
	guide := NewRuntime().Guide("")
	if len(guide.ProjectInstructions) != 1 || guide.ProjectInstructions[0].Content != "Use repo-specific rules." {
		t.Fatalf("expected injected project instructions, got %#v", guide.ProjectInstructions)
	}
}

func TestProjectAllowedToolsetsAreEnforced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	projectRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveProjects([]Project{{
		ID:              "repo",
		Name:            "Repo",
		Path:            projectRoot,
		DefaultMode:     ModeInspect,
		AllowedToolsets: []string{"workspace"},
	}}); err != nil {
		t.Fatal(err)
	}
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool:    "git.status",
		Project: "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" || res.Error == "" {
		t.Fatalf("expected allowed_toolsets rejection, got %#v", res)
	}
}
