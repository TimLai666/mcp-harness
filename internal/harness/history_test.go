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
	res, err := rt.Run(context.Background(), RunRequest{
		Mode:       ModeWork,
		AccessMode: AccessFullAccess,
		Message: `<harness_tool_call>
{"tool":"terminal.run","args":{"command":"` + command + `"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.HistoryEvents) != 1 {
		t.Fatalf("expected one history event, got %#v", res.HistoryEvents)
	}
	if !strings.Contains(res.HistoryEvents[0].Diff, "-original") || !strings.Contains(res.HistoryEvents[0].Diff, "+changed") {
		t.Fatalf("expected terminal diff, got %s", res.HistoryEvents[0].Diff)
	}

	restore, err := rt.Run(context.Background(), RunRequest{
		Mode:       ModeWork,
		AccessMode: AccessFullAccess,
		Message: `<harness_tool_call>
{"tool":"history.restore","args":{"version_id":"` + res.HistoryEvents[0].BeforeVersion + `"}}
</harness_tool_call>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(restore.Observations) != 1 || restore.Observations[0].Status != "ok" {
		t.Fatalf("expected restore ok, got %#v", restore.Observations)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("expected restored file, got %q", string(data))
	}
}
