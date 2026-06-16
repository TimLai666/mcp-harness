package harness

import "testing"

func TestParseToolCalls(t *testing.T) {
	message := `<harness_tool_call>
{"tool":"workspace.read_file","args":{"path":"README.md"}}
</harness_tool_call>`
	calls, err := ParseToolCalls(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Tool != "workspace.read_file" {
		t.Fatalf("unexpected tool: %s", calls[0].Tool)
	}
	if calls[0].Args["path"] != "README.md" {
		t.Fatalf("unexpected path arg: %#v", calls[0].Args["path"])
	}
}

func TestParseToolCallsRejectsBadToolName(t *testing.T) {
	message := `<harness_tool_call>
{"tool":"Workspace.ReadFile","args":{"path":"README.md"}}
</harness_tool_call>`
	if _, err := ParseToolCalls(message); err == nil {
		t.Fatal("expected error")
	}
}
