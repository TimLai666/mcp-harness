package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Even a read-only tool call must emit live activity events so the dashboard
// reacts the instant any MCP tool is called.
func TestEveryToolCallEmitsActivityEvents(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	ctx := context.Background()

	events, cancel := harness.DefaultBroker().Subscribe()
	defer cancel()

	server := New(harness.NewRuntime())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "activity-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	guide, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "harness", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	sid := ""
	for _, content := range guide.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			var payload struct {
				SessionID string `json:"session_id"`
			}
			if json.Unmarshal([]byte(text.Text), &payload) == nil && payload.SessionID != "" {
				sid = payload.SessionID
			}
		}
	}
	if sid == "" {
		t.Fatal("harness did not return a session_id")
	}
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_list", Arguments: map[string]any{"session_id": sid}}); err != nil {
		t.Fatal(err)
	}

	gotStart, gotEnd := false, false
	timeout := time.After(5 * time.Second)
	for !(gotStart && gotEnd) {
		select {
		case ev := <-events:
			if ev.Type == harness.EventActivity && ev.Tool == "project_list" {
				if ev.Data == "start" {
					gotStart = true
				} else if ev.Data == "end" && ev.Status == "ok" {
					gotEnd = true
				}
			}
		case <-timeout:
			t.Fatalf("expected activity start+end for project_list (start=%v end=%v)", gotStart, gotEnd)
		}
	}
}

func TestAllToolsAdvertiseOutputSchemaAndStructuredContent(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	ctx := context.Background()

	server := New(harness.NewRuntime())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("expected MCP tools")
	}
	for _, tool := range tools.Tools {
		if tool.OutputSchema == nil {
			t.Fatalf("tool %q is missing outputSchema", tool.Name)
		}
	}

	guide, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "harness", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if guide.StructuredContent == nil {
		t.Fatal("expected harness to return structured content")
	}

	guideMap, ok := guide.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected harness structured content to be an object, got %#v", guide.StructuredContent)
	}
	sid, _ := guideMap["session_id"].(string)
	if sid == "" {
		t.Fatal("expected harness structured content to include session_id")
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_list", Arguments: map[string]any{"session_id": sid}})
	if err != nil {
		t.Fatal(err)
	}
	if res.StructuredContent == nil {
		t.Fatal("expected project_list to return structured content")
	}
}
