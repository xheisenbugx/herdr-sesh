package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	want := Candidate{Kind: "config", Name: "a name with spaces", Path: "/tmp/project x", Alias: "px"}
	got, err := decodeCandidate(encodeCandidate(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
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
