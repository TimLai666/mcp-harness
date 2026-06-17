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
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.write_file",
		Args: map[string]any{"path": "note.txt", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "approval_required" {
		t.Fatalf("expected approval_required, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "sandbox", "note.txt")); err == nil {
		t.Fatal("file should not be written before approval")
	}
}

func TestApprovedMutationExecutesWithApprovalID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	sessionID := "approve-flow"
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "workspace.write_file",
		SessionID: sessionID,
		Args:      map[string]any{"path": "note.txt", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "approval_required" {
		t.Fatalf("expected approval_required, got %#v", res)
	}
	result, ok := res.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected approval result map, got %#v", res.Result)
	}
	record, ok := result["approval"].(ApprovalRecord)
	if !ok {
		t.Fatalf("expected approval record, got %#v", result["approval"])
	}
	if _, err := (ApprovalStore{}).SetStatus(record.ID, ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "workspace.write_file",
		SessionID: sessionID,
		Args:      map[string]any{"path": "note.txt", "content": "hello", "approval_id": record.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok after approval, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "sandbox", "note.txt")); err != nil {
		t.Fatalf("file should be written after approval: %v", err)
	}
}

func TestFullAccessPolicyExecutesMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "workspace.write_file",
		Args: map[string]any{"path": "note.txt", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok under full_access policy, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "sandbox", "note.txt")); err != nil {
		t.Fatalf("file should be written under full_access: %v", err)
	}
}
