package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient(handler func(string, map[string]any) (any, *herdrError)) *HerdrClient {
	return &HerdrClient{dial: func() (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var req struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.NewDecoder(server).Decode(&req) != nil {
				return
			}
			result, err := handler(req.Method, req.Params)
			_ = json.NewEncoder(server).Encode(map[string]any{"result": result, "error": err})
		}()
		return client, nil
	}}
}

func TestListKeepsEveryFrecencyPathAndBackendOrder(t *testing.T) {
	base := t.TempDir()
	paths := []string{
		filepath.Join(base, "repo"),
		filepath.Join(base, "repo", "services", "api", "src", "deep"),
		filepath.Join(base, "repo", "services", "worker"),
		filepath.Join(base, "one", "nvim"),
		filepath.Join(base, "two", "nvim"),
		filepath.Join(base, "literal $NOT_AN_ENV_VAR ' spaces"),
	}
	if err := os.MkdirAll(filepath.Join(paths[0], ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&input, "  10.0 %s\n", path)
	}
	file := filepath.Join(base, "frecency.txt")
	if err := os.WriteFile(file, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Frecency.ListCommand = "/bin/cat " + shellQuote(file)
	cfg.Wildcards = []WildcardConfig{{Pattern: filepath.Join(paths[0], "**"), PreviewCommand: "pwd"}}
	s := &Service{cfg: cfg}
	got, err := s.List(map[string]bool{"zoxide": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, c := range got {
		actual = append(actual, c.Path)
		if c.Name != shortPath(c.Path) {
			t.Fatalf("directory name = %q for %q", c.Name, c.Path)
		}
	}
	if !reflect.DeepEqual(actual, paths) {
		t.Fatalf("paths = %q, want %q", actual, paths)
	}
	if len(s.projectCache) != 0 {
		t.Fatal("listing should not discover Git projects")
	}
	if got[1].PreviewCommand != "pwd" {
		t.Fatal("nested wildcard preview was lost")
	}
}

func TestDedupeKeepsDistinctLiveWorkspacesAndConfiguredCommands(t *testing.T) {
	in := []Candidate{
		{Kind: "herdr", Name: "same", Path: "/repo", WorkspaceID: "w1"},
		{Kind: "herdr", Name: "same", Path: "/repo", WorkspaceID: "w2"},
		{Kind: "config", Name: "editor", Path: "/repo", StartupCommand: "nvim"},
		{Kind: "zoxide", Name: "/repo", Path: "/repo"},
		{Kind: "zoxide", Name: "/repo/api", Path: "/repo/api"},
	}
	got := (&Service{}).dedupeCandidates(in)
	if !reflect.DeepEqual(got, append(in[:3:3], in[4])) {
		t.Fatalf("dedupe = %+v", got)
	}
}

func TestConnectDirectoryDoesNotReuseDifferentPathWithSameName(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	var createdPath string
	client := testClient(func(method string, params map[string]any) (any, *herdrError) {
		switch method {
		case "session.snapshot":
			return map[string]any{"snapshot": Snapshot{
				Workspaces: []Workspace{{ID: "old", Label: "repo"}},
				Panes:      []Pane{{WorkspaceID: "old", CWD: filepath.Join(dir, "other")}},
			}}, nil
		case "workspace.create":
			createdPath = params["cwd"].(string)
			return map[string]any{"workspace": Workspace{ID: "new"}, "root_pane": Pane{CWD: createdPath}}, nil
		default:
			return nil, &herdrError{Message: "unexpected " + method}
		}
	})
	s := &Service{client: client, cfg: defaultConfig()}
	s.cfg.Frecency.AddCommand = "/usr/bin/true"
	w, err := s.Connect(Candidate{Kind: "zoxide", Name: "repo", Path: dir}, "")
	if err != nil || w.ID != "new" || createdPath != dir {
		t.Fatalf("connect = %+v, %v; created path %q", w, err, createdPath)
	}
}

func TestExplicitDirectoryPreviewDoesNotListHerdrOrFrecency(t *testing.T) {
	dir := t.TempDir()
	s := &Service{cfg: defaultConfig(), client: testClient(func(string, map[string]any) (any, *herdrError) {
		t.Error("explicit directory preview queried Herdr")
		return nil, &herdrError{Message: "unexpected"}
	})}
	s.cfg.Frecency.ListCommand = "/usr/bin/false"
	s.cfg.Wildcards = []WildcardConfig{{Pattern: dir, PreviewCommand: "printf '%s' {}"}}
	c, err := s.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Preview(c)
	if err != nil || out != dir {
		t.Fatalf("preview = %q, %v", out, err)
	}
}

func TestConnectConfiguredSessionIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       string
		workspace  string
		existing   bool
		foreground bool
		want       string
	}{
		{name: "create despite matching cwd", kind: "config", want: "new"},
		{name: "create despite matching foreground cwd", kind: "config", foreground: true, want: "new"},
		{name: "reuse named session after its cwd changes", kind: "config", existing: true, want: "configured"},
		{name: "directory still reuses path", kind: "zoxide", want: "other"},
		{name: "explicit workspace takes precedence", kind: "config", workspace: "other", existing: true, want: "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			dir := t.TempDir()
			snap := Snapshot{
				FocusedWorkspaceID: "other",
				Workspaces:         []Workspace{{ID: "other", Label: "webapp/backend"}},
				Panes:              []Pane{{WorkspaceID: "other", CWD: dir}},
			}
			if tc.foreground {
				snap.Panes[0].CWD = filepath.Join(dir, "backend")
				snap.Panes[0].ForegroundCWD = dir
			}
			if tc.existing {
				snap.Workspaces = append(snap.Workspaces, Workspace{ID: "configured", Label: "Herdr Sesh Config"})
				snap.Panes = append(snap.Panes, Pane{WorkspaceID: "configured", CWD: filepath.Join(dir, "elsewhere")})
			}
			var focused string
			var created bool
			client := testClient(func(method string, params map[string]any) (any, *herdrError) {
				switch method {
				case "session.snapshot":
					return map[string]any{"snapshot": snap}, nil
				case "workspace.focus":
					focused = params["workspace_id"].(string)
					return map[string]any{}, nil
				case "workspace.create":
					created = true
					if params["cwd"] != dir || params["label"] != "herdr sesh config" {
						t.Errorf("workspace creation = %+v", params)
					}
					return map[string]any{"workspace": Workspace{ID: "new"}, "root_pane": Pane{CWD: dir}}, nil
				default:
					return nil, &herdrError{Message: "unexpected " + method}
				}
			})
			s := &Service{client: client, cfg: defaultConfig()}
			s.cfg.Frecency.AddCommand = "/usr/bin/true"
			w, err := s.Connect(Candidate{Kind: tc.kind, Name: "herdr sesh config", Path: dir, WorkspaceID: tc.workspace}, "")
			if err != nil || w.ID != tc.want {
				t.Fatalf("connect = %+v, %v; want %s", w, err, tc.want)
			}
			if tc.want == "new" {
				if !created || focused != "" {
					t.Fatalf("created = %v, focused = %q", created, focused)
				}
			} else if created || focused != tc.want {
				t.Fatalf("created = %v, focused = %q; want focus %q", created, focused, tc.want)
			}
		})
	}
}

