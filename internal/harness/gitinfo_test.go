package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestWorkspaceGitBranchesListsCurrentAndRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, remote, "init", "--bare")
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "init")
	runGit(t, root, "remote", "add", "origin", remote)
	branchOut := strings.TrimSpace(runGitOutput(t, root, "branch", "--show-current"))
	runGit(t, root, "push", "-u", "origin", branchOut)
	runGit(t, root, "checkout", "-b", "feature/ui")

	branches := WorkspaceGitBranches(root)
	if len(branches) == 0 {
		t.Fatal("expected branches")
	}
	var sawCurrent, sawRemote bool
	for _, branch := range branches {
		if branch.Name == "feature/ui" && branch.Current {
			sawCurrent = true
		}
		if strings.Contains(branch.Name, "origin/") {
			sawRemote = true
		}
	}
	if !sawCurrent || !sawRemote {
		t.Fatalf("expected current and remote branches, got %#v", branches)
	}
}

func TestWorkspaceGitStatusEntriesReportsTrackedAndUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "init")
	if err := os.WriteFile(tracked, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := WorkspaceGitStatusEntries(root)
	if len(entries) < 2 {
		t.Fatalf("expected tracked + untracked entries, got %#v", entries)
	}
	var sawTracked, sawUntracked bool
	for _, entry := range entries {
		if entry.Path == "tracked.txt" && entry.Unstaged {
			sawTracked = true
		}
		if entry.Path == "new.txt" && entry.Untracked {
			sawUntracked = true
		}
	}
	if !sawTracked || !sawUntracked {
		t.Fatalf("expected tracked + untracked flags, got %#v", entries)
	}
}

func runGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
