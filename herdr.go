package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"
)

type HerdrClient struct {
	socketPath string
	dial       func() (net.Conn, error)
	sequence   atomic.Uint64
}

type herdrRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}
type herdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type herdrResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *herdrError     `json:"error"`
}

func newHerdrClient() (*HerdrClient, error) {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return nil, errors.New("HERDR_SOCKET_PATH is not set; run this through Herdr or set it explicitly")
	}
	return &HerdrClient{socketPath: path}, nil
}

func (c *HerdrClient) call(method string, params, out any) error {
	var conn net.Conn
	var err error
	if c.dial != nil {
		conn, err = c.dial()
	} else {
		conn, err = net.DialTimeout("unix", c.socketPath, 2*time.Second)
	}
	if err != nil {
		return fmt.Errorf("connect to Herdr: %w", err)
	}
	defer conn.Close()
	id := fmt.Sprintf("herdr-sesh-%d", c.sequence.Add(1))
	if err := json.NewEncoder(conn).Encode(herdrRequest{ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	var resp herdrResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return fmt.Errorf("decode Herdr response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("Herdr %s: %s", resp.Error.Code, resp.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

type Workspace struct {
	ID          string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	ActiveTabID string `json:"active_tab_id"`
	TabCount    int    `json:"tab_count"`
	PaneCount   int    `json:"pane_count"`
}
type Tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
}
type Pane struct {
	ID            string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	CWD           string `json:"cwd"`
	ForegroundCWD string `json:"foreground_cwd"`
	Focused       bool   `json:"focused"`
	Label         string `json:"label"`
}
type Snapshot struct {
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	FocusedTabID       string      `json:"focused_tab_id"`
	FocusedPaneID      string      `json:"focused_pane_id"`
	Workspaces         []Workspace `json:"workspaces"`
	Tabs               []Tab       `json:"tabs"`
	Panes              []Pane      `json:"panes"`
}

func (c *HerdrClient) Snapshot() (Snapshot, error) {
	var out struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	err := c.call("session.snapshot", map[string]any{}, &out)
	return out.Snapshot, err
}
func (c *HerdrClient) WorkspaceCreate(path, label string, focus bool) (Workspace, Tab, Pane, error) {
	var out struct {
		Workspace Workspace `json:"workspace"`
		Tab       Tab       `json:"tab"`
		RootPane  Pane      `json:"root_pane"`
	}
	err := c.call("workspace.create", map[string]any{"cwd": path, "label": label, "focus": focus, "env": map[string]string{}}, &out)
	return out.Workspace, out.Tab, out.RootPane, err
}
func (c *HerdrClient) WorkspaceFocus(id string) error {
	return c.call("workspace.focus", map[string]any{"workspace_id": id}, nil)
}
func (c *HerdrClient) WorkspaceClose(id string) error {
	return c.call("workspace.close", map[string]any{"workspace_id": id}, nil)
}
func (c *HerdrClient) WorkspaceRename(id, label string) error {
	return c.call("workspace.rename", map[string]any{"workspace_id": id, "label": label}, nil)
}
func (c *HerdrClient) OpenPickerPopup() error {
	return c.call("plugin.pane.open", map[string]any{
		"plugin_id":  "herdr.sesh",
		"entrypoint": "picker",
		"placement":  "popup",
		"width":      "85%",
		"height":     "75%",
		"focus":      true,
		"env":        map[string]string{},
	}, nil)
}
func (c *HerdrClient) TabCreate(workspaceID, path, label string, focus bool) (Tab, Pane, error) {
	var out struct {
		Tab      Tab  `json:"tab"`
		RootPane Pane `json:"root_pane"`
	}
	err := c.call("tab.create", map[string]any{"workspace_id": workspaceID, "cwd": path, "label": label, "focus": focus, "env": map[string]string{}}, &out)
	return out.Tab, out.RootPane, err
}
func (c *HerdrClient) TabRename(id, label string) error {
	return c.call("tab.rename", map[string]any{"tab_id": id, "label": label}, nil)
}
func (c *HerdrClient) TabFocus(id string) error {
	return c.call("tab.focus", map[string]any{"tab_id": id}, nil)
}
func (c *HerdrClient) PaneRead(id string, lines int) (string, error) {
	var out struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err := c.call("pane.read", map[string]any{"pane_id": id, "source": "recent_unwrapped", "lines": lines, "format": "ansi", "strip_ansi": false}, &out)
	return out.Read.Text, err
}
func (c *HerdrClient) PaneRun(id, command string) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if text, err := c.PaneRead(id, 5); err == nil && len(text) > 0 {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	return c.call("pane.send_input", map[string]any{"pane_id": id, "text": command, "keys": []string{"Enter"}}, nil)
}