func TestWorkspacePreviewOrdersPanesPreservesANSIAndSkipsPicker(t *testing.T) {
	t.Setenv("HERDR_SESH_PICKER_PANE", "picker")
	var mu sync.Mutex
	var read []string
	client := testClient(func(method string, params map[string]any) (any, *herdrError) {
		if method == "session.snapshot" {
			return map[string]any{"snapshot": Snapshot{
				Workspaces: []Workspace{{ID: "w1", ActiveTabID: "t2"}},
				Panes: []Pane{
					{ID: "background", WorkspaceID: "w1", TabID: "t1"},
					{ID: "active", WorkspaceID: "w1", TabID: "t2"},
					{ID: "closed", WorkspaceID: "w1", TabID: "t2"},
					{ID: "picker", WorkspaceID: "w1", TabID: "t2"},
					{ID: "other", WorkspaceID: "w2"},
				},
			}}, nil
		}
		id := params["pane_id"].(string)
		mu.Lock()
		read = append(read, id)
		mu.Unlock()
		if params["source"] != "recent_unwrapped" || params["format"] != "ansi" || params["strip_ansi"] != false {
			t.Errorf("pane read parameters = %+v", params)
		}
		if id == "closed" {
			return nil, &herdrError{Code: "not_found", Message: "pane closed"}
		}
		return map[string]any{"read": map[string]string{"text": "\x1b[32m" + id + "\x1b[0m"}}, nil
	})
	out, err := (&Service{client: client}).Preview(Candidate{WorkspaceID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b[32mactive\x1b[0m") || strings.Index(out, "active") > strings.Index(out, "background") {
		t.Fatalf("preview = %q", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(read) != 3 {
		t.Fatalf("read panes = %q", read)
	}
}

func TestHerdrReadDeadline(t *testing.T) {
	var server net.Conn
	client := &HerdrClient{timeout: 20 * time.Millisecond, dial: func() (net.Conn, error) {
		var conn net.Conn
		conn, server = net.Pipe()
		return conn, nil
	}}
	_, err := client.Snapshot()
	defer server.Close()
	if err == nil {
		t.Fatal("unresponsive server should time out")
	}
}

func TestDirectoryPreviewFallsBackAfterBrokenTool(t *testing.T) {
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "eza"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := directoryPreview(dir)
	if err != nil || !strings.Contains(out, "nested/") {
		t.Fatalf("fallback = %q, %v", out, err)
	}
}

func TestPickerChildrenUseSelectedConfig(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "custom ' config.toml")
	if err := os.WriteFile(file, []byte("[default_session]\npreview_command = \"printf custom\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _, err := serviceFor(&rootOptions{config: file})
	if err != nil {
		t.Fatal(err)
	}
	// Execute the generated prefix with a harmless argv-printing script.
	script := filepath.Join(dir, "test child")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := shellCommand(s.pickerCommand(script) + " preview").Output()
	if err != nil || string(out) != "--config\n"+file+"\npreview\n" {
		t.Fatalf("child arguments = %q, %v", out, err)
	}
}

func TestCLIConfigAndDirectoryPreviewWithoutHerdr(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	dir := t.TempDir()
	file := filepath.Join(dir, "sesh.toml")
	if err := os.WriteFile(file, []byte("[default_session]\npreview_command = \"printf offline\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand()
	cmd.SetArgs([]string{"-C", file, "preview", dir})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil || out.String() != "offline" {
		t.Fatalf("offline preview = %q, %v", out.String(), err)
	}
}

func TestPickerRetainsRankingAndMatchesNestedPaths(t *testing.T) {
	if _, err := exec.LookPath("fzf"); err != nil {
		t.Skip("fzf not installed")
	}
	rows := "first\t~/repo/services/api/deep\t~/repo/services/api/deep\nsecond\t~/repo/api\t~/repo/api\n"
	cmd := exec.Command("fzf", append(pickerFieldArgs(), "--filter=api")...)
	cmd.Stdin = strings.NewReader(rows)
	out, err := cmd.Output()
	if err != nil || string(out) != rows {
		t.Fatalf("filtered rows = %q, %v", out, err)
	}
}

func TestPopupPassesExplicitConfigToPane(t *testing.T) {
	var environment map[string]any
	client := testClient(func(method string, params map[string]any) (any, *herdrError) {
		if method != "plugin.pane.open" {
			t.Errorf("method = %q", method)
		}
		environment = params["env"].(map[string]any)
		return map[string]any{}, nil
	})
	file := filepath.Join(t.TempDir(), "custom.toml")
	if err := client.OpenPickerPopup(file); err != nil {
		t.Fatal(err)
	}
	if environment["HERDR_SESH_CONFIG"] != file {
		t.Fatalf("popup environment = %+v", environment)
	}
	t.Setenv("HERDR_SESH_CONFIG", file)
	if got, err := configPath(""); err != nil || got != file {
		t.Fatalf("inherited config path = %q, %v", got, err)
	}
}

func TestSmartNameDistinguishesRepositorySubdirectories(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(repo) + "/services/api"
	if got := (&Service{cfg: defaultConfig()}).smartName(nested); got != want {
		t.Fatalf("smart name = %q, want %q", got, want)
	}
}

func TestFrecencyShellExpansionsAndLiteralArguments(t *testing.T) {
	t.Setenv("HERDR_SESH_TEST_VALUE", "expanded value")
	for _, command := range []string{
		`printf '%s' "$HERDR_SESH_TEST_VALUE"`,
		`HERDR_SESH_LOCAL_VALUE='expanded value'; printf '%s' "$HERDR_SESH_LOCAL_VALUE"`,
	} {
		out, err := frecencyCommand(command, "").Output()
		if err != nil || string(out) != "expanded value" {
			t.Fatalf("%s = %q, %v", command, out, err)
		}
	}
	command := frecencyCommand(`zoxide query {}`, `/tmp/literal $HOME ' spaces`)
	if !reflect.DeepEqual(command.Args, []string{"zoxide", "query", `/tmp/literal $HOME ' spaces`}) {
		t.Fatalf("literal query = %q", command.Args)
	}
}
