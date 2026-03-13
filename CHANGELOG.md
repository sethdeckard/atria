# Changelog

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
