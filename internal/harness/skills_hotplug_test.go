package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveSkillReloadsChangedContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	skillDir := filepath.Join(home, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(body string) {
		t.Helper()
		content := "---\nname: demo\ndescription: demo skill\n---\n\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("version one")
	rt := NewRuntime()
	sessionID := "skill-hotplug"
	res, err := rt.Run(context.Background(), RunRequest{
		SessionID: sessionID,
		Message: `<harness_tool_call>
{"tool":"skill.use","args":{"name":"demo"}}
</harness_tool_call>`,
	})
	if err != nil || len(res.ActiveSkills) != 1 {
		t.Fatalf("expected active skill, res=%#v err=%v", res, err)
	}
	writeSkill("version two")
	res, err = rt.Run(context.Background(), RunRequest{SessionID: sessionID, Message: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.SystemPrompt, "version two") {
		t.Fatal("expected changed skill content to be injected immediately")
	}
}
