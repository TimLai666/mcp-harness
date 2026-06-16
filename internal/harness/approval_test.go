package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAccessQueuesMutationForApproval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Mode: ModeWork,
		Message: `<harness_tool_call>
{"tool":"workspace.write_file","args":{"path":"note.txt","content":"hello"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "approval_required" {
		t.Fatalf("expected approval_required, got %#v", res.Observations)
	}
	if _, err := os.Stat(filepath.Join(home, "sandbox", "note.txt")); err == nil {
		t.Fatal("file should not be written before approval")
	}
}

func TestAutoAccessRequiresAgentAuthorizationReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Mode:       ModeWork,
		AccessMode: AccessAuto,
		Message: `<harness_tool_call>
{"tool":"workspace.write_file","args":{"path":"note.txt","content":"hello","user_authorized":true,"approval_reason":"user asked to continue implementation"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "ok" {
		t.Fatalf("expected ok, got %#v", res.Observations)
	}
	if _, err := os.Stat(filepath.Join(home, "sandbox", "note.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestFullAccessExecutesMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Mode:       ModeWork,
		AccessMode: AccessFullAccess,
		Message: `<harness_tool_call>
{"tool":"workspace.write_file","args":{"path":"note.txt","content":"hello"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "ok" {
		t.Fatalf("expected ok, got %#v", res.Observations)
	}
}
