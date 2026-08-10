package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

type rootOptions struct{ config string }

func newRootCommand() *cobra.Command {
	opts := &rootOptions{}
	root := &cobra.Command{Use: "herdr-sesh", Short: "Smart Herdr workspace manager powered by zoxide", Version: version, SilenceUsage: true}
	root.PersistentFlags().StringVarP(&opts.config, "config", "C", "", "path to sesh.toml")
	root.AddCommand(newPickerCommand(opts), newUICommand(opts), newListCommand(opts), newConnectCommand(opts), newPreviewCommand(opts), newCloseCommand(opts), newLastCommand(), newRootDirCommand(opts), newMkdirCommand(opts), newCloneCommand(opts), newRenameCommand(), newTabCommand(opts), newTrackCommand(), newConfigCommand(opts))
	root.AddCommand(newWorktreeCommand())
	root.AddCommand(newCompletionCommand(root))
	return root
}

func serviceFor(opts *rootOptions) (*Service, string, error) {
	cfg, path, err := loadConfig(opts.config)
	if err != nil {
		return nil, path, err
	}
	s, err := newService(cfg)
	return s, path, err
}

func newPickerCommand(_ *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "picker", Aliases: []string{"pick", "pk"}, Short: "Open the Herdr fuzzy workspace picker", RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newHerdrClient()
		if err != nil {
			return err
		}
		return client.OpenPickerPopup()
	}}
}
func newUICommand(opts *rootOptions) *cobra.Command {
	var query string
	c := &cobra.Command{Use: "ui", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		return runPickerUI(s, query)
	}}
	c.Flags().StringVarP(&query, "query", "q", "", "initial fuzzy query")
	return c
}

func newListCommand(opts *rootOptions) *cobra.Command {
	var source string
	var jsonOut, pickerLinesOut, blacklisted bool
	c := &cobra.Command{Use: "list", Aliases: []string{"l"}, Short: "List active, configured, and zoxide workspaces", RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		kinds := map[string]bool{}
		if source != "" && source != "all" {
			for _, k := range strings.Split(source, ",") {
				kinds[k] = true
			}
		}
		if pickerLinesOut {
			return pickerLines(s, kinds, cmd.OutOrStdout())
		}
		list, err := s.List(kinds, blacklisted)
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(list)
		}
		show := s.cfg.TUI.ShowIcons == nil || *s.cfg.TUI.ShowIcons
		for _, v := range list {
			fmt.Fprintln(cmd.OutOrStdout(), v.displayName(show))
		}
		return nil
	}}
	c.Flags().StringVar(&source, "source", "all", "comma-separated sources: herdr,config,zoxide")
	c.Flags().BoolVarP(&jsonOut, "json", "j", false, "output JSON")
	c.Flags().BoolVar(&pickerLinesOut, "picker-lines", false, "output internal fzf rows")
	c.Flags().BoolVarP(&blacklisted, "blacklisted", "b", false, "show blacklisted entries")
	return c
}

func newConnectCommand(opts *rootOptions) *cobra.Command {
	var command string
	c := &cobra.Command{Use: "connect <workspace-or-directory>", Aliases: []string{"cn"}, Args: cobra.MinimumNArgs(1), Short: "Focus a workspace or create it from a directory", RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		candidate, err := s.Resolve(strings.Join(args, " "))
		if err != nil {
			return err
		}
		_, err = s.Connect(candidate, command)
		return err
	}}
	c.Flags().StringVarP(&command, "command", "c", "", "command to run only when creating the workspace")
	return c
}

func newPreviewCommand(opts *rootOptions) *cobra.Command {
	var encoded string
	c := &cobra.Command{Use: "preview [workspace-or-directory]", Aliases: []string{"p"}, Args: cobra.MaximumNArgs(1), Short: "Preview a workspace or directory", RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		var candidate Candidate
		if encoded != "" {
			candidate, err = decodeCandidate(encoded)
		} else if len(args) == 1 {
			candidate, err = s.Resolve(args[0])
		} else {
			return errors.New("workspace or --encoded is required")
		}
		if err != nil {
			return err
		}
		out, err := s.Preview(candidate)
		fmt.Fprint(cmd.OutOrStdout(), out)
		return err
	}}
	c.Flags().StringVar(&encoded, "encoded", "", "internal encoded candidate")
	return c
}

