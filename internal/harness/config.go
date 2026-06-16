package harness

import (
	"encoding/json"
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
	path, err := ProjectsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload projectsFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	for i := range payload.Projects {
		abs, err := filepath.Abs(payload.Projects[i].Path)
		if err == nil {
			payload.Projects[i].Path = abs
		}
		if payload.Projects[i].DefaultMode == "" {
			payload.Projects[i].DefaultMode = ModeInspect
		}
	}
	return payload.Projects, nil
}

func SaveProjects(projects []Project) error {
	path, err := ProjectsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(projectsFile{Projects: projects}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
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
