package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func LoadSessionState(sessionID string) SessionState {
	state := SessionState{ID: sessionID}
	path, err := sessionStatePath(sessionID)
	if err != nil {
		return state
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state
	}
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return SessionState{ID: sessionID}
	}
	if state.ID == "" {
		state.ID = sessionID
	}
	return state
}

func SaveSessionState(state SessionState) error {
	path, err := sessionStatePath(state.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func AddActiveSkill(state *SessionState, name string) {
	key := NormalizeSkillName(name)
	for _, existing := range state.ActiveSkills {
		if NormalizeSkillName(existing) == key {
			return
		}
	}
	state.ActiveSkills = append(state.ActiveSkills, name)
}

func sessionStatePath(sessionID string) (string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".state.json"), nil
}
