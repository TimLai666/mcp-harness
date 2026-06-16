package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Runtime struct {
	projects ProjectRegistry
	skills   *SkillRegistry
}

func NewRuntime() *Runtime {
	return &Runtime{skills: NewSkillRegistry()}
}

func (r *Runtime) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "session-" + time.Now().Format("20060102T150405.000000000")
	}
	if req.AccessMode == "" {
		req.AccessMode = AccessDefault
	}
	sessionState := LoadSessionState(sessionID)
	workspace, err := r.projects.Resolve(req.Project, req.Mode)
	if err != nil {
		return RunResponse{}, err
	}
	references := ResolveReferences(req.Message, workspace, 40000)
	registry := NewToolsetRegistry(workspace, r.skills, sessionID, req.AccessMode)
	calls, parseErr := ParseToolCalls(req.Message)
	var observations []Observation
	var historyEvents []HistoryEvent
	currentSnapshot, snapshotErr := CaptureWorkspaceSnapshot(workspace.Root)
	if snapshotErr != nil {
		currentSnapshot = WorkspaceSnapshot{Files: map[string]SnapshotFile{}, Truncated: true, OmittedPaths: []string{snapshotErr.Error()}}
	}
	if parseErr == nil {
		for step, call := range calls {
			beforeSnapshot := currentSnapshot
			observation := registry.Execute(ctx, call)
			observations = append(observations, observation)
			if call.Tool == "skill.use" && observation.Status == "ok" {
				if name, ok := call.Args["name"].(string); ok {
					AddActiveSkill(&sessionState, name)
				}
			}
			afterSnapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
			if err != nil {
				afterSnapshot = WorkspaceSnapshot{Files: map[string]SnapshotFile{}, Truncated: true, OmittedPaths: []string{err.Error()}}
			}
			diff, diffTruncated := DiffSnapshots(beforeSnapshot, afterSnapshot)
			beforeVersion, beforeErr := SaveWorkspaceVersion(sessionID, workspace, step+1, call.Tool, "before", beforeSnapshot)
			afterVersion, afterErr := SaveWorkspaceVersion(sessionID, workspace, step+1, call.Tool, "after", afterSnapshot)
			if beforeErr == nil && afterErr == nil {
				event := NewHistoryEvent(sessionID, workspace, step+1, call, observation, beforeVersion, afterVersion, diff, diffTruncated)
				_ = AppendHistoryEvent(event)
				historyEvents = append(historyEvents, event)
			}
			currentSnapshot = afterSnapshot
		}
	}
	_ = SaveSessionState(sessionState)
	prompt := ComposeSystemPrompt(workspace, req.AccessMode, registry.Catalog(), r.skills, references, observations, sessionState.ActiveSkills)
	status := "ok"
	errText := ""
	if parseErr != nil {
		status = "error"
		errText = parseErr.Error()
	}
	response := RunResponse{
		SessionID:       sessionID,
		Status:          status,
		Mode:            workspace.Mode,
		AccessMode:      req.AccessMode,
		Project:         workspace.Project,
		WorkspaceRoot:   workspace.Root,
		SystemPrompt:    prompt,
		ActiveSkills:    sessionState.ActiveSkills,
		ReferencedFiles: references,
		Observations:    observations,
		HistoryEvents:   historyEvents,
		Error:           errText,
	}
	_ = recordSession(sessionID, req, response)
	return response, nil
}

func recordSession(sessionID string, req RunRequest, res RunResponse) error {
	dir, err := SessionsDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	compact := res
	compact.SystemPrompt = ""
	event := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"request":   req,
		"response":  compact,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}
