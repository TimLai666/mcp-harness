package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeExecutesReadFileCall(t *testing.T) {
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
	res, err := rt.Run(context.Background(), RunRequest{
		Mode: ModeWork,
		Message: `<harness_tool_call>
{"tool":"workspace.read_file","args":{"path":"note.txt"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("unexpected status: %s", res.Status)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "ok" {
		t.Fatalf("unexpected observations: %#v", res.Observations)
	}
}

func TestRuntimeInjectsProjectInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("Use repo-specific rules."), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewRuntime().Run(context.Background(), RunRequest{
		Mode:    ModeInspect,
		Message: "inspect the project rules",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Instructions) != 1 || res.Instructions[0].Content != "Use repo-specific rules." {
		t.Fatalf("expected injected project instructions, got %#v", res.Instructions)
	}
	if !strings.Contains(res.SystemPrompt, "Use repo-specific rules.") {
		t.Fatal("expected system prompt to include project instructions")
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
	res, err := NewRuntime().Run(context.Background(), RunRequest{
		Project: "repo",
		Message: `<harness_tool_call>
{"tool":"git.status","args":{}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "error" || !strings.Contains(res.Observations[0].Error, "not allowed") {
		t.Fatalf("expected allowed_toolsets rejection, got %#v", res.Observations)
	}
}
