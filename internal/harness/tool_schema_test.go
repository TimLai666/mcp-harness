package harness

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltinToolSchemasDescribeArgs(t *testing.T) {
	schemas := BuiltinToolSchemas()
	readFile, ok := schemas["workspace.read_file"]
	if !ok {
		t.Fatal("expected workspace.read_file schema")
	}
	path, ok := readFile.Args["path"]
	if !ok || !path.Required {
		t.Fatalf("expected required path arg, got %#v", readFile.Args)
	}
	move, ok := schemas["workspace.move"]
	if !ok {
		t.Fatal("expected workspace.move schema")
	}
	if !move.Args["source_path"].Required || !move.Args["destination_path"].Required {
		t.Fatalf("expected required move args, got %#v", move.Args)
	}
}

func TestExecuteRejectsInvalidToolArgsBeforeHandler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.read_file",
		Args: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" || !strings.Contains(res.Error, `missing required arg "path"`) {
		t.Fatalf("expected missing arg error, got %#v", res)
	}
}

func TestExecuteRejectsUnknownToolArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "git.status",
		Args: map[string]any{"surprise": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" || !strings.Contains(res.Error, "unknown arg") {
		t.Fatalf("expected unknown arg error, got %#v", res)
	}
}
