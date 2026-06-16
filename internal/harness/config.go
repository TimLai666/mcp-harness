package harness

import (
	"errors"
	"os"
	"path/filepath"
)

const appDirEnv = "MCP_HARNESS_HOME"

type projectsFile struct {
	Projects []Project `json:"projects"`
}

func AppDir() (string, error) {
	if raw := os.Getenv(appDirEnv); raw != "" {
		return ensureDir(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, ".mcp-harness"))
}

func SandboxDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "sandbox"))
}

func SessionsDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "sessions"))
}

func ProjectsPath() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "projects.json"), nil
}

func DBPath() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "harness.db"), nil
}

func MCPsPath() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mcps.json"), nil
}

func ApprovalsDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "approvals"))
}

func HistoryDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "history"))
}

func HistoryVersionsDir() (string, error) {
	base, err := HistoryDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "versions"))
}

func RepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "prompts", "main.md")); err == nil {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", errors.New("could not locate repository root")
		}
		wd = next
	}
}

func LoadProjects() ([]Project, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	projects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	for i := range projects {
		abs, err := filepath.Abs(projects[i].Path)
		if err == nil {
			projects[i].Path = abs
		}
		if projects[i].DefaultMode == "" {
			projects[i].DefaultMode = ModeInspect
		}
	}
	return projects, nil
}

func SaveProjects(projects []Project) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.SaveProjects(projects)
}

func ReadPrompt(name string) string {
	root, err := RepoRoot()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "prompts", name))
	if err != nil {
		return ""
	}
	return string(data)
}

func ensureDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", err
	}
	return abs, nil
}
