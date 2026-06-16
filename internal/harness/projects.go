package harness

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var projectIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type ProjectRegistry struct{}

func (ProjectRegistry) List() ([]Project, error) {
	return LoadProjects()
}

func (r ProjectRegistry) Add(path, name, projectID, description string, mode Mode) (Project, error) {
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
		ID:          projectID,
		Name:        name,
		Path:        abs,
		Description: description,
		DefaultMode: mode,
	}
	projects = append(projects, project)
	return project, SaveProjects(projects)
}

func (r ProjectRegistry) Resolve(project string, mode Mode) (Workspace, error) {
	sandbox, err := SandboxDir()
	if err != nil {
		return Workspace{}, err
	}
	if project == "" {
		if mode == "" {
			mode = ModeWork
		}
		return Workspace{Root: sandbox, Mode: mode, SandboxPath: sandbox}, nil
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
			return Workspace{Root: candidate.Path, Project: &candidate, Mode: mode, SandboxPath: sandbox}, nil
		}
	}
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
			return Workspace{Root: abs, Project: &transient, Mode: mode, SandboxPath: sandbox}, nil
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
