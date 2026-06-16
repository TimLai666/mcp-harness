package harness

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogIncludesToolSchemas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{Message: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.SystemPrompt, `"path"`) || !strings.Contains(res.SystemPrompt, `"required": true`) {
		t.Fatal("expected injected catalog to include argument schemas")
	}
}

func TestExecuteRejectsInvalidToolArgsBeforeHandler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Message: `<harness_tool_call>
{"tool":"workspace.read_file","args":{}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "error" {
		t.Fatalf("expected error observation, got %#v", res.Observations)
	}
	if !strings.Contains(res.Observations[0].Error, `missing required arg "path"`) {
		t.Fatalf("unexpected error: %s", res.Observations[0].Error)
	}
}

func TestExecuteRejectsUnknownToolArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Message: `<harness_tool_call>
{"tool":"git.status","args":{"surprise":true}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || !strings.Contains(res.Observations[0].Error, "unknown arg") {
		t.Fatalf("expected unknown arg error, got %#v", res.Observations)
	}
}

func TestAutoApprovalAuditArgsAreAllowedBySchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	rt := NewRuntime()
	res, err := rt.Run(context.Background(), RunRequest{
		Mode:       ModeWork,
		AccessMode: AccessAuto,
		Message: `<harness_tool_call>
{"tool":"workspace.write_file","args":{"path":"note.txt","content":"ok","user_authorized":true,"approval_reason":"user asked to continue"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 || res.Observations[0].Status != "ok" {
		t.Fatalf("expected ok, got %#v", res.Observations)
	}
}
