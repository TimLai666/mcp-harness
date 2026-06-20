package harness

func LoadSessionState(sessionID string) SessionState {
	return LoadSessionStateFor(DefaultOwner, sessionID)
}

func LoadSessionStateFor(owner, sessionID string) SessionState {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return SessionState{ID: sessionID}
	}
	return store.LoadSessionState(sessionID)
}

func SaveSessionState(state SessionState) error { return SaveSessionStateFor(DefaultOwner, state) }

func SaveSessionStateFor(owner string, state SessionState) error {
	store, err := DefaultStoreFor(owner)
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
