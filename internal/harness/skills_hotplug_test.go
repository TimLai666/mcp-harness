package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUseSkillReloadsChangedContent(t *testing.T) {
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
	res, err := rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "skill.use",
		SessionID: sessionID,
		Args:      map[string]any{"name": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" || len(res.ActiveSkills) != 1 {
		t.Fatalf("expected active skill, got %#v", res)
	}
	if !resultContains(t, res.Result, "version one") {
		t.Fatalf("expected first skill content, got %#v", res.Result)
	}

	writeSkill("version two")
	res, err = rt.ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "skill.use",
		SessionID: sessionID,
		Args:      map[string]any{"name": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resultContains(t, res.Result, "version two") {
		t.Fatal("expected changed skill content to be loaded immediately")
	}
}

func resultContains(t *testing.T, value any, needle string) bool {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		return false
	}
	content, _ := result["content"].(string)
	return content != "" && content == needle
}
