package harness

import "path/filepath"

type Mode string

const (
	ModeInspect Mode = "inspect"
	ModeWork    Mode = "work"
)

type Project struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	Description     string   `json:"description,omitempty"`
	DefaultMode     Mode     `json:"default_mode"`
	AllowedToolsets []string `json:"allowed_toolsets,omitempty"`
}

type Workspace struct {
	Root        string   `json:"root"`
	Project     *Project `json:"project,omitempty"`
	Mode        Mode     `json:"mode"`
	SandboxPath string   `json:"sandbox_path"`
}

func (w Workspace) DisplayName() string {
	if w.Project != nil {
		return w.Project.Name
	}
	return "Default sandbox"
}

func (w Workspace) AbsRoot() string {
	abs, err := filepath.Abs(w.Root)
	if err != nil {
		return w.Root
	}
	return abs
}

type HarnessCall struct {
	Index int            `json:"index"`
	Tool  string         `json:"tool"`
	Args  map[string]any `json:"args"`
	Raw   string         `json:"raw"`
}

type Observation struct {
	CallID string `json:"call_id"`
	Tool   string `json:"tool"`
	Status string `json:"status"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ReferencedFile struct {
	Ref      string `json:"ref"`
	Path     string `json:"path"`
	Complete bool   `json:"complete"`
	Content  string `json:"content,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Type     string `json:"type"`
	Error    string `json:"error,omitempty"`
}

type RunRequest struct {
	Message   string `json:"message"`
	Project   string `json:"project,omitempty"`
	Mode      Mode   `json:"mode,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type RunResponse struct {
	SessionID       string           `json:"session_id"`
	Status          string           `json:"status"`
	Mode            Mode             `json:"mode"`
	Project         *Project         `json:"project,omitempty"`
	WorkspaceRoot   string           `json:"workspace_root"`
	SystemPrompt    string           `json:"system_prompt"`
	ReferencedFiles []ReferencedFile `json:"referenced_files"`
	Observations    []Observation    `json:"observations"`
	Error           string           `json:"error,omitempty"`
}
