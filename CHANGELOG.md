# Changelog

## v0.4.0

- Add armed quick responses from the stream panel: `Ctrl+R` arms the selected agent when it needs input, then `y`, `Esc`, or `1-9` sends the response without leaving the dashboard
- Fix stale project-list and stream-panel artifacts by normalizing rendered frames to the terminal width as well as height

## v0.3.2

- Fix tmux discovery missing Codex sessions when generic pane titles override agent-identifying window names
- Run `golangci-lint` in CI using the repo's Go toolchain

## v0.3.1

- tmux discovery now spans all tmux sessions, with new launches defaulting to the current tmux session and an optional `tmux_session` override
- Fix ambiguous tmux targets when the current session name is numeric
- Surface `SendText` errors in the embedded terminal view instead of silently dropping them
- Fix PTY/vt10x size divergence after terminal resize

## v0.3.0

- GitHub Copilot support as a first-class agent alongside Claude Code, Codex, and OpenCode
- Startup upgrade checks, with install hints for Homebrew and `go install` users
- `update_check = false` config option to disable startup upgrade checks
- Safer debug logging with metadata-only `--debug`, an explicit `--debug-unsafe` mode for raw terminal output, and private per-user log storage
- Quit confirmation when embedded PTY sessions are still running
- Session reliability fixes for launch-target selection, tmux session handling, and PTY cleanup on exit
- Recent projects in directory browser navigate to the directory instead of launching immediately, allowing backend choice
- Fix phantom duplicate sessions when integration auto-title inherits embedded agent name
- Fix tall screen reads in iTerm, Kitty, WezTerm, and tmux integrations
- Dashboard polish: status-colored stream panel, responsive layout, 2-segment directory paths, color-coded agent types

## v0.2.0

- Per-launch backend choice: directory browser shows launch actions for both the active integration and embedded terminal
- Environment column in agent list showing integration source (tmux, iterm, kitty, wezterm, embedded)
- Reorder columns: status before directory for better scannability

## v0.1.0

Initial public release.

- Built-in PTY terminal multiplexer for launching and managing agents with no external dependencies
- Real-time agent status detection (working, idle, needs input) via screen reads
- Support for Claude Code, Codex, and OpenCode agents
- iTerm2 integration via native protobuf-over-WebSocket API
- Kitty integration via Unix socket remote control
- tmux integration with detached session management
- WezTerm integration via `wezterm cli` over Unix socket
- Setup wizard for first-run configuration
- Settings screen for managing integrations, watch directories, and preferences
- Embedded terminal view with full keystroke forwarding
- Chat view for sending prompts to agents
- Bell notification when agents need attention
- Debug logging with `--debug` flag
