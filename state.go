package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type State struct {
	History []string `json:"history"`
}

func stateFile() string {
	if d := os.Getenv("HERDR_PLUGIN_STATE_DIR"); d != "" {
		return filepath.Join(d, "state.json")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "herdr-sesh-state.json")
	}
	return filepath.Join(base, "herdr-sesh", "state.json")
}
func loadState() State {
	data, err := os.ReadFile(stateFile())
	if err != nil {
		return State{}
	}
	var s State
	_ = json.Unmarshal(data, &s)
	return s
}
func saveState(s State) {
	path := stateFile()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.MarshalIndent(s, "", "  ")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}
func recordTransition(from, to string) {
	s := loadState()
	for _, id := range []string{from, to} {
		if id == "" {
			continue
		}
		next := []string{id}
		for _, old := range s.History {
			if old != id {
				next = append(next, old)
			}
		}
		s.History = next
	}
	if len(s.History) > 20 {
		s.History = s.History[:20]
	}
	saveState(s)
}
func trackFocus() error {
	c, err := newHerdrClient()
	if err != nil {
		return err
	}
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	valid := map[string]bool{}
	for _, w := range snap.Workspaces {
		valid[w.ID] = true
	}
	s := loadState()
	next := []string{}
	if snap.FocusedWorkspaceID != "" {
		next = append(next, snap.FocusedWorkspaceID)
	}
	for _, id := range s.History {
		if valid[id] && id != snap.FocusedWorkspaceID {
			next = append(next, id)
		}
	}
	s.History = next
	saveState(s)
	return nil
}
