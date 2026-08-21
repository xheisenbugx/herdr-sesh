package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestWildcardRecursiveAndSingleLevel(t *testing.T) {
	base := t.TempDir()
	s := &Service{cfg: Config{Wildcards: []WildcardConfig{{Pattern: filepath.Join(base, "one", "*")}, {Pattern: filepath.Join(base, "deep", "**"), StartupCommand: "deep"}}}}
	if _, ok := s.matchWildcard(filepath.Join(base, "one", "project")); !ok {
		t.Fatal("single-level wildcard did not match")
	}
	if _, ok := s.matchWildcard(filepath.Join(base, "one", "project", "nested")); ok {
		t.Fatal("single-level wildcard matched nested path")
	}
	w, ok := s.matchWildcard(filepath.Join(base, "deep", "project", "nested"))
	if !ok || w.StartupCommand != "deep" {
		t.Fatal("recursive wildcard did not match")
	}
}

func TestDedupePrefersActiveCandidate(t *testing.T) {
	path := t.TempDir()
	in := []Candidate{{Kind: "herdr", Name: "live", Path: path, WorkspaceID: "w1"}, {Kind: "config", Name: "configured", Path: path}, {Kind: "zoxide", Name: "recent", Path: path}}
	out := (&Service{cfg: defaultConfig()}).dedupeCandidates(in)
	if len(out) != 1 || out[0].WorkspaceID != "w1" {
		t.Fatalf("dedupe = %+v", out)
	}
}

