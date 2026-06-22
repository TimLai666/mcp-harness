package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type Runtime struct {
	projects ProjectRegistry
	skills   *SkillRegistry
	sessions *sessionRegistry
}

func NewRuntime() *Runtime {
	return &Runtime{skills: NewSkillRegistry(), sessions: newSessionRegistry()}
}

// IssueSession mints a server-issued session id. The harness tool calls this so
// that every other tool can require a valid id, forcing the agent through the
// protocol guide before it acts.
func (r *Runtime) IssueSession() string { return r.sessions.issue() }

// ValidSession reports whether id was issued by this server and has not expired.
func (r *Runtime) ValidSession(id string) bool { return r.sessions.valid(id) }

const sessionTTL = 24 * time.Hour

type sessionRegistry struct {
	mu  sync.Mutex
	ids map[string]time.Time
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{ids: map[string]time.Time{}}
}

func (s *sessionRegistry) issue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, issued := range s.ids {
		if now.Sub(issued) > sessionTTL {
			delete(s.ids, id)
		}
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	id := "sess-" + hex.EncodeToString(buf)
	s.ids[id] = now
	return id
}

func (s *sessionRegistry) valid(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issued, ok := s.ids[id]
	if !ok {
		return false
	}
	if time.Since(issued) > sessionTTL {
		delete(s.ids, id)
		return false
	}
	return true
}

// Skills exposes the runtime skill registry for read-only tools.
func (r *Runtime) Skills() *SkillRegistry {
	return r.skills
}

// Guide returns the harness protocol prompt and lightweight orientation. It runs
// no local work; agents call it once before using any other harness tool.
func (r *Runtime) Guide(owner, project string) GuideResult {
	owner = NormalizeOwner(owner)
	projects := ProjectRegistry{Owner: owner}
	result := GuideResult{Owner: owner, Instructions: ComposeGuide(), SessionID: r.IssueSession()}
	if list, err := projects.List(); err == nil {
		result.Projects = list
	}
	result.Skills = r.skills.List()
	result.AccessMode = CurrentAccessModeFor(owner)
	if workspace, err := projects.Resolve(project, ""); err == nil {
		result.CurrentProject = workspace.Project
		result.WorkspaceRoot = workspace.Root
		result.Mode = workspace.Mode
		result.SandboxPath = workspace.SandboxPath
		result.ProjectInstructions = LoadProjectInstructions(workspace, 60000)
	}
	return result
}

