package harness

func LoadSessionState(sessionID string) SessionState {
	store, err := DefaultStore()
	if err != nil {
		return SessionState{ID: sessionID}
	}
	return store.LoadSessionState(sessionID)
}

func SaveSessionState(state SessionState) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.SaveSessionState(state)
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
