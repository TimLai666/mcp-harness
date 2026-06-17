package harness

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTerminalRunRecordsDiffAndRestoreVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	sandbox := filepath.Join(home, "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(sandbox, "note.txt")
	if err := os.WriteFile(notePath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	command := "printf changed > note.txt"
	if runtime.GOOS == "windows" {
		command = "Set-Content -LiteralPath note.txt -Value changed -NoNewline"
	}
	rt := NewRuntime()
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "terminal.run",
		Args: map[string]any{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.HistoryEvent == nil {
		t.Fatalf("expected one history event, got %#v", res)
	}
	if !strings.Contains(res.HistoryEvent.Diff, "-original") || !strings.Contains(res.HistoryEvent.Diff, "+changed") {
		t.Fatalf("expected terminal diff, got %s", res.HistoryEvent.Diff)
	}

	restore, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "history.restore",
		Args: map[string]any{"version_id": res.HistoryEvent.BeforeVersion},
	})
	if err != nil {
		t.Fatal(err)
	}
	if restore.Status != "ok" {
		t.Fatalf("expected restore ok, got %#v", restore)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("expected restored file, got %q", string(data))
	}
}
