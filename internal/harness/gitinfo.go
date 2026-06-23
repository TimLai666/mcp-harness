package harness

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GitInfo is a structured summary of a workspace's git state for the Web UI:
// current branch and how many lines/files changed since the last commit.
type GitInfo struct {
	IsRepo   bool   `json:"is_repo"`
	Branch   string `json:"branch,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
	Files    int    `json:"files_changed"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

type GitBranch struct {
	Name    string `json:"name"`
	Current bool   `json:"current,omitempty"`
	Remote  bool   `json:"remote,omitempty"`
}

type GitStatusEntry struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"original_path,omitempty"`
	IndexStatus    string `json:"index_status,omitempty"`
	WorktreeStatus string `json:"worktree_status,omitempty"`
	Staged         bool   `json:"staged,omitempty"`
	Unstaged       bool   `json:"unstaged,omitempty"`
	Untracked      bool   `json:"untracked,omitempty"`
}

// WorkspaceGitInfo inspects a workspace root and returns its branch and a diff
// summary (lines added/removed vs HEAD). A non-repository returns IsRepo false.
func WorkspaceGitInfo(root string) GitInfo {
	info := GitInfo{}
	branch, ok := runGitInfo(root, "rev-parse", "--abbrev-ref", "HEAD")
	if !ok {
		return info
	}
	info.IsRepo = true
	info.Branch = strings.TrimSpace(branch)
	if out, ok := runGitInfo(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); ok {
		info.Upstream = strings.TrimSpace(out)
	}

	stat, ok := runGitInfo(root, "diff", "--numstat", "HEAD")
	if !ok {
		stat, _ = runGitInfo(root, "diff", "--numstat")
	}
	info.Added, info.Removed, info.Files = parseNumstat(stat)

	if out, ok := runGitInfo(root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); ok {
		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) == 2 {
			info.Behind, _ = strconv.Atoi(fields[0])
			info.Ahead, _ = strconv.Atoi(fields[1])
		}
	}
	if out, ok := runGitInfo(root, "remote", "get-url", "origin"); ok {
		info.Remote = strings.TrimSpace(out)
	}
	return info
}

func WorkspaceGitBranches(root string) []GitBranch {
	out, ok := runGitInfo(root, "branch", "-a", "--no-color")
	if !ok {
		return nil
	}
	var branches []GitBranch
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		current := strings.HasPrefix(line, "* ")
		line = strings.TrimSpace(strings.TrimPrefix(line, "* "))
		if strings.Contains(line, "->") {
			continue
		}
		remote := strings.HasPrefix(line, "remotes/")
		name := line
		if remote {
			name = strings.TrimPrefix(line, "remotes/")
		}
		branches = append(branches, GitBranch{Name: name, Current: current, Remote: remote})
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Current != branches[j].Current {
			return branches[i].Current
		}
		if branches[i].Remote != branches[j].Remote {
			return !branches[i].Remote
		}
		return branches[i].Name < branches[j].Name
	})
	return branches
}

func WorkspaceGitStatusEntries(root string) []GitStatusEntry {
	out, ok := runGitInfo(root, "status", "--porcelain=v1")
	if !ok {
		return nil
	}
	var entries []GitStatusEntry
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		indexStatus := string(line[0])
		worktreeStatus := string(line[1])
		payload := strings.TrimSpace(line[3:])
		entry := GitStatusEntry{
			IndexStatus:    indexStatus,
			WorktreeStatus: worktreeStatus,
			Staged:         indexStatus != " " && indexStatus != "?",
			Unstaged:       worktreeStatus != " ",
			Untracked:      indexStatus == "?" && worktreeStatus == "?",
		}
		if strings.Contains(payload, " -> ") {
			parts := strings.SplitN(payload, " -> ", 2)
			entry.OriginalPath = parts[0]
			entry.Path = parts[1]
		} else {
			entry.Path = payload
		}
		if entry.Path != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseNumstat(out string) (added, removed, files int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		files++
		if value, err := strconv.Atoi(parts[0]); err == nil {
			added += value
		}
		if value, err := strconv.Atoi(parts[1]); err == nil {
			removed += value
		}
	}
	return added, removed, files
}

func runGitInfo(root string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	full := append([]string{"-C", root}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
