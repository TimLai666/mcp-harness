package harness

import "path/filepath"

type Mode string

const (
	ModeInspect Mode = "inspect"
	ModeWork    Mode = "work"
)

// AccessMode is the server-side permission policy. It is set by the operator
// through the Web UI, never by the calling agent.
type AccessMode string

const (
	// AccessDefault routes high-risk operations into the Web UI approval queue.
	AccessDefault AccessMode = "default"
	// AccessFullAccess lets high-risk operations execute directly. The operator
	// opts into this from the Web UI when they are supervising the session.
	AccessFullAccess AccessMode = "full_access"
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

// ToolCallRequest is one direct tool invocation from an external agent. Each
// MCP tool maps to a single ToolCallRequest; there is no batching DSL.
type ToolCallRequest struct {
	Tool      string         `json:"tool"`
	Project   string         `json:"project,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
}

// GuideResult is what the prompt-only `harness` tool returns: the protocol
// instructions plus lightweight orientation so the agent can discover context.
type GuideResult struct {
	Instructions        string               `json:"instructions"`
	AccessMode          AccessMode           `json:"access_mode"`
	CurrentProject      *Project             `json:"current_project,omitempty"`
	WorkspaceRoot       string               `json:"workspace_root,omitempty"`
	Mode                Mode                 `json:"mode,omitempty"`
	SandboxPath         string               `json:"sandbox_path,omitempty"`
	Projects            []Project            `json:"projects"`
	Skills              []SkillSpec          `json:"skills"`
	ProjectInstructions []ProjectInstruction `json:"project_instructions,omitempty"`
}

// ToolCallResult is the structured outcome of a single direct tool invocation.
type ToolCallResult struct {
	SessionID     string        `json:"session_id"`
	Tool          string        `json:"tool"`
	Status        string        `json:"status"`
	Result        any           `json:"result,omitempty"`
	Error         string        `json:"error,omitempty"`
	Project       *Project      `json:"project,omitempty"`
	WorkspaceRoot string        `json:"workspace_root"`
	Mode          Mode          `json:"mode"`
	AccessMode    AccessMode    `json:"access_mode"`
	ActiveSkills  []string      `json:"active_skills,omitempty"`
	HistoryEvent  *HistoryEvent `json:"history_event,omitempty"`
}

type Observation struct {
	CallID string `json:"call_id"`
	Tool   string `json:"tool"`
	Status string `json:"status"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

type ApprovalRecord struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Project   string         `json:"project,omitempty"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	Reason    string         `json:"reason"`
	Status    ApprovalStatus `json:"status"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type MCPServerConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Endpoint  string            `json:"endpoint,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Trusted   bool              `json:"trusted,omitempty"`
}

type SessionState struct {
	ID           string   `json:"id"`
	ActiveSkills []string `json:"active_skills"`
}

type SessionRecord struct {
	ID            string     `json:"id"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
	ProjectID     string     `json:"project_id,omitempty"`
	ProjectName   string     `json:"project_name,omitempty"`
	WorkspaceRoot string     `json:"workspace_root"`
	Mode          Mode       `json:"mode"`
	AccessMode    AccessMode `json:"access_mode"`
	ActiveSkills  []string   `json:"active_skills,omitempty"`
	TurnCount     int        `json:"turn_count,omitempty"`
}

type TurnRecord struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	Timestamp string      `json:"timestamp"`
	Status    string      `json:"status"`
	Request   RunRequest  `json:"request"`
	Response  RunResponse `json:"response"`
}

type ToolCallRecord struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	TurnID    string         `json:"turn_id"`
	Index     int            `json:"index"`
	Tool      string         `json:"tool"`
	Status    string         `json:"status"`
	Args      map[string]any `json:"args,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type SnapshotFile struct {
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Content string `json:"content,omitempty"`
}

type WorkspaceSnapshot struct {
	Files        map[string]SnapshotFile `json:"files"`
	Truncated    bool                    `json:"truncated,omitempty"`
	OmittedPaths []string                `json:"omitted_paths,omitempty"`
}

type WorkspaceVersion struct {
	ID            string            `json:"id"`
	Timestamp     string            `json:"timestamp"`
	SessionID     string            `json:"session_id"`
	ProjectID     string            `json:"project_id,omitempty"`
	ProjectName   string            `json:"project_name,omitempty"`
	WorkspaceRoot string            `json:"workspace_root"`
	Mode          Mode              `json:"mode"`
	Step          int               `json:"step"`
	Tool          string            `json:"tool"`
	Label         string            `json:"label"`
	Snapshot      WorkspaceSnapshot `json:"snapshot"`
}

type HistoryEvent struct {
	ID             string         `json:"id"`
	Timestamp      string         `json:"timestamp"`
	SessionID      string         `json:"session_id"`
	ProjectID      string         `json:"project_id,omitempty"`
	ProjectName    string         `json:"project_name,omitempty"`
	WorkspaceRoot  string         `json:"workspace_root"`
	Mode           Mode           `json:"mode"`
	Step           int            `json:"step"`
	Tool           string         `json:"tool"`
	Status         string         `json:"status"`
	Args           map[string]any `json:"args,omitempty"`
	Error          string         `json:"error,omitempty"`
	BeforeVersion  string         `json:"before_version"`
	AfterVersion   string         `json:"after_version"`
	Diff           string         `json:"diff,omitempty"`
	DiffTruncated  bool           `json:"diff_truncated,omitempty"`
	SnapshotNotice string         `json:"snapshot_notice,omitempty"`
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

type ProjectInstruction struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	Complete  bool   `json:"complete"`
	Truncated bool   `json:"truncated,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Error     string `json:"error,omitempty"`
}

type RunRequest struct {
	Message    string     `json:"message"`
	Project    string     `json:"project,omitempty"`
	Mode       Mode       `json:"mode,omitempty"`
	AccessMode AccessMode `json:"access_mode,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
}

type RunResponse struct {
	SessionID       string               `json:"session_id"`
	Status          string               `json:"status"`
	Mode            Mode                 `json:"mode"`
	AccessMode      AccessMode           `json:"access_mode"`
	Project         *Project             `json:"project,omitempty"`
	WorkspaceRoot   string               `json:"workspace_root"`
	SystemPrompt    string               `json:"system_prompt"`
	ActiveSkills    []string             `json:"active_skills,omitempty"`
	ReferencedFiles []ReferencedFile     `json:"referenced_files"`
	Instructions    []ProjectInstruction `json:"project_instructions,omitempty"`
	Observations    []Observation        `json:"observations"`
	HistoryEvents   []HistoryEvent       `json:"history_events,omitempty"`
	Error           string               `json:"error,omitempty"`
}
