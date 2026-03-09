# Atria

A TUI for managing multiple AI coding agents (Claude Code, Codex) running in terminal tabs/panes.

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

# Backend: "iterm2" or "tmux" (auto-detected if omitted)
# backend = "tmux"

# Default agent to launch: "claude" or "codex"
# default_agent = "claude"
```

### iTerm2 Backend

The default on macOS. Requires iTerm2 with the Python API enabled (Settings > General > Magic > Enable Python API). The `it2` CLI is auto-installed on first run.

```toml
backend = "iterm2"
# it2_path = "~/.local/share/atria/venv/bin/it2"
```

### tmux Backend

Works in any terminal on any platform. Agent sessions run as windows inside a detached tmux session named `atria`.

```toml
backend = "tmux"
# tmux_path = "/usr/bin/tmux"
# tmux_session = "atria"
```

To interact with agents directly: `tmux attach -t atria`.

The tmux default `allow-rename on` is required for Claude Code's terminal title to propagate as `pane_title`.

### Auto-detection

When `backend` is not set:
- Inside tmux (`$TMUX` set) -> `tmux`
- Inside iTerm2 (`$TERM_PROGRAM` = `iTerm.app`) -> `iterm2`
- Otherwise -> `iterm2`

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

### Chat View

Press `Enter` on an agent to open the chat view. Type a prompt and press `Enter` to send it. The stream panel shows live terminal output from the agent.

## Requirements

- Go 1.21+
- tmux or iTerm2
- Claude Code (`claude`) and/or Codex (`codex`) on `$PATH`

## Debug

Run with `--debug` to log screen read diagnostics to `/tmp/atria-debug.log`.
