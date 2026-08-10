package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	StrictMode         bool                 `toml:"strict_mode"`
	ImportPaths        []string             `toml:"import"`
	DefaultSession     DefaultSessionConfig `toml:"default_session"`
	Blacklist          []string             `toml:"blacklist"`
	Sessions           []SessionConfig      `toml:"session"`
	SortOrder          []string             `toml:"sort_order"`
	Windows            []WindowConfig       `toml:"window"`
	Wildcards          []WildcardConfig     `toml:"wildcard"`
	DirLength          int                  `toml:"dir_length"`
	GitDirLength       int                  `toml:"git_dir_length"`
	GitUseWorktreeRoot bool                 `toml:"git_namer_use_worktree_root"`
	Terminal           string               `toml:"terminal"`
	Frecency           FrecencyConfig       `toml:"frecency"`
	TUI                TUIConfig            `toml:"tui"`
}

type DefaultSessionConfig struct {
	StartupCommand string   `toml:"startup_command"`
	PreviewCommand string   `toml:"preview_command"`
	Windows        []string `toml:"windows"`
}

type SessionConfig struct {
	Name                  string   `toml:"name"`
	Path                  string   `toml:"path"`
	Alias                 string   `toml:"alias"`
	Icon                  string   `toml:"icon"`
	DisableStartupCommand bool     `toml:"disable_startup_command"`
	StartupCommand        string   `toml:"startup_command"`
	PreviewCommand        string   `toml:"preview_command"`
	Windows               []string `toml:"windows"`
}

type WindowConfig struct {
	Name          string `toml:"name"`
	Path          string `toml:"path"`
	StartupScript string `toml:"startup_script"`
}

type WildcardConfig struct {
	Pattern               string   `toml:"pattern"`
	Icon                  string   `toml:"icon"`
	DisableStartupCommand bool     `toml:"disable_startup_command"`
	StartupCommand        string   `toml:"startup_command"`
	PreviewCommand        string   `toml:"preview_command"`
	Windows               []string `toml:"windows"`
}

type FrecencyConfig struct {
	ListCommand  string `toml:"list_command"`
	QueryCommand string `toml:"query_command"`
	AddCommand   string `toml:"add_command"`
}

type TUIConfig struct {
	Prompt       string `toml:"prompt"`
	Header       string `toml:"header"`
	ShowIcons    *bool  `toml:"show_icons"`
	Preview      *bool  `toml:"preview"`
	PreviewWidth int    `toml:"preview_width"`
	Reverse      *bool  `toml:"reverse"`
}

func defaultConfig() Config {
	show, preview, reverse := true, true, true
	return Config{
		SortOrder:    []string{"herdr", "config", "zoxide"},
		DirLength:    1,
		GitDirLength: 1,
		Frecency: FrecencyConfig{
			ListCommand:  "zoxide query --list --score",
			QueryCommand: "zoxide query {}",
			AddCommand:   "zoxide add {}",
		},
		TUI: TUIConfig{
			Prompt: "⚡  ", Header: "enter open/create • ctrl-a all • ctrl-w active • ctrl-g config • ctrl-z zoxide • ctrl-d close",
			ShowIcons: &show, Preview: &preview, PreviewWidth: 55, Reverse: &reverse,
		},
	}
}

func configPath(explicit string) (string, error) {
	if explicit != "" {
		return expandPath(explicit)
	}
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "sesh.toml"), nil
	}
	herdr := os.Getenv("HERDR_BIN_PATH")
	if herdr == "" {
		herdr = "herdr"
	}
	if out, err := exec.Command(herdr, "plugin", "config-dir", "herdr.sesh").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return filepath.Join(dir, "sesh.toml"), nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "herdr-sesh", "sesh.toml"), nil
}

func loadConfig(explicit string) (Config, string, error) {
	cfg := defaultConfig()
	path, err := configPath(explicit)
	if err != nil {
		return cfg, "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if explicit != "" {
			return cfg, path, fmt.Errorf("config file does not exist: %s", path)
		}
		return cfg, path, nil
	} else if err != nil {
		return cfg, path, err
	}
	seen := map[string]bool{}
	if err := decodeConfig(path, &cfg, seen); err != nil {
		return cfg, path, err
	}
	if cfg.DirLength < 1 {
		cfg.DirLength = 1
	}
	if cfg.GitDirLength < 1 {
		cfg.GitDirLength = 1
	}
	if cfg.TUI.PreviewWidth < 10 || cfg.TUI.PreviewWidth > 90 {
		cfg.TUI.PreviewWidth = 55
	}
	if err := validateConfig(cfg); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}

