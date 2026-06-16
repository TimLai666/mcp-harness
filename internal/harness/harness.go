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
	workspace, err := r.projects.Resolve(req.Project, req.Mode)
	if err != nil {
		return RunResponse{}, err
	}
	references := ResolveReferences(req.Message, workspace, 40000)
	registry := NewToolsetRegistry(workspace, r.skills)
	calls, parseErr := ParseToolCalls(req.Message)
	var observations []Observation
	if parseErr == nil {
		for _, call := range calls {
			observations = append(observations, registry.Execute(ctx, call))
		}
	}
	prompt := ComposeSystemPrompt(workspace, registry.Catalog(), r.skills, references, observations)
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
		Project:         workspace.Project,
		WorkspaceRoot:   workspace.Root,
		SystemPrompt:    prompt,
		ReferencedFiles: references,
		Observations:    observations,
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
