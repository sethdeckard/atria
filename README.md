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

### Integrations

The built-in PTY backend is always available. Optional **integrations** discover agents running in other terminals and, when the environment matches, handle launching too — agents appear as native tabs/windows.

#### PTY (built-in)

The default when not running inside iTerm2 or tmux. Each agent runs in a built-in pseudo-terminal with an embedded terminal view. No external dependencies.

```toml
# Terminal dimensions for PTY sessions
# pty_cols = 120
# pty_rows = 40
```

#### iTerm2

Discovers existing agents in iTerm2 tabs and panes (including splits). When running inside iTerm2, also launches new agents as native tabs. Communicates via iTerm2's native protobuf-over-WebSocket API on a Unix socket — no external dependencies.

Requires iTerm2 with the Python API enabled (Settings > General > Magic > Enable Python API).

For cross-terminal discovery (running Atria in Terminal/tmux/Kitty while discovering iTerm2 sessions), disable iTerm2's automation auth: create `~/.config/iterm2/disable-automation-auth`. Without this, iTerm2 discovery only works when Atria runs inside iTerm2.

```toml
integrations = ["iterm2"]
```

#### Kitty

Discovers existing agents in Kitty windows. When running inside Kitty, also launches new agents as native tabs. Communicates via Unix socket to avoid TTY conflicts with Bubble Tea.

Requires `kitty.conf`:
```
allow_remote_control yes
listen_on unix:/tmp/kitty-{kitty_pid}
```

```toml
integrations = ["kitty"]
# kitten_path = "kitten"
```

#### tmux

Discovers existing agents in tmux windows. When running inside tmux, also launches new agents as native windows in a detached tmux session named `atria`.

```toml
integrations = ["tmux"]
# tmux_path = "/usr/bin/tmux"
# tmux_session = "atria"
```

To interact with agents directly: `tmux attach -t atria`.

The tmux default `allow-rename on` is required for Claude Code's terminal title to propagate as `pane_title`.

#### Multiple integrations

You can enable multiple integrations to discover agents across environments:

```toml
integrations = ["iterm2", "tmux", "kitty"]
```

Each discovered agent is managed through its native integration — focusing an iTerm-discovered agent switches to its iTerm tab, focusing a Kitty agent switches to its Kitty window, while focusing a PTY agent opens the embedded terminal view.

Integrations must be explicitly enabled. Without `integrations` configured, only the built-in PTY backend is used. You can also toggle integrations from the settings screen (`I` key) — changes take effect immediately and persist to config.

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
| `I` | Open settings |
| `s` | Cycle sort column |
| `q` / `Ctrl+C` | Quit |

### Embedded Terminal (PTY)

When focusing a PTY agent, Atria opens an embedded terminal view with full keystroke forwarding. Press `Ctrl+\` to return to the dashboard.

### Chat View

Press `Enter` on an agent to open the chat view. Type a prompt and press `Enter` to send it. The stream panel shows live terminal output from the agent.

## Requirements

- Go 1.21+
- Claude Code (`claude`), Codex (`codex`), and/or OpenCode (`opencode`) on `$PATH`
- Optional: tmux, iTerm2, or Kitty for native terminal integration

## Debug

Run with `--debug` to log screen read diagnostics to `/tmp/atria-debug.log`.

## License

`atria` is licensed under the MIT License. See `LICENSE` for the project
license and `THIRD_PARTY_NOTICES.md` for dependency notices.
