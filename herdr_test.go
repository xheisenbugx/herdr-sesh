package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"
)

func TestHerdrClientWorkspaceLifecycleProtocol(t *testing.T) {
	var mu sync.Mutex
	methods := []string{}
	client := &HerdrClient{dial: func() (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go func(conn net.Conn) {
			defer conn.Close()
			var req struct {
				Method string `json:"method"`
			}
			_ = json.NewDecoder(bufio.NewReader(conn)).Decode(&req)
			mu.Lock()
			methods = append(methods, req.Method)
			mu.Unlock()
			var result any
			switch req.Method {
			case "session.snapshot":
				result = map[string]any{"type": "session_snapshot", "snapshot": map[string]any{"workspaces": []any{}, "tabs": []any{}, "panes": []any{}, "layouts": []any{}, "agents": []any{}}}
			case "workspace.create":
				result = map[string]any{"type": "workspace_created", "workspace": map[string]any{"workspace_id": "w1", "label": "project"}, "tab": map[string]any{"tab_id": "t1"}, "root_pane": map[string]any{"pane_id": "p1"}}
			case "workspace.focus":
				result = map[string]any{"type": "ok"}
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{"id": "x", "result": result})
		}(serverConn)
		return clientConn, nil
	}}
	if _, err := client.Snapshot(); err != nil {
		t.Fatal(err)
	}
	w, _, _, err := client.WorkspaceCreate("/tmp/project", "project", true)
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "w1" {
		t.Fatalf("workspace id = %q", w.ID)
	}
	if err = client.WorkspaceFocus("w1"); err != nil {
		t.Fatal(err)
	}
	if err = client.OpenPickerPopup(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"session.snapshot", "workspace.create", "workspace.focus", "plugin.pane.open"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %v, want %v", methods, want)
		}
	}
}

func TestStateHistoryDeduplicates(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	recordTransition("w1", "w2")
	recordTransition("w2", "w3")
	recordTransition("w3", "w1")
	got := loadState().History
	want := []string{"w1", "w3", "w2"}
	if len(got) != len(want) {
		t.Fatalf("history = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history = %v, want %v", got, want)
		}
	}
	if _, err := os.Stat(stateFile()); err != nil {
		t.Fatal(err)
	}
}
