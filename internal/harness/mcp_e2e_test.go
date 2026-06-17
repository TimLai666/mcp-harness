package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const runSchemaMCPServerEnv = "MCP_HARNESS_RUN_SCHEMA_TEST_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(runSchemaMCPServerEnv) == "1" {
		runSchemaMCPServer()
		return
	}
	os.Exit(m.Run())
}

func TestMCPCallValidatesExternalToolSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMCPServer(MCPServerConfig{
		ID:        "schema",
		Name:      "Schema",
		Transport: "stdio",
		Command:   exe,
		Env:       map[string]string{runSchemaMCPServerEnv: "1"},
		Trusted:   true,
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "mcp.call",
		SessionID: "mcp-schema",
		Args:      map[string]any{"server": "schema", "tool": "greet", "arguments": map[string]any{"extra": true}, "timeout_ms": 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "error" || !strings.Contains(res.Error, "$.name is required") {
		t.Fatalf("expected local schema validation error, got %#v", res)
	}

	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "mcp.call",
		SessionID: "mcp-schema",
		Args:      map[string]any{"server": "schema", "tool": "greet", "arguments": map[string]any{"name": "Ada"}, "timeout_ms": 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected valid external MCP call to pass, got %#v", res)
	}
	data, err := json.Marshal(res.Result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Hi Ada") {
		t.Fatalf("expected external MCP response, got %s", data)
	}
}

func runSchemaMCPServer() {
	server := mcp.NewServer(&mcp.Implementation{Name: "schema-test", Version: "0.1.0"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "greet",
		Description: "greet a user",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required":             []any{"name"},
			"additionalProperties": false,
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Hi " + fmt.Sprint(args["name"])}},
		}, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