func TestCandidateEncodingRoundTrip(t *testing.T) {
	want := Candidate{
		Kind: "config", Name: "a name with spaces", Path: "/tmp/project x", Alias: "px",
		Tabs: []string{"editor", "git"}, StartupCommand: "nvim",
		PreviewCommand: "bat --color=always README.md", DisableStartup: true,
	}
	got, err := decodeCandidate(encodeCandidate(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestConfiguredPreviewRunsFromSessionDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "preview.txt"), []byte("session preview\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := decodeCandidate(encodeCandidate(Candidate{
		Kind: "config", Path: dir, PreviewCommand: "cat preview.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := (&Service{}).Preview(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if out != "session preview\n" {
		t.Fatalf("preview = %q", out)
	}
}

func TestConnectFromPickerCreatesConfiguredTabsAndRunsCommands(t *testing.T) {
	dir := t.TempDir()
	sessionStartup := "nvim ~/.config/tmux/tmux.conf"
	var mu sync.Mutex
	var methods []string
	var sent []string
	client := &HerdrClient{dial: func() (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go func(conn net.Conn) {
			defer conn.Close()
			var req struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.NewDecoder(bufio.NewReader(conn)).Decode(&req)
			mu.Lock()
			methods = append(methods, req.Method)
			if req.Method == "pane.send_input" {
				sent = append(sent, req.Params["text"].(string))
			}
			mu.Unlock()
			var result any = map[string]any{"type": "ok"}
			switch req.Method {
			case "session.snapshot":
				result = map[string]any{"snapshot": map[string]any{"workspaces": []any{}, "tabs": []any{}, "panes": []any{}}}
			case "workspace.create":
				result = map[string]any{
					"workspace": map[string]any{"workspace_id": "w1", "label": "tmux config"},
					"tab":       map[string]any{"tab_id": "t1", "workspace_id": "w1"},
					"root_pane": map[string]any{"pane_id": "p1", "workspace_id": "w1", "tab_id": "t1", "cwd": dir},
				}
			case "tab.create":
				result = map[string]any{
					"tab":       map[string]any{"tab_id": "t2", "workspace_id": "w1"},
					"root_pane": map[string]any{"pane_id": "p2", "workspace_id": "w1", "tab_id": "t2", "cwd": dir},
				}
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{"id": "x", "result": result})
		}(serverConn)
		return clientConn, nil
	}}
	s := &Service{
		cfg: Config{Tabs: []TabConfig{
			{Name: "editor", StartupCommand: "nvim"},
			{Name: "git", StartupCommand: "lazygit"},
		}},
		client: client,
	}
	candidate, err := decodeCandidate(encodeCandidate(Candidate{
		Kind: "config", Name: "tmux config", Path: dir,
		Tabs: []string{"editor", "git"}, StartupCommand: sessionStartup,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(candidate, ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(sent, []string{sessionStartup, "lazygit"}) {
		t.Fatalf("commands = %q", sent)
	}
	if got := strings.Join(methods, ","); !strings.Contains(got, "tab.rename") || !strings.Contains(got, "tab.create") {
		t.Fatalf("methods = %v", methods)
	}
	for _, method := range methods {
		if method == "pane.read" {
			t.Fatalf("command launch polled pane output: methods = %v", methods)
		}
	}
}

func TestParseRepoName(t *testing.T) {
	for input, want := range map[string]string{
		"git@github.com:owner/project.git":     "project",
		"https://github.com/owner/project.git": "project",
		"ssh://git@example.com/owner/project":  "project",
	} {
		if got := parseRepoName(input); got != want {
			t.Errorf("parseRepoName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReplaceBracesQuotesPaths(t *testing.T) {
	got := replaceBraces("tool {} --again {}", "/tmp/a b")
	if got != "tool '/tmp/a b' --again '/tmp/a b'" {
		t.Fatalf("got %q", got)
	}
}

func TestFrecencyCommandAvoidsShellAndPreservesSpaces(t *testing.T) {
	cmd := frecencyCommand(`zoxide query {}`, "/tmp/a directory")
	want := []string{"zoxide", "query", "/tmp/a directory"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	if !requiresShell(`zoxide query --list | sort`) {
		t.Fatal("pipeline should require a shell")
	}
}

func TestGitProjectUsesFilesystemMetadata(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "checkout")
	nested := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[remote "origin"]
	url = git@github.com:acme/fast-picker.git
`
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	// Proves naming does not need the git executable in the hot path.
	t.Setenv("PATH", "")
	project, ok := findGitProject(nested, Config{GitDirLength: 1})
	if !ok {
		t.Fatal("expected a Git project")
	}
	if canonicalPath(project.Root) != canonicalPath(repo) || project.Name != "fast-picker" {
		t.Fatalf("project = %+v", project)
	}
}

func TestDedupeCollapsesNestedZoxideRepositoryEntries(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	first := filepath.Join(repo, "services", "api")
	second := filepath.Join(repo, "services", "worker")
	for _, path := range []string{filepath.Join(repo, ".git"), first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{cfg: defaultConfig()}
	out := s.dedupeCandidates([]Candidate{
		{Kind: "zoxide", Name: "repo", Path: first, Score: 10},
		{Kind: "zoxide", Name: "repo", Path: second, Score: 5},
	})
	if len(out) != 1 || out[0].Path != first {
		t.Fatalf("dedupe = %+v", out)
	}
}

func TestDedupePrefersActiveWorkspaceOverZoxideRepository(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(repo, "services", "api")
	for _, path := range []string{filepath.Join(repo, ".git"), nested} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{cfg: defaultConfig()}
	out := s.dedupeCandidates([]Candidate{
		{Kind: "herdr", Name: "api", Path: nested, WorkspaceID: "w1"},
		{Kind: "zoxide", Name: "repo", Path: repo, Score: 10},
	})
	if len(out) != 1 || out[0].WorkspaceID != "w1" {
		t.Fatalf("dedupe = %+v", out)
	}
}

func TestDedupeRemovesSameZoxideDisplayName(t *testing.T) {
	s := &Service{cfg: defaultConfig()}
	out := s.dedupeCandidates([]Candidate{
		{Kind: "zoxide", Name: "nvim", Path: "/config/nvim", Score: 100},
		{Kind: "zoxide", Name: "nvim", Path: "/state/nvim", Score: 10},
	})
	if len(out) != 1 || out[0].Path != "/config/nvim" {
		t.Fatalf("dedupe = %+v", out)
	}
}

func TestDirectoryPreviewUsesCurrentEzaIconSyntax(t *testing.T) {
	tools := t.TempDir()
	eza := filepath.Join(tools, "eza")
	if err := os.WriteFile(eza, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	path := filepath.Join(t.TempDir(), "directory with spaces")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := directoryPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--icons=auto", "--", path} {
		if !strings.Contains(out, want+"\n") {
			t.Fatalf("preview args %q do not contain %q", out, want)
		}
	}
}

func BenchmarkNameAndDedupe250ZoxidePaths(b *testing.B) {
	repo := filepath.Join(b.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		b.Fatal(err)
	}
	paths := make([]string, 250)
	for i := range paths {
		paths[i] = filepath.Join(repo, "services", fmt.Sprintf("service-%03d", i))
		if err := os.MkdirAll(paths[i], 0o755); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := &Service{cfg: defaultConfig()}
		candidates := make([]Candidate, len(paths))
		for j, path := range paths {
			project, ok := s.projectForPath(path)
			if !ok {
				b.Fatal("project not found")
			}
			candidates[j] = Candidate{Kind: "zoxide", Name: project.Name, Path: project.Root}
		}
		if got := len(s.dedupeCandidates(candidates)); got != 1 {
			b.Fatalf("got %d candidates", got)
		}
	}
}
