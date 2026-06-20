package harness

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GitInfo is a structured summary of a workspace's git state for the Web UI:
// current branch and how many lines/files changed since the last commit.
type GitInfo struct {
	IsRepo  bool   `json:"is_repo"`
	Branch  string `json:"branch,omitempty"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Files   int    `json:"files_changed"`
	Ahead   int    `json:"ahead,omitempty"`
	Behind  int    `json:"behind,omitempty"`
	Remote  string `json:"remote,omitempty"`
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