// ExecuteTool runs one direct tool call. It resolves the workspace, applies the
// operator's access policy, records history and a step-level diff, and returns a
// structured result. There is no batching DSL: one call, one tool.
func (r *Runtime) ExecuteTool(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "session-" + time.Now().Format("20060102T150405.000000000")
	}
	args := req.Args
	if args == nil {
		args = map[string]any{}
	}
	owner := NormalizeOwner(req.Owner)
	workspace, err := ProjectRegistry{Owner: owner}.Resolve(req.Project, "")
	if err != nil {
		return ToolCallResult{}, err
	}
	policy := CurrentAccessModeFor(owner)
	sessionState := LoadSessionStateFor(owner, sessionID)
	registry := NewToolsetRegistry(workspace, r.skills, sessionID, policy)
	call := HarnessCall{Index: 0, Tool: req.Tool, Args: args}

	mutates := toolMutates(req.Tool)
	step := r.nextStep(owner, sessionID)
	callID := fmt.Sprintf("call-%s-%d-%d", sessionID, step, time.Now().UnixNano())
	ctx = WithCallID(ctx, callID)
	publishToolStart(callID, workspace, sessionID, req.Tool, getString(args, "command", ""))

	var beforeSnapshot WorkspaceSnapshot
	if mutates {
		beforeSnapshot = captureSnapshotOrTruncated(workspace.Root)
	}

	observation := registry.Execute(ctx, call)
	publishToolEnd(callID, workspace, sessionID, req.Tool, observation.Status, observation.Error)
	if observation.Status == "approval_required" {
		if resultMap, ok := observation.Result.(map[string]any); ok {
			if record, ok := resultMap["approval"].(ApprovalRecord); ok {
				publishApproval(record)
			}
		}
	}

	if req.Tool == "skill.use" && observation.Status == "ok" {
		if name, ok := args["name"].(string); ok {
			AddActiveSkill(&sessionState, name)
			_ = SaveSessionStateFor(owner, sessionState)
		}
	}

	var historyEvent *HistoryEvent
	if mutates {
		afterSnapshot := captureSnapshotOrTruncated(workspace.Root)
		diff, diffTruncated := DiffSnapshots(beforeSnapshot, afterSnapshot)
		beforeVersion, beforeErr := SaveWorkspaceVersion(sessionID, workspace, step, call.Tool, "before", beforeSnapshot)
		afterVersion, afterErr := SaveWorkspaceVersion(sessionID, workspace, step, call.Tool, "after", afterSnapshot)
		if beforeErr == nil && afterErr == nil {
			event := NewHistoryEvent(sessionID, workspace, step, call, observation, beforeVersion, afterVersion, diff, diffTruncated)
			_ = AppendHistoryEvent(event)
			historyEvent = &event
		}
	} else {
		event := NewHistoryEvent(sessionID, workspace, step, call, observation, WorkspaceVersion{}, WorkspaceVersion{}, "", false)
		_ = AppendHistoryEvent(event)
		historyEvent = &event
	}
	if historyEvent != nil {
		publishHistory(*historyEvent)
	}

	result := ToolCallResult{
		SessionID:     sessionID,
		Tool:          req.Tool,
		Status:        observation.Status,
		Result:        observation.Result,
		Error:         observation.Error,
		Project:       workspace.Project,
		WorkspaceRoot: workspace.Root,
		Mode:          workspace.Mode,
		AccessMode:    policy,
		ActiveSkills:  sessionState.ActiveSkills,
		HistoryEvent:  historyEvent,
	}
	_ = recordToolTurn(owner, sessionID, req, workspace, policy, observation, historyEvent)
	return result, nil
}

// toolMutates reports whether a tool can change workspace files, so the runtime
// only pays for before/after snapshots and version blobs when they matter.
func toolMutates(tool string) bool {
	switch tool {
	case "workspace.write_file", "workspace.apply_patch", "workspace.replace_lines", "terminal.run", "history.restore",
		"git.checkout", "git.pull", "git.merge", "git.reset", "git.stash":
		return true
	}
	return false
}

func captureSnapshotOrTruncated(root string) WorkspaceSnapshot {
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return WorkspaceSnapshot{Files: map[string]SnapshotFile{}, Truncated: true, OmittedPaths: []string{err.Error()}}
	}
	return snapshot
}

func (r *Runtime) nextStep(owner, sessionID string) int {
	events, err := ListHistoryEventsFor(owner, "", sessionID, 1000, false)
	if err != nil {
		return 1
	}
	return len(events) + 1
}

func recordToolTurn(owner, sessionID string, req ToolCallRequest, workspace Workspace, policy AccessMode, observation Observation, event *HistoryEvent) error {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	runReq := RunRequest{
		Message:    req.Tool,
		Project:    req.Project,
		Mode:       workspace.Mode,
		AccessMode: policy,
		SessionID:  sessionID,
	}
	runRes := RunResponse{
		SessionID:     sessionID,
		Status:        observation.Status,
		Mode:          workspace.Mode,
		AccessMode:    policy,
		Project:       workspace.Project,
		WorkspaceRoot: workspace.Root,
		Observations:  []Observation{observation},
	}
	if event != nil {
		runRes.HistoryEvents = []HistoryEvent{*event}
	}
	return store.RecordTurn(sessionID, runReq, runRes)
}
