package harness

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var projectIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ProjectRegistry manages one tenant's projects (zero value = default owner).
type ProjectRegistry struct{ Owner string }

func (r ProjectRegistry) owner() string { return NormalizeOwner(r.Owner) }

type CloneResult struct {
	Project    Project `json:"project"`
	Command    string  `json:"command"`
	ReturnCode int     `json:"returncode"`
	Stdout     string  `json:"stdout,omitempty"`
	Stderr     string  `json:"stderr,omitempty"`
}

func (r ProjectRegistry) List() ([]Project, error) {
	return LoadProjectsFor(r.owner())
}

func (r ProjectRegistry) Add(path, name, projectID, description string, mode Mode) (Project, error) {
	return r.AddWithAllowedToolsets(path, name, projectID, description, mode, nil)
}

func (r ProjectRegistry) AddWithAllowedToolsets(path, name, projectID, description string, mode Mode, allowedToolsets []string) (Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Project{}, err
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("project path is not a directory: %s", abs)
	}
	projects, err := r.List()
	if err != nil {
		return Project{}, err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	if projectID == "" {
		projectID = makeProjectID(name, projects)
	}
	if !projectIDRE.MatchString(projectID) {
		return Project{}, fmt.Errorf("invalid project id: %s", projectID)
	}
	for _, existing := range projects {
		if existing.ID == projectID {
			return Project{}, fmt.Errorf("project id already exists: %s", projectID)
		}
	}
	if mode == "" {
		mode = ModeInspect
	}
	project := Project{
		ID:              projectID,
		Owner:           r.owner(),
		Name:            name,
		Path:            abs,
		Description:     description,
		DefaultMode:     mode,
		AllowedToolsets: normalizeAllowedToolsets(allowedToolsets),
	}
	projects = append(projects, project)
	return project, SaveProjectsFor(r.owner(), projects)
}

func (r ProjectRegistry) CreateWorkspace(name, projectID, description string, mode Mode, allowedToolsets []string) (Project, error) {
	if strings.TrimSpace(name) == "" && strings.TrimSpace(projectID) == "" {
		return Project{}, fmt.Errorf("workspace name or project_id is required")
	}
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSpace(projectID)
	}
	if mode == "" {
		mode = ModeWork
	}
	projects, err := r.List()
	if err != nil {
		return Project{}, err
	}
	if projectID == "" {
		projectID = makeProjectID(name, projects)
	}
	if err := validateNewProjectID(projectID, projects); err != nil {
		return Project{}, err
	}
	workspaces, err := WorkspacesDirFor(r.owner())
	if err != nil {
		return Project{}, err
	}
	path, err := nextWorkspacePath(workspaces, projectID, projects)
	if err != nil {
		return Project{}, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return Project{}, err
	}
	return r.AddWithAllowedToolsets(path, name, projectID, description, mode, allowedToolsets)
}

