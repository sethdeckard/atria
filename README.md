# atria

Agent multiplexer for your terminal.

atria is a TUI dashboard for managing multiple AI coding agents running across your terminal environment. It discovers running agents, shows their real-time status (working, idle, needs input), and lets you send prompts — all from a single view.

Comes with a built-in terminal multiplexer ready to launch agents out of the box. Optional integrations discover agents already running in iTerm2, Kitty, or tmux.

![atria demo](demo.gif)

## Supported Agents

- **Claude Code** (`claude`)
- **Codex** (`codex`)
- **OpenCode** (`opencode`)

## Quick Start

1. Install:

   ```
   brew install sethdeckard/tap/atria
   ```

   Or with Go:

   ```
   go install github.com/sethdeckard/atria@latest
   ```

2. Run:

   ```
   atria
   ```

3. On first run with no config, atria opens a setup wizard to configure watch directories, default agent, and optional integrations. You can also open it later with `S`.

atria scans your watch directories for projects and lets you launch and manage agents in each one. Configuration is stored at `~/.config/atria/config.toml`.

## Requirements

- macOS or Linux
- At least one supported agent on `$PATH`: `claude`, `codex`, or `opencode`

## Configuration

Create or edit `~/.config/atria/config.toml`:

```toml
# Directories to watch for projects
watch_dirs = ["~/projects"]

# Default agent to launch: "claude", "codex", or "opencode"
# default_agent = "claude"

# Terminal dimensions for built-in PTY sessions
# pty_cols = 120
# pty_rows = 40
```

You can also configure everything interactively from the settings screen (`I` key).

## Usage

| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate agents |
| `Enter` | Open chat view to send a prompt |
| `l` | Launch a new agent in the selected project |
| `f` | Focus (switch to) the agent's terminal |
| `d` | Delete an agent session |
| `I` | Open settings |
| `S` | Open setup wizard |
| `s` | Cycle sort column |
| `q` / `Ctrl+C` | Quit |

### Embedded Terminal

When focusing a built-in PTY agent, atria opens an embedded terminal view with full keystroke forwarding. Press `Ctrl+\` to return to the dashboard.

### Chat View

Press `Enter` on an agent to open the chat view. Type a prompt and press `Enter` to send it. The stream panel shows live terminal output from the agent.

## Integrations

Integrations let atria discover agents already running in other terminals and launch new ones as native tabs/windows. They must be explicitly enabled — without them, only the built-in PTY backend is used.

- **iTerm2** — discovers agents in iTerm2 tabs/panes; launches as native tabs when running inside iTerm2
- **Kitty** — discovers agents in Kitty windows; launches as native windows when running inside Kitty
- **tmux** — discovers agents in tmux windows; launches in a detached tmux session

Enable in config or toggle from the settings screen (`I`):

```toml
integrations = ["iterm2", "tmux", "kitty"]
```

Each discovered agent is managed through its native integration — focusing an iTerm2-discovered agent switches to its iTerm2 tab, focusing a Kitty agent switches to its Kitty window, while focusing a PTY agent opens the embedded terminal view.

### iTerm2

Communicates via iTerm2's native protobuf-over-WebSocket API on a Unix socket. No external dependencies.

**Requirements:**
- iTerm2 with the Python API enabled (Settings > General > Magic > Enable Python API)

**Config:**
```toml
integrations = ["iterm2"]
```

For cross-terminal discovery (running atria outside iTerm2 while discovering iTerm2 sessions), disable iTerm2's automation auth by creating `~/.config/iterm2/disable-automation-auth`. Without this, iTerm2 discovery only works when atria runs inside iTerm2.

### Kitty

Communicates via Unix socket using the `kitten @` CLI.

**Requirements** (`kitty.conf`):
```
allow_remote_control yes
listen_on unix:/tmp/kitty-{kitty_pid}
```

**Config:**
```toml
integrations = ["kitty"]
# kitten_path = "kitten"
```

### tmux

Agent sessions run as windows inside a detached tmux session (default: `atria`).

**Config:**
```toml
integrations = ["tmux"]
# tmux_path = "/usr/bin/tmux"
# tmux_session = "atria"
```

The tmux default `allow-rename on` is required for Claude Code's terminal title to propagate. To interact with agents directly: `tmux attach -t atria`.

## Debug

Run with `--debug` to log screen read diagnostics to `/tmp/atria-debug.log`.

## License

atria is licensed under the MIT License. See `LICENSE` for the project license and `THIRD_PARTY_NOTICES.md` for dependency notices.
