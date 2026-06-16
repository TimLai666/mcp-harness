package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPListReloadsChangedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	sessionID := "mcp-hotplug"

	if err := AddMCPServer(MCPServerConfig{
		ID:        "one",
		Name:      "One",
		Transport: "stdio",
		Command:   "noop-one",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := rt.Run(context.Background(), RunRequest{
		SessionID: sessionID,
		Message: `<harness_tool_call>
{"tool":"mcp.list","args":{}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, res.Observations), `"id":"one"`) {
		t.Fatalf("expected first MCP config, got %#v", res.Observations)
	}

	if err := AddMCPServer(MCPServerConfig{
		ID:        "two",
		Name:      "Two",
		Transport: "stdio",
		Command:   "noop-two",
	}); err != nil {
		t.Fatal(err)
	}
	res, err = rt.Run(context.Background(), RunRequest{
		SessionID: sessionID,
		Message: `<harness_tool_call>
{"tool":"mcp.list","args":{}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustJSON(t, res.Observations)
	if !strings.Contains(got, `"id":"one"`) || !strings.Contains(got, `"id":"two"`) {
		t.Fatalf("expected changed MCP config to be visible immediately, got %s", got)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