func (r ProjectRegistry) CloneWorkspace(ctx context.Context, repoURL, branch, name, projectID, description string, mode Mode, allowedToolsets []string, depth int, timeout time.Duration) (CloneResult, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return CloneResult{}, fmt.Errorf("repo_url is required")
	}
	if depth < 0 {
		return CloneResult{}, fmt.Errorf("depth must be >= 0")
	}
	if strings.TrimSpace(name) == "" {
		name = inferProjectNameFromRepoURL(repoURL)
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(projectID) == "" {
		return CloneResult{}, fmt.Errorf("project name or project_id is required")
	}
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSpace(projectID)
	}
	if mode == "" {
		mode = ModeWork
	}
	projects, err := r.List()
	if err != nil {
		return CloneResult{}, err
	}
	if projectID == "" {
		projectID = makeProjectID(name, projects)
	}
	if err := validateNewProjectID(projectID, projects); err != nil {
		return CloneResult{}, err
	}
	workspaces, err := WorkspacesDirFor(r.owner())
	if err != nil {
		return CloneResult{}, err
	}
	path, err := nextWorkspacePath(workspaces, projectID, projects)
	if err != nil {
		return CloneResult{}, err
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{"clone"}
	if branch = strings.TrimSpace(branch); branch != "" {
		args = append(args, "--branch", branch)
	}
	if depth > 0 {
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	args = append(args, repoURL, path)
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Env = AppendGitHubEnv(os.Environ(), r.owner())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	displayCommand := gitCloneDisplayCommand(args)
	if err := cmd.Run(); err != nil {
		code := 1
		var exitErr *exec.ExitError
		if ok := errorAs(err, &exitErr); ok {
			code = exitErr.ExitCode()
		}
		stderrText := tail(stderr.String(), 20000)
		return CloneResult{
			Command:    displayCommand,
			ReturnCode: code,
			Stdout:     tail(stdout.String(), 20000),
			Stderr:     stderrText,
		}, fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(stderrText))
	}
	project, err := r.AddWithAllowedToolsets(path, name, projectID, description, mode, allowedToolsets)
	if err != nil {
		return CloneResult{
			Command:    displayCommand,
			ReturnCode: 0,
			Stdout:     tail(stdout.String(), 20000),
			Stderr:     tail(stderr.String(), 20000),
		}, err
	}
	return CloneResult{
		Project:    project,
		Command:    displayCommand,
		ReturnCode: 0,
		Stdout:     tail(stdout.String(), 20000),
		Stderr:     tail(stderr.String(), 20000),
	}, nil
}

// Rename changes a project's display name (and optionally its description). The
// stable project id is preserved.
func (r ProjectRegistry) Rename(selector, newName, newDescription string) (Project, error) {
	if strings.TrimSpace(newName) == "" {
		return Project{}, fmt.Errorf("new project name is required")
	}
	projects, err := r.List()
	if err != nil {
		return Project{}, err
	}
	idx := indexOfProject(projects, selector)
	if idx < 0 {
		return Project{}, fmt.Errorf("unknown project: %s", selector)
	}
	projects[idx].Name = strings.TrimSpace(newName)
	if strings.TrimSpace(newDescription) != "" {
		projects[idx].Description = newDescription
	}
	if err := SaveProjectsFor(r.owner(), projects); err != nil {
		return Project{}, err
	}
	return projects[idx], nil
}