func newCloseCommand(opts *rootOptions) *cobra.Command {
	var encoded string
	c := &cobra.Command{Use: "close [workspace]", Args: cobra.MaximumNArgs(1), Short: "Close a workspace", RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		var candidate Candidate
		if encoded != "" {
			candidate, err = decodeCandidate(encoded)
		} else if len(args) > 0 {
			candidate, err = s.Resolve(args[0])
		} else {
			return errors.New("workspace is required")
		}
		if err != nil {
			return err
		}
		if candidate.WorkspaceID == "" {
			return nil
		}
		return s.client.WorkspaceClose(candidate.WorkspaceID)
	}}
	c.Flags().StringVar(&encoded, "encoded", "", "internal encoded candidate")
	return c
}

func newLastCommand() *cobra.Command {
	return &cobra.Command{Use: "last", Aliases: []string{"L"}, Short: "Focus the previously used workspace", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newHerdrClient()
		if err != nil {
			return err
		}
		snap, err := c.Snapshot()
		if err != nil {
			return err
		}
		s := loadState()
		valid := map[string]bool{}
		for _, w := range snap.Workspaces {
			valid[w.ID] = true
		}
		for _, id := range s.History {
			if id != snap.FocusedWorkspaceID && valid[id] {
				recordTransition(snap.FocusedWorkspaceID, id)
				return c.WorkspaceFocus(id)
			}
		}
		return errors.New("no previous workspace found")
	}}
}

func contextCWD() string {
	var ctx struct {
		FocusedPaneCWD string `json:"focused_pane_cwd"`
		WorkspaceCWD   string `json:"workspace_cwd"`
	}
	_ = json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
	if ctx.FocusedPaneCWD != "" {
		return ctx.FocusedPaneCWD
	}
	if ctx.WorkspaceCWD != "" {
		return ctx.WorkspaceCWD
	}
	cwd, _ := os.Getwd()
	return cwd
}
func newRootDirCommand(opts *rootOptions) *cobra.Command {
	var connect bool
	c := &cobra.Command{Use: "root [directory]", Args: cobra.MaximumNArgs(1), Short: "Print or connect to the git repository root", RunE: func(cmd *cobra.Command, args []string) error {
		path := contextCWD()
		if len(args) > 0 {
			path = args[0]
		}
		root, err := gitRoot(path)
		if err != nil {
			return fmt.Errorf("no git root for %s", path)
		}
		if !connect {
			fmt.Fprint(cmd.OutOrStdout(), root)
			return nil
		}
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		candidate := Candidate{Kind: "zoxide", Path: root, Name: smartName(root, s.cfg)}
		s.enrich(&candidate)
		_, err = s.Connect(candidate, "")
		return err
	}}
	c.Flags().BoolVar(&connect, "connect", false, "connect to the root workspace")
	return c
}

