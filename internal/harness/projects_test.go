package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSkillRootsIncludesUserAgentsSkills(t *testing.T) {
	var found bool
	for _, root := range DefaultSkillRoots() {
		if strings.HasSuffix(filepath.ToSlash(root), "/.agents/skills") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DefaultSkillRoots to include user-home .agents/skills, got %#v", DefaultSkillRoots())
	}
}

func TestProjectCreateToolCreatesPersistentWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	t.Setenv(accessModeEnv, string(AccessFullAccess))
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "project.create",
		SessionID: "project-create",
		Args:      map[string]any{"name": "Demo Workspace", "project_id": "demo", "allowed_toolsets": []any{"workspace", "git"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok project.create result, got %#v", res)
	}
	result, ok := res.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %#v", res.Result)
	}
	project, ok := result["project"].(Project)
	if !ok {
		t.Fatalf("expected project result, got %#v", result["project"])
	}
	if project.ID != "demo" || project.DefaultMode != ModeWork {
		t.Fatalf("unexpected project: %#v", project)
	}
	expectedRoot := filepath.Join(home, "workspaces")
	if !strings.HasPrefix(filepath.Clean(project.Path), filepath.Clean(expectedRoot)+string(os.PathSeparator)) {
		t.Fatalf("expected project path under %s, got %s", expectedRoot, project.Path)
	}
	if _, err := os.Stat(project.Path); err != nil {
		t.Fatalf("expected created workspace directory: %v", err)
	}
	projects, err := (ProjectRegistry{}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != "demo" {
		t.Fatalf("expected persisted project, got %#v", projects)
	}
}

func TestProjectCreateRequiresApprovalByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "project.create",
		SessionID: "project-create-approval",
		Args:      map[string]any{"name": "Needs Approval", "project_id": "needs-approval"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "approval_required" {
		t.Fatalf("expected approval_required, got %#v", res)
	}
	projects, err := (ProjectRegistry{}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("project.create should not run before approval, got %#v", projects)
	}
	if _, err := os.Stat(filepath.Join(home, "workspaces", "needs-approval")); !os.IsNotExist(err) {
		t.Fatalf("workspace directory should not be created before approval, stat err=%v", err)
	}
}

func TestProjectCloneToolClonesAndRegistersWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)
	source := filepath.Join(home, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init")
	runGit(t, source, "config", "user.email", "test@example.com")
	runGit(t, source, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "init")

	t.Setenv(accessModeEnv, string(AccessFullAccess))
	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool:      "project.clone",
		SessionID: "project-clone",
		Args:      map[string]any{"repo_url": filepath.ToSlash(source), "project_id": "cloned", "timeout_ms": 60000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok project.clone result, got %#v", res)
	}
	result, ok := res.Result.(CloneResult)
	if !ok {
		t.Fatalf("expected CloneResult, got %#v", res.Result)
	}
	if result.Project.ID != "cloned" || result.Project.DefaultMode != ModeWork {
		t.Fatalf("unexpected clone result: %#v", result)
	}
	if strings.Contains(result.Command, filepath.ToSlash(source)) || strings.Contains(result.Command, source) {
		t.Fatalf("clone command should redact repo url, got %q", result.Command)
	}
	if _, err := os.Stat(filepath.Join(result.Project.Path, "README.md")); err != nil {
		t.Fatalf("expected cloned README: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}
