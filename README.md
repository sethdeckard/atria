# Atria

A TUI for managing multiple AI coding agents (Claude Code, Codex, OpenCode) running in terminal tabs/panes.

Atria discovers running agents, shows their status (working, idle, needs input), and lets you send prompts — all from a single dashboard.

## Installation

```
go install github.com/sethdeckard/atria@latest
```

## Configuration

Create `~/.config/atria/config.toml`:

```toml
# Directories to watch for projects
watch_dirs = ["~/projects"]

# Default agent to launch: "claude", "codex", or "opencode"
# default_agent = "claude"
```

### Backends

Atria uses a composite backend architecture. A **primary** backend handles launching new agents, while optional **integrations** discover agents already running in other terminals.

The built-in PTY backend is always available as a fallback. When running inside iTerm2 or tmux with the corresponding integration enabled, that environment is used for launching instead — agents appear as native tabs/windows.

#### PTY (built-in)

The default when not running inside iTerm2 or tmux. Each agent runs in a built-in pseudo-terminal with an embedded terminal view. No external dependencies.

```toml
# Terminal dimensions for PTY sessions
# pty_cols = 120
# pty_rows = 40
```

#### iTerm2

Used for launching when running inside iTerm2 with the integration enabled. Requires iTerm2 with the Python API enabled (Settings > General > Magic > Enable Python API). The `it2` CLI is auto-installed on first run.

```toml
integrations = ["iterm2"]
# it2_path = "~/.local/share/atria/venv/bin/it2"
```

#### tmux

Used for launching when running inside tmux with the integration enabled. Agent sessions run as windows inside a detached tmux session named `atria`.

```toml
integrations = ["tmux"]
# tmux_path = "/usr/bin/tmux"
# tmux_session = "atria"
```

To interact with agents directly: `tmux attach -t atria`.

The tmux default `allow-rename on` is required for Claude Code's terminal title to propagate as `pane_title`.

#### Discovery integrations

Integrations also discover agents already running in their environment. You can enable multiple integrations to discover agents across backends:

```toml
integrations = ["iterm2", "tmux"]
```

Each discovered agent is managed through its native backend — focusing an iTerm-discovered agent switches to its iTerm tab, while focusing a PTY agent opens the embedded terminal view.

### Auto-detection

When `integrations` is not set:
- Inside iTerm2 (`$TERM_PROGRAM` = `iTerm.app`) → enables `iterm2` integration
- Inside tmux (`$TMUX` set) → enables `tmux` integration

Launch target follows the same logic — the matching integration is used for launching, with PTY as the fallback.

## Usage

Run `atria` to start the dashboard.

### Key Bindings

| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate agents |
| `Enter` | Open chat view to send a prompt |
| `l` | Launch a new agent in the selected project |
| `f` | Focus (switch to) the agent's terminal |
| `d` | Delete an agent session |
| `Tab` | Cycle sort column |
| `q` / `Ctrl+C` | Quit |

### Embedded Terminal (PTY)

When focusing a PTY agent, Atria opens an embedded terminal view with full keystroke forwarding. Press `Ctrl+\` to return to the dashboard.

### Chat View

Press `Enter` on an agent to open the chat view. Type a prompt and press `Enter` to send it. The stream panel shows live terminal output from the agent.

## Requirements

- Go 1.21+
- Claude Code (`claude`), Codex (`codex`), and/or OpenCode (`opencode`) on `$PATH`
- Optional: tmux or iTerm2 for native terminal integration

## Debug

Run with `--debug` to log screen read diagnostics to `/tmp/atria-debug.log`.
