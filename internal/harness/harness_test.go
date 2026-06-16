package harness

import (
	"context"
	"os"
	"path/filepath"
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
