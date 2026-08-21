package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMergesImportsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	imported := filepath.Join(dir, "work.toml")
	if err := os.WriteFile(imported, []byte(`
[[session]]
name = "api"
path = "~/work/api"
alias = "a"

[[wildcard]]
pattern = "~/work/**"
startup_command = "nvim"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "sesh.toml")
	if err := os.WriteFile(root, []byte(`
import = ["work.toml"]
dir_length = 2
[default_session]
preview_command = "ls {}"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, gotPath, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != root {
		t.Fatalf("path = %q, want %q", gotPath, root)
	}
	if cfg.DirLength != 2 || len(cfg.Sessions) != 1 || cfg.Sessions[0].Alias != "a" {
		t.Fatalf("unexpected merged config: %+v", cfg)
	}
	if cfg.Frecency.ListCommand == "" || cfg.DefaultSession.PreviewCommand != "ls {}" {
		t.Fatalf("defaults or override missing: %+v", cfg)
	}
}

func TestLoadConfigRejectsDuplicateAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sesh.toml")
	data := `
[[session]]
name = "one"
path = "/one"
alias = "x"
[[session]]
name = "two"
path = "/two"
alias = "X"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate alias") {
		t.Fatalf("error = %v", err)
	}
}

func TestStrictConfigRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sesh.toml")
	if err := os.WriteFile(path, []byte("strict_mode = true\nunknown = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown config keys") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadConfigUsesHerdrTabTerminology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sesh.toml")
	data := `
[[session]]
name = "tmux config"
path = "~/.config/tmux"
tabs = ["editor", "git"]

[[tab]]
name = "editor"
startup_command = "nvim"

[[tab]]
name = "git"
startup_command = "lazygit"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sessions) != 1 || len(cfg.Sessions[0].Tabs) != 2 {
		t.Fatalf("session tabs = %+v", cfg.Sessions)
	}
	if len(cfg.Tabs) != 2 || cfg.Tabs[0].StartupCommand != "nvim" || cfg.Tabs[1].StartupCommand != "lazygit" {
		t.Fatalf("tab definitions = %+v", cfg.Tabs)
	}
}

func TestLoadConfigKeepsLegacyWindowAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sesh.toml")
	data := `
[[session]]
name = "legacy"
path = "/tmp"
windows = ["editor"]

[[window]]
name = "editor"
startup_script = "nvim"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sessions[0].Tabs) != 1 || cfg.Sessions[0].Tabs[0] != "editor" {
		t.Fatalf("legacy session tabs = %+v", cfg.Sessions[0].Tabs)
	}
	if len(cfg.Tabs) != 1 || cfg.Tabs[0].StartupCommand != "nvim" {
		t.Fatalf("legacy tab = %+v", cfg.Tabs)
	}
}