func decodeConfig(path string, cfg *Config, seen map[string]bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if seen[abs] {
		return fmt.Errorf("config import cycle at %s", abs)
	}
	seen[abs] = true
	var local Config
	md, err := toml.DecodeFile(abs, &local)
	if err != nil {
		return fmt.Errorf("parse %s: %w", abs, err)
	}
	if local.StrictMode && len(md.Undecoded()) > 0 {
		return fmt.Errorf("unknown config keys in %s: %v", abs, md.Undecoded())
	}
	for _, imp := range local.ImportPaths {
		imp, err = expandPath(imp)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(imp) {
			imp = filepath.Join(filepath.Dir(abs), imp)
		}
		matches, globErr := filepath.Glob(imp)
		if globErr != nil {
			return globErr
		}
		sort.Strings(matches)
		for _, match := range matches {
			if err := decodeConfig(match, cfg, seen); err != nil {
				return err
			}
		}
	}
	mergeConfig(cfg, local)
	return nil
}

func mergeConfig(dst *Config, src Config) {
	if src.StrictMode {
		dst.StrictMode = true
	}
	if src.DefaultSession.StartupCommand != "" {
		dst.DefaultSession.StartupCommand = src.DefaultSession.StartupCommand
	}
	if src.DefaultSession.PreviewCommand != "" {
		dst.DefaultSession.PreviewCommand = src.DefaultSession.PreviewCommand
	}
	if src.DefaultSession.Windows != nil {
		dst.DefaultSession.Windows = src.DefaultSession.Windows
	}
	dst.Blacklist = append(dst.Blacklist, src.Blacklist...)
	dst.Sessions = append(dst.Sessions, src.Sessions...)
	dst.Windows = append(dst.Windows, src.Windows...)
	dst.Wildcards = append(dst.Wildcards, src.Wildcards...)
	if src.SortOrder != nil {
		dst.SortOrder = src.SortOrder
	}
	if src.DirLength != 0 {
		dst.DirLength = src.DirLength
	}
	if src.GitDirLength != 0 {
		dst.GitDirLength = src.GitDirLength
	}
	if src.GitUseWorktreeRoot {
		dst.GitUseWorktreeRoot = true
	}
	if src.Terminal != "" {
		dst.Terminal = src.Terminal
	}
	if src.Frecency.ListCommand != "" {
		dst.Frecency.ListCommand = src.Frecency.ListCommand
	}
	if src.Frecency.QueryCommand != "" {
		dst.Frecency.QueryCommand = src.Frecency.QueryCommand
	}
	if src.Frecency.AddCommand != "" {
		dst.Frecency.AddCommand = src.Frecency.AddCommand
	}
	if src.TUI.Prompt != "" {
		dst.TUI.Prompt = src.TUI.Prompt
	}
	if src.TUI.Header != "" {
		dst.TUI.Header = src.TUI.Header
	}
	if src.TUI.ShowIcons != nil {
		dst.TUI.ShowIcons = src.TUI.ShowIcons
	}
	if src.TUI.Preview != nil {
		dst.TUI.Preview = src.TUI.Preview
	}
	if src.TUI.PreviewWidth != 0 {
		dst.TUI.PreviewWidth = src.TUI.PreviewWidth
	}
	if src.TUI.Reverse != nil {
		dst.TUI.Reverse = src.TUI.Reverse
	}
}

func validateConfig(cfg Config) error {
	aliases := map[string]string{}
	names := map[string]bool{}
	for i, s := range cfg.Sessions {
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("session %d requires name and path", i+1)
		}
		if names[strings.ToLower(s.Name)] {
			return fmt.Errorf("duplicate session name %q", s.Name)
		}
		names[strings.ToLower(s.Name)] = true
		if s.Alias != "" {
			key := strings.ToLower(s.Alias)
			if prev := aliases[key]; prev != "" {
				return fmt.Errorf("duplicate alias %q for %q and %q", s.Alias, prev, s.Name)
			}
			aliases[key] = s.Name
		}
	}
	windowNames := map[string]bool{}
	for _, w := range cfg.Windows {
		windowNames[w.Name] = true
	}
	for _, s := range cfg.Sessions {
		for _, w := range s.Windows {
			if !windowNames[w] {
				return fmt.Errorf("session %q references unknown window %q", s.Name, w)
			}
		}
	}
	for _, w := range cfg.Wildcards {
		for _, n := range w.Windows {
			if !windowNames[n] {
				return fmt.Errorf("wildcard %q references unknown window %q", w.Pattern, n)
			}
		}
	}
	return nil
}

func expandPath(path string) (string, error) {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	return filepath.Clean(path), nil
}
