package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorkspaceGitInfoReportsBranchAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "init")

	clean := WorkspaceGitInfo(root)
	if !clean.IsRepo || clean.Branch == "" {
		t.Fatalf("expected repo with branch, got %#v", clean)
	}
	if clean.Added != 0 || clean.Removed != 0 {
		t.Fatalf("expected clean tree, got %#v", clean)
	}

	if err := os.WriteFile(file, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty := WorkspaceGitInfo(root)
	if dirty.Added < 1 || dirty.Removed < 1 || dirty.Files != 1 {
		t.Fatalf("expected one changed file with +/- counts, got %#v", dirty)
	}
}

func TestWorkspaceGitInfoNonRepo(t *testing.T) {
	info := WorkspaceGitInfo(t.TempDir())
	if info.IsRepo {
		t.Fatalf("expected non-repo, got %#v", info)
	}
}
