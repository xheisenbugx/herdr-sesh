# herdr-sesh

A smart [Herdr](https://herdr.dev) workspace manager inspired by
[joshmedeski/sesh](https://github.com/joshmedeski/sesh).

`herdr-sesh` combines live Herdr workspaces, explicitly configured workspaces,
and your zoxide history in one fuzzy picker. Select a live workspace to focus it,
or select a directory to create and focus a workspace rooted there.

<img width="1512" height="808" alt="Screenshot 2026-08-20 at 10 28 57 p m" src="https://github.com/user-attachments/assets/feadf6a0-20ce-463d-95a6-54cb290dcf6d" />


## Features

- Fuzzy picker in a Herdr popup, powered by `fzf`
- Active Herdr workspaces, configured sessions, and zoxide directories in one list
- Full zoxide paths at every depth, in the same order as `sesh list -z`
- Exact-path deduplication across sources, preserving nested directories and distinct live workspaces
- Create-or-focus behavior: Enter focuses an existing workspace or creates it
- Smart names from a Git remote/repository, with directory fallback
- Global, per-session, and wildcard startup and preview commands
- Reusable native Herdr tab layouts
- Session aliases, custom icons, source sorting, blacklist, and JSON output
- Custom frecency backends (`fasd`, `autojump`, `memy`, and similar tools)
- `clone`, `mkdir`, `last`, repository `root`, `rename --enrich`, and tab management
- Shell completions for Bash, Zsh, Fish, and PowerShell
- Imported config files and optional strict config validation
- Native Herdr workspace focus tracking, including workspaces focused outside the picker

## Requirements

- Herdr 0.7.0 or newer
- Go for a source install
- `fzf` for the popup picker
- `zoxide` by default; it can be replaced in `[frecency]`
- `git` for smart repository names, `clone`, and `root`
- Optional preview tools: `eza`, `lsd`, or `tree`
- Optional `gh` for `rename --enrich`

## Install

During local development:

```sh
./scripts/build.sh
herdr plugin link "$PWD"
```

Once this repository is published on GitHub, install it by shorthand:

```sh
herdr plugin install xheisenbugx/herdr-sesh
```

Open the picker from Herdr's plugin actions, or bind it directly in
`~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+s"
type = "plugin_action"
command = "herdr.sesh.picker"
description = "fuzzy workspace picker"
```

Reload Herdr after changing the keybinding:

```sh
herdr server reload-config
```

## Picker

The main action opens an 85% by 75% popup. Its default controls are:

| Key                 | Action                                                        |
| ------------------- | ------------------------------------------------------------- |
| `Enter`             | Focus the selected workspace, or create one for its directory |
| `Ctrl+A`            | Show all sources                                              |
| `Ctrl+W`            | Show active Herdr workspaces                                  |
| `Ctrl+G`            | Show configured sessions                                      |
| `Ctrl+Z`            | Show zoxide/frecency directories                              |
| `Ctrl+D`            | Close the selected live workspace                             |
| `Tab` / `Shift+Tab` | Move down/up                                                  |
| `Esc`               | Cancel                                                        |

The preview shows recent output from every pane in a live workspace. For a
directory it runs the matching `preview_command`, then the default preview
command, then falls back to `eza`, `lsd`, `tree`, or a plain directory listing.
Live previews put the active tab first, retain terminal colors, and exclude the
picker's own pane. Pane reads run concurrently (up to four at a time) with socket
deadlines; a closed pane does not prevent the others from appearing. If an
optional directory preview tool fails, the next available tool is tried.
Directory listings use one entry per line. Previews keep long lines unwrapped
and hide wrap indicators, so narrow popups do not add continuation symbols.

Zoxide entries display their full paths with `~` for your home directory. They
are never collapsed to Git roots or removed because two directories have the
same basename. The combined list hides only zoxide paths already represented by
an active or configured workspace; `Ctrl+Z` shows the complete zoxide source
(subject to your blacklist). Search preserves source/frecency order. Selecting
a nested directory opens that exact directory, with a workspace name such as
`repo/services/api`.

An explicit `--config` is preserved by the popup, source reloads, and previews.

## Configuration

Find the stable, Herdr-managed config path with:

```sh
herdr plugin config-dir herdr.sesh
# sesh.toml lives inside the printed directory

# or, when running the binary directly:
herdr-sesh config-path
```

See [`config.example.toml`](config.example.toml) for a complete starting point.
The most common setup is:

```toml
[default_session]
startup_command = "nvim"
preview_command = "eza --all --git --icons=auto --color=always -- {}"

[[session]]
name = "api"
path = "~/code/api"
alias = "a"
startup_command = "nvim"
preview_command = "bat --color=always README.md"
tabs = ["editor", "git"]

[[tab]]
name = "editor"
startup_command = "nvim"

[[tab]]
name = "git"
startup_command = "lazygit"

[[wildcard]]
pattern = "~/work/**"
startup_command = "make dev"
preview_command = "eza --all --git --icons=auto --color=always -- {}"
```

`{}` in startup, preview, and frecency commands is replaced with a safely quoted
workspace path. A per-session command wins over a wildcard command, which wins
over `[default_session]`. Set `disable_startup_command = true` on a session or
wildcard to suppress the default.

Use `tabs = ["editor", "git"]` with `[[tab]]` definitions for multi-tab Herdr
workspaces. Each tab's `startup_command` runs in that tab. The session startup
command overrides the first tab's reusable command, so a session can customize
what its editor opens while other tabs retain their shared commands.
The older `windows`, `[[window]]`, and `startup_script` spellings remain accepted
for compatibility.

### Wildcards

`*`, `?`, and character classes use Go's `filepath.Match` behavior. A trailing
`/**` recursively matches the directory itself and every descendant. Explicit
`[[session]]` entries always win; otherwise the first matching wildcard wins.

### Imports and strict mode

Split configuration across files with paths or globs relative to the importing
file:

```toml
import = ["work.toml", "sessions/*.toml"]
strict_mode = true
```

Imports are loaded first and the current file overrides scalar settings. Session,
tab, wildcard, and blacklist arrays are appended. Strict mode rejects unknown
keys.

### Custom frecency backend

The defaults use zoxide:

```toml
[frecency]
list_command = "zoxide query --list --score"
query_command = "zoxide query {}"
add_command = "zoxide add {}"
```

Simple commands run directly for minimum startup latency. Commands containing
shell operators or expansions use `/bin/sh -c`, so alternative backends may
still use pipelines when needed. Preview commands also use `/bin/sh -c` and
inherit Herdr's environment without loading shell login profiles. List output is
one literal path per line; a leading numeric score is recognized automatically.
Paths and backend ordering are preserved, including directories containing `$`
or spaces. No filesystem crawl, depth limit, or Git lookup is applied to the list.

## CLI

The plugin binary is also scriptable:

```text
herdr-sesh picker
herdr-sesh list [--source herdr,config,zoxide] [--json]
herdr-sesh connect <name|alias|directory> [--command CMD]
herdr-sesh preview <name|directory>
herdr-sesh close <workspace>
herdr-sesh last
herdr-sesh root [directory] [--connect]
herdr-sesh mkdir <directory> [--command CMD]
herdr-sesh clone <git-url> [directory] [--command CMD]
herdr-sesh rename <name>
herdr-sesh rename --enrich
herdr-sesh tab [name|directory] [--workspace ID] [--json]
herdr-sesh worktree <list|create|open|remove> [Herdr worktree flags]
herdr-sesh completion <bash|zsh|fish|powershell>
```

Commands that talk to Herdr require `HERDR_SOCKET_PATH`; Herdr injects it into
plugin actions and panes automatically. When calling the binary from an ordinary
shell, export the socket path or run the equivalent plugin action.
Directory previews and `list --source zoxide` / `list --source config` also work
without a Herdr socket.

## Sesh compatibility map

The project preserves the behavior of portable sesh features while translating
tmux concepts to Herdr's model:

| sesh                                             | herdr-sesh                                              |
| ------------------------------------------------ | ------------------------------------------------------- |
| tmux session                                     | Herdr workspace                                         |
| tmux window                                      | Herdr tab                                               |
| `sesh list`                                      | `herdr-sesh list`                                       |
| `sesh connect`                                   | `herdr-sesh connect`                                    |
| `sesh picker` / fzf integration                  | popup `picker` action                                   |
| `sesh preview` / `capture-pane`                  | configured preview or Herdr `pane.read`                 |
| `sesh window`                                    | `herdr-sesh tab` (also aliased as `window`)             |
| `sesh last`                                      | `herdr-sesh last`, backed by `workspace.focused` events |
| `sesh root`, `mkdir`, `clone`, `rename --enrich` | same command names                                      |
| tmuxinator/tmuxp                                 | reusable `[[tab]]` layouts                              |
| tmux worktree sessions                           | Herdr's native `herdr worktree` commands                |

Herdr already owns features that sesh has to build around tmux—persistent
workspaces, worktrees, layouts, agent state, remote attachment, and direct pane
APIs. This plugin deliberately uses those native surfaces instead of emulating
tmux or invoking tmuxinator. Sesh's macOS browser-to-GitHub-issue worktree flow is
not duplicated; use Herdr's native worktree picker/API for worktree creation and
opening.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
./scripts/build.sh
herdr plugin link "$PWD"
herdr plugin action invoke herdr.sesh.picker
```

The plugin communicates with Herdr through its newline-delimited JSON socket API.
Protocol tests use in-memory connections and do not require a running Herdr
server.

To compare directory coverage and warmed CLI latency with the installed sesh
and zoxide, run this inside Herdr (Python 3 required):

```sh
python3 scripts/benchmark.py --runs 20
# Optionally compare an older binary saved before rebuilding:
python3 scripts/benchmark.py --before /tmp/herdr-sesh-before --runs 20
go test -run '^$' -bench . -benchmem
```

The CLI benchmark runs only listing and preview commands. Zoxide queries may
perform their normal database housekeeping. Timing includes process startup;
the Go benchmark isolates directory naming and deduplication. No directory list
is cached between commands, so newly visited paths appear on the next reload.
See [PERFORMANCE.md](PERFORMANCE.md) for the measured comparison and validation.

## License

MIT
