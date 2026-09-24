# Changelog

## v0.7.0

- Add libatria: atria's terminal integrations, agent detection, status tracking, and a session watcher as a public Go library at `github.com/sethdeckard/atria/libatria`; see `docs/libatria/README.md` for the guide and `docs/libatria/API.md` for the reference. It ships in this module and follows atria's version, so the API may change between minor releases until v1.0.0, and breaking changes are listed here
- Add named key sends to the library (`terminal.SendKey`: Enter, Escape, Tab, arrows, Ctrl-C, Ctrl-D, digits), with tmux mapping them to its own key names because `send-keys -l` is unreliable for control bytes; library only, atria's own UI is unchanged
- Bound every tmux, Kitty, WezTerm, and DeviceTerm CLI call, and each iTerm2 round trip, with a 5s timeout so a hung terminal no longer stalls status polling; a terminal that can't be reached is reported as `terminal.ErrUnavailable`, which library callers can use to keep tracked sessions rather than drop them
- Treat a bell anywhere on the screen as needs-input; before, a bell on a full screen could be swallowed by the bottom-region rule, and the read after a bell no longer looks like the agent moved on
- Discovery under `watch_dirs` now trusts the agent process's own working directory: an agent started from a shell inside a watch directory but running elsewhere is skipped, and a title match to a project outside `watch_dirs` no longer admits a session
- With no `watch_dirs`, discovery accepts agents running at or beneath a known project's directory, not only ones whose title names the project; with no projects either, nothing is discovered, as before
- Fix tmux `SendText` dropping a trailing semicolon or a bare `;`, which tmux's own parser consumed before the pane saw the text
- Fix `watch_dirs = ["/"]` matching nothing
- Fix a data race on the iTerm2 client's connection and another on the PTY dimensions during launch
- `cache_ttl = 0` now means the 5s default rather than no caching, and a name listed twice in `integrations` is probed once

## v0.6.0

- Add a DeviceTerm integration (`integrations = ["deviceterm"]`): agents in DeviceTerm tabs and split panes appear on the dashboard with status, focus, chat, and native launch into a new tab; requires DeviceTerm 0.11.0 or later and atria running in a DeviceTerm Automation tab, because the CLI verbs it uses need that tab's automation grant
- Show product names in the dashboard's env column (DeviceTerm, WezTerm, Kitty, iTerm2) instead of config keys, and size the column to its longest label
- Fix embedded PTY agents being killed when the integration serving as launch target is disabled from settings; they now survive the switch back to the built-in terminal
- Fix the launch target depending on the order integrations were toggled in settings; the settings screen now follows the same precedence as startup
- Fix tracked sessions being dropped and rediscovered with their state lost when enabling or disabling an integration changes the launch target

## v0.5.1

- Homebrew installs are now published as a cask instead of a prebuilt-binary formula, which GoReleaser and Homebrew have both deprecated; `brew install sethdeckard/tap/atria` is unchanged, and both macOS and Linux (Linuxbrew) remain supported
- Atria's unsigned macOS binaries are de-quarantined during cask installation so Gatekeeper does not block them

## v0.5.0

- Show agents' terminal colors in the dashboard stream preview, chat view, and embedded terminal, reconstructed from each backend (PTY, iTerm2, tmux, Kitty, WezTerm) while status detection keeps reading plain text
- Add a `theme` config option: `default` keeps Atria's built-in palette, while `ansi` renders the UI from your terminal's base-16 colors to match your scheme (Catppuccin, Solarized, Dracula, etc.); switchable live from the settings screen
- Fix needs-input detection when Claude renders a todo/task summary below the active prompt, which previously left permission prompts showing as idle
- Fix a data race on the vt10x terminal in the PTY backend
- Fix unbounded growth of the chat view's entry list
- Bump the minimum Go version to 1.26.2 for `go install` builds

## v0.4.3

- Add responsive narrow dashboard layout that adapts columns for small terminal widths
- Add iTerm shell-job orphan detection to clean up stale sessions when the agent process exits but the iTerm pane survives
- Move ASCII logo from `--version` to `--help` output

## v0.4.2

- Add visibility-aware agent refresh so visible sessions update more responsively without increasing polling cost across all tracked sessions
- Fix Codex needs-input detection for plan-style prompts and fallback question UI
- Fix iTerm Claude discovery when session titles are neutral or missing by falling back to CWD and screen-based inference
- Tighten agent screen inference and monitor heuristics to reduce false OpenCode detection and recognize more Claude background-task working states

## v0.4.1

- Fix dashboard stream-panel layout corruption when terminal preview content contains tabs, carriage returns, or ANSI escape sequences, which could hide the header or leave stale render artifacts

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