func newMkdirCommand(opts *rootOptions) *cobra.Command {
	var command string
	c := &cobra.Command{Use: "mkdir <path>", Aliases: []string{"md"}, Args: cobra.ExactArgs(1), Short: "Create a directory and connect to it", RunE: func(cmd *cobra.Command, args []string) error {
		path, err := expandPath(args[0])
		if err != nil {
			return err
		}
		if !filepath.IsAbs(path) {
			cwd, _ := os.Getwd()
			path = filepath.Join(cwd, path)
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		candidate := Candidate{Kind: "zoxide", Path: path, Name: smartName(path, s.cfg)}
		s.enrich(&candidate)
		_, err = s.Connect(candidate, command)
		return err
	}}
	c.Flags().StringVarP(&command, "command", "c", "", "command to run in the new workspace")
	return c
}

func newCloneCommand(opts *rootOptions) *cobra.Command {
	var command string
	c := &cobra.Command{Use: "clone <repository> [directory]", Args: cobra.RangeArgs(1, 2), Short: "Clone a git repository and connect to it", RunE: func(cmd *cobra.Command, args []string) error {
		cloneArgs := []string{"clone", args[0]}
		dest := ""
		if len(args) == 2 {
			dest, _ = expandPath(args[1])
			cloneArgs = append(cloneArgs, dest)
		}
		git := exec.Command("git", cloneArgs...)
		git.Stdout = os.Stdout
		git.Stderr = os.Stderr
		if err := git.Run(); err != nil {
			return err
		}
		if dest == "" {
			dest = parseRepoName(args[0])
		}
		if !filepath.IsAbs(dest) {
			cwd, _ := os.Getwd()
			dest = filepath.Join(cwd, dest)
		}
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		candidate := Candidate{Kind: "zoxide", Path: dest, Name: smartName(dest, s.cfg)}
		s.enrich(&candidate)
		_, err = s.Connect(candidate, command)
		return err
	}}
	c.Flags().StringVarP(&command, "command", "c", "", "command to run in the new workspace")
	return c
}

func newRenameCommand() *cobra.Command {
	var enrich bool
	c := &cobra.Command{Use: "rename [name]", Args: cobra.MaximumNArgs(1), Short: "Rename the focused workspace", RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newHerdrClient()
		if err != nil {
			return err
		}
		snap, err := client.Snapshot()
		if err != nil {
			return err
		}
		if snap.FocusedWorkspaceID == "" {
			return errors.New("no focused workspace")
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		if enrich {
			for _, p := range snap.Panes {
				if p.WorkspaceID == snap.FocusedWorkspaceID {
					branchOut, e := exec.Command("git", "-C", p.CWD, "branch", "--show-current").Output()
					if e == nil {
						branch := strings.TrimSpace(string(branchOut))
						digits := regexpIssue.FindString(branch)
						if digits != "" {
							out, e := exec.Command("gh", "issue", "view", digits, "--json", "title", "--jq", ".title").Output()
							if e == nil {
								name = branch + " — " + strings.TrimSpace(string(out))
							}
						}
					}
					break
				}
			}
		}
		if name == "" {
			return errors.New("name is required (or use --enrich in a GitHub issue branch)")
		}
		return client.WorkspaceRename(snap.FocusedWorkspaceID, name)
	}}
	c.Flags().BoolVar(&enrich, "enrich", false, "append the GitHub issue title from the current branch")
	return c
}

var regexpIssue = regexp.MustCompile(`\d+`)

func newTabCommand(opts *rootOptions) *cobra.Command {
	var workspace string
	var jsonOut bool
	c := &cobra.Command{Use: "tab [name-or-directory]", Aliases: []string{"window", "w"}, Args: cobra.MaximumNArgs(1), Short: "List, focus, or create tabs (sesh windows)", RunE: func(cmd *cobra.Command, args []string) error {
		s, _, err := serviceFor(opts)
		if err != nil {
			return err
		}
		snap, err := s.client.Snapshot()
		if err != nil {
			return err
		}
		wid := workspace
		if wid == "" {
			wid = snap.FocusedWorkspaceID
		}
		if wid == "" {
			return errors.New("no workspace selected")
		}
		tabs := []Tab{}
		for _, t := range snap.Tabs {
			if t.WorkspaceID == wid {
				tabs = append(tabs, t)
			}
		}
		if len(args) == 0 {
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(tabs)
			}
			for _, t := range tabs {
				fmt.Fprintln(cmd.OutOrStdout(), t.Label)
			}
			return nil
		}
		value := args[0]
		for _, t := range tabs {
			if strings.EqualFold(t.Label, value) {
				return s.client.TabFocus(t.ID)
			}
		}
		path, _ := expandPath(value)
		if st, e := os.Stat(path); e != nil || !st.IsDir() {
			return fmt.Errorf("%q is not an existing tab or directory", value)
		}
		_, _, err = s.client.TabCreate(wid, path, filepath.Base(path), true)
		return err
	}}
	c.Flags().StringVarP(&workspace, "workspace", "s", "", "target workspace ID")
	c.Flags().BoolVarP(&jsonOut, "json", "j", false, "output JSON")
	return c
}

func newTrackCommand() *cobra.Command {
	return &cobra.Command{Use: "track-focus", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error { return trackFocus() }}
}
func newConfigCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "config-path", Short: "Print the active config path", RunE: func(cmd *cobra.Command, args []string) error {
		_, path, err := loadConfig(opts.config)
		if err != nil && opts.config != "" {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return nil
	}}
}
func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Args: cobra.ExactArgs(1), Short: "Generate shell completion", RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return root.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return root.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return root.GenPowerShellCompletion(cmd.OutOrStdout())
		default:
			return fmt.Errorf("unsupported shell %q", args[0])
		}
	}}
}

func newWorktreeCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "worktree [herdr worktree arguments...]",
		Aliases:            []string{"wt"},
		Short:              "Use Herdr's native worktree manager",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			herdr := os.Getenv("HERDR_BIN_PATH")
			if herdr == "" {
				herdr = "herdr"
			}
			child := exec.Command(herdr, append([]string{"worktree"}, args...)...)
			child.Stdin = os.Stdin
			child.Stdout = cmd.OutOrStdout()
			child.Stderr = cmd.ErrOrStderr()
			return child.Run()
		},
	}
}
