package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServerListsAndCallsHarnessTools(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	ctx := context.Background()
	server := mcpserver.New(harness.NewRuntime())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
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
	for _, name := range []string{"harness", "project_list", "skill_list", "mcp_list", "approval_list", "history_list", "history_show", "history_restore_preview"} {
		if !gotTools[name] {
			t.Fatalf("expected tool %q in MCP server list, got %#v", name, gotTools)
		}
	}

	projectList, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(t, projectList, "projects") {
		t.Fatalf("expected project_list structured result, got %#v", projectList)
	}

	run, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "harness",
		Arguments: map[string]any{
			"session_id": "mcp-e2e",
			"message": `<harness_tool_call>
{"tool":"workspace.list_files","args":{"path":".","max_entries":5}}
</harness_tool_call>`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(t, run, `"status":"ok"`) || !containsJSON(t, run, "workspace.list_files") {
		t.Fatalf("expected harness tool call result, got %#v", run)
	}
}

func containsJSON(t *testing.T, value any, needle string) bool {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(data), needle)
}