// Relocate repoints a project at a different existing directory. It only updates
// the registry; it does not move files.
func (r ProjectRegistry) Relocate(selector, newPath string) (Project, error) {
	abs, err := filepath.Abs(newPath)
	if err != nil {
		return Project{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Project{}, err
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("project path is not a directory: %s", abs)
	}
	projects, err := r.List()
	if err != nil {
		return Project{}, err
	}
	idx := indexOfProject(projects, selector)
	if idx < 0 {
		return Project{}, fmt.Errorf("unknown project: %s", selector)
	}
	projects[idx].Path = abs
	if err := SaveProjectsFor(r.owner(), projects); err != nil {
		return Project{}, err
	}
	return projects[idx], nil
}

// Remove unregisters a project. When deleteFiles is true it also deletes the
// workspace directory, but only for harness-managed workspaces under
// MCP_HARNESS_HOME/workspaces; it refuses to delete files anywhere else.
func (r ProjectRegistry) Remove(selector string, deleteFiles bool) (Project, bool, error) {
	projects, err := r.List()
	if err != nil {
		return Project{}, false, err
	}
	idx := indexOfProject(projects, selector)
	if idx < 0 {
		return Project{}, false, fmt.Errorf("unknown project: %s", selector)
	}
	removed := projects[idx]
	filesDeleted := false
	if deleteFiles {
		workspaces, err := WorkspacesDirFor(r.owner())
		if err != nil {
			return Project{}, false, err
		}
		if !isWithin(workspaces, removed.Path) {
			return Project{}, false, fmt.Errorf("refusing to delete files outside harness-managed workspaces: %s", removed.Path)
		}
		if err := os.RemoveAll(removed.Path); err != nil {
			return Project{}, false, err
		}
		filesDeleted = true
	}
	projects = append(projects[:idx], projects[idx+1:]...)
	if err := SaveProjectsFor(r.owner(), projects); err != nil {
		return Project{}, false, err
	}
	return removed, filesDeleted, nil
}

func indexOfProject(projects []Project, selector string) int {
	selector = strings.TrimSpace(selector)
	abs, absErr := filepath.Abs(selector)
	for i, project := range projects {
		if selector == project.ID || selector == project.Name || selector == project.Path {
			return i
		}
		if absErr == nil && filepath.Clean(abs) == filepath.Clean(project.Path) {
			return i
		}
	}
	return -1
}

func isWithin(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	return target == base || strings.HasPrefix(target, base+string(os.PathSeparator))
}

func normalizeAllowedToolsets(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func validateNewProjectID(projectID string, projects []Project) error {
	if !projectIDRE.MatchString(projectID) {
		return fmt.Errorf("invalid project id: %s", projectID)
	}
	for _, existing := range projects {
		if existing.ID == projectID {
			return fmt.Errorf("project id already exists: %s", projectID)
		}
	}
	return nil
}

func nextWorkspacePath(root, projectID string, projects []Project) (string, error) {
	used := map[string]bool{}
	for _, project := range projects {
		abs, err := filepath.Abs(project.Path)
		if err == nil {
			used[filepath.Clean(abs)] = true
		}
	}
	for i := 1; ; i++ {
		name := projectID
		if i > 1 {
			name = fmt.Sprintf("%s-%d", projectID, i)
		}
		path := filepath.Join(root, name)
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if used[filepath.Clean(abs)] {
			continue
		}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return abs, nil
		} else if err != nil {
			return "", err
		}
	}
}

func inferProjectNameFromRepoURL(repoURL string) string {
	repoURL = strings.TrimSpace(strings.TrimSuffix(repoURL, "/"))
	if repoURL == "" {
		return ""
	}
	repoURL = strings.TrimSuffix(repoURL, ".git")
	if idx := strings.LastIndexAny(repoURL, "/:"); idx >= 0 && idx < len(repoURL)-1 {
		return repoURL[idx+1:]
	}
	return repoURL
}

func gitCloneDisplayCommand(args []string) string {
	display := append([]string(nil), args...)
	if len(display) >= 3 {
		display[len(display)-2] = "<repo_url>"
	}
	return "git " + strings.Join(display, " ")
}

func (r ProjectRegistry) Resolve(project string, mode Mode) (Workspace, error) {
	sandbox, err := SandboxDirFor(r.owner())
	if err != nil {
		return Workspace{}, err
	}
	owner := r.owner()
	if project == "" {
		if mode == "" {
			mode = ModeWork
		}
		return Workspace{Owner: owner, Root: sandbox, Mode: mode, SandboxPath: sandbox}, nil
	}
	projects, err := r.List()
	if err != nil {
		return Workspace{}, err
	}
	for _, candidate := range projects {
		if project == candidate.ID || project == candidate.Name || project == candidate.Path {
			if mode == "" {
				mode = candidate.DefaultMode
			}
			return Workspace{Owner: owner, Root: candidate.Path, Project: &candidate, Mode: mode, SandboxPath: sandbox}, nil
		}
	}
	// Resolving an arbitrary absolute host path as a transient project is a
	// convenience for the local single-user case only. Authenticated tenants
	// must use registered projects so they cannot reach paths outside their
	// own tenant.
	if owner == DefaultOwner {
		abs, err := filepath.Abs(project)
		if err == nil {
			if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
				if mode == "" {
					mode = ModeInspect
				}
				sum := sha1.Sum([]byte(abs))
				transient := Project{
					ID:          "transient-" + hex.EncodeToString(sum[:])[:12],
					Name:        filepath.Base(abs),
					Path:        abs,
					Description: "Transient project resolved from harness request.",
					DefaultMode: mode,
				}
				return Workspace{Owner: owner, Root: abs, Project: &transient, Mode: mode, SandboxPath: sandbox}, nil
			}
		}
	}
	return Workspace{}, fmt.Errorf("unknown project: %s", project)
}

func makeProjectID(name string, projects []Project) string {
	base := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "project"
	}
	used := map[string]bool{}
	for _, p := range projects {
		used[p.ID] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s-%d", base, i)
		if !used[next] {
			return next
		}
	}
}
