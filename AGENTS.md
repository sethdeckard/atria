# Atria - Agent Conventions

## Project Overview

Atria is a Go TUI tool for managing multiple AI coding agents (Claude Code, Codex, OpenCode) running in terminal tabs/panes. Built with Bubble Tea (bubbletea, lipgloss, bubbles).

## Code Style

- Idiomatic Go: follow Effective Go patterns, standard naming conventions
- Format all code with `gofmt` / `goimports`
- Error handling: return errors, don't panic. Wrap errors with context using `fmt.Errorf("context: %w", err)`
- No `log.Fatal` in library code (internal packages). Only `main.go` may exit the process

## Commit Messages

- Subject: max 50 chars, capitalized, imperative mood (e.g. "Add feature" not "Added feature")
- Blank line between subject and body
- Body: wrapped at 72 chars, describe what and why, not how

## Project Structure

```
main.go                          # Entry point
internal/
  config/config.go               # TOML config parsing
  model/types.go                 # Domain types (Project, AgentSession, etc.)
  model/store.go                 # In-memory store + JSON persistence
  terminal/backend.go            # Backend interface
  terminal/detect.go             # Agent detection from session name
  terminal/monitor.go            # Log reading + status classification
  terminal/cache.go              # Cached session list with TTL
  terminal/cwd.go                # CWD discovery strategies (shared)
  terminal/iterm/client.go       # iTerm2 backend (it2 CLI wrapper)
  terminal/tmux/client.go        # tmux backend
  project/discover.go            # Watch dir scanning
  tui/app.go                     # Root Bubble Tea model
  tui/messages.go                # tea.Msg types
  tui/commands.go                # tea.Cmd factories
  tui/keys.go                    # Key bindings
  tui/styles.go                  # Lip Gloss styles
  tui/projectlist.go             # Agent list view (agents dashboard)
  tui/chatview.go                # Chat/send view
  tui/paths.go                   # Path display utilities (contractHome)
  tui/gitinfo.go                 # Git worktree detection
```

## Testing

- Use `_test.go` files in the same package
- Table-driven tests for logic (detection, classification, parsing)
- Use `t.TempDir()` for filesystem tests
- Mock the `terminal.Backend` interface for tests that need terminal operations
- Run: `go test ./...`

## Architecture Rules

- All terminal backend calls must happen in `tea.Cmd` functions (never in Update/View)
- TUI Model owns all state; terminal/project packages are stateless
- Use `tea.Batch` for parallel commands, sequential when dependencies exist
- Monitor processes are OS-level (spawned via os/exec), not goroutines

## Key Patterns

- Two-step send for raw-mode TUI agents: `SendText(text)`, 50ms sleep, `SendText("\r")`
- Session list cached with 5s TTL, invalidated after mutations
- Agent detection: `✳` prefix or "claude" -> Claude; "opencode" -> OpenCode; "codex" -> Codex
- Config uses TOML at `~/.config/atria/config.toml`
- Multiple agents per directory: sessions keyed by SessionID, not ProjectDir
- Bell notification: write `\a` to `/dev/tty` (not stderr) to work through Bubble Tea

## Backends

### iTerm2 Backend (`backend = "iterm2"`)

Uses the `it2` Python CLI wrapper. Auto-installed if missing. Requires iTerm2 with Python API enabled.

### tmux Backend (`backend = "tmux"`)

Agent sessions live as windows inside a dedicated tmux session (`atria`), created detached. Each agent gets its own window; the tmux pane ID (`%0`, `%1`, etc.) serves as the session ID.

**Config options:**
- `tmux_path` — path to tmux binary (default: found via `$PATH`)
- `tmux_session` — tmux session name (default: `"atria"`)

**Requirements:**
- `allow-rename on` (tmux default) for Claude Code's terminal title escape sequences to work as `pane_title`

**Focus behavior:**
- `select-window -t <id>` + best-effort `switch-client -t atria`
- Works automatically when Atria runs inside tmux
- When not in tmux, silently no-ops — user must `tmux attach -t atria` themselves

**MonitorOutput:** Unsupported (no-op with error). Screen reads every 3s are the primary status mechanism.

### Auto-detection

When `backend` is not set in config:
- `$TMUX` set → `"tmux"`
- `$TERM_PROGRAM == "iTerm.app"` → `"iterm2"`
- otherwise → `"iterm2"` (preserve current default)

## Status Detection

Status is determined by reading the bottom 25 lines of each agent's terminal session (via `it2 session read` or `tmux capture-pane`) every 3 seconds. Each line is classified independently, and the highest-priority match wins (needs_input > error > working > idle).

### Per-Agent Pattern Architecture

Patterns are organized per-agent via the `AgentPatterns` struct and `agentPatternRegistry` map in `monitor.go`. Each agent type has its own `NeedsInput`, `Working`, `WorkingExclude`, and `Idle` regex slices. Shared patterns (bell, `Error:`, completed, shell prompt) apply as fallbacks for all agents. This isolates agents so one agent's patterns cannot false-positive on another's screen output. Classification order: shared bell → agent needs_input → shared error → agent working (with exclusions) → shared idle → agent idle.

### Bottom-Region Anchoring

Active statuses (working, needs_input, error) are only trusted in the **bottom 8 lines**, measured from the last non-blank line. This prevents false positives from conversation history in scrollback that may contain quoted prompt text or working indicators. Idle/completed patterns match anywhere since they're low-priority and harmless.

### Claude Code Patterns

| Status | Pattern | Example |
|--------|---------|---------|
| Working | `[✻✶·] \S+…` | `✻ Reading…`, `✶ Doodling… (48s · ↓ 1.4k tokens)` |
| Working | `esc to interrupt` (not in `⏵⏵` lines) | `esc to interrupt` |
| Idle | `❯` prompt | `❯ ` |
| Idle | `? for shortcuts` | `? for shortcuts` |
| Needs input | `Do you want to proceed` | `Do you want to proceed?` |
| Needs input | `Allow .+\?` | `Allow file edit?` |
| Needs input | `Esc to cancel` | `Esc to cancel · Tab to amend` |

Notes:
- `✻` alone (no trailing activity text with `…`) means Claude is **done**, not working.
- Background task lines (`⏵⏵ ... esc to interrupt`) are excluded from working detection.
- Permission dialogs have ~10 blank lines of padding below them; the non-blank anchor handles this.

### Codex Patterns

| Status | Pattern | Example |
|--------|---------|---------|
| Working | `[•●] Working` | `• Working (30s • esc to interrupt)` |
| Idle | `›` prompt | `› Write tests for @filename` |
| Idle | `gpt-\S+-codex` | `gpt-5.3-codex default · 73% left · ~/projects/foo` |

Notes:
- Codex pads the bottom of its screen with many blank lines; 25-line reads are needed to capture content.
- Codex session names are typically static ("codex"), unlike Claude which updates dynamically.

### OpenCode Patterns

| Status | Pattern | Example |
|--------|---------|---------|
| Working | `esc interrupt` (matched by `esc\s+(?:to\s+)?interrupt`) | `■ ..... esc interrupt` |
| Idle | `ctrl\+p commands` | `ctrl+t variants  tab agents  ctrl+p commands` |
| Needs input | `Permission required` | `△ Permission required` |

Notes:
- OpenCode's working indicator uses `esc interrupt` (no "to"), matched by its own agent-specific regex.
- Session names follow `OC | <description> (opencode)` format; `ExtractActivity()` strips both prefix and suffix.
- OpenCode is also a Bubble Tea TUI, so two-step send applies.

### Session Name Activity

Session names are checked on each tick via `ListSessions()`. `ExtractActivity()` strips the `✳` prefix and parenthesized suffixes. Activity text is informational only — displayed in all states (including idle) but does **not** change status. Screen reads are the sole authority on status. Claude updates its tab title even while idle, so session name changes are unreliable as a working signal.

### Debug Logging

Run with `--debug` to write screen read diagnostics to `/tmp/atria-debug.log`. Each entry shows: project name, previous status, new status, whether content changed, the matched line, and full screen content.

### Lessons Learned

1. **5 screen lines is not enough.** Codex pads heavily; its prompt can be 20+ lines from the bottom. Use 25 lines.
2. **Anchor from last non-blank line.** Claude's permission dialogs have ~10 blank lines below them. Measuring "bottom 8" from the absolute screen bottom misses the actual prompt entirely.
3. **Conversation history pollutes detection.** With 25 lines, scrollback contains quoted patterns ("Do you want to proceed?", `✶ Doodling…`, etc.). Restricting active-status matching to the bottom region solves this.
4. **Background task bars look like working.** Claude shows `⏵⏵ ... esc to interrupt` for background tasks even when idle. Must exclude `⏵` lines from `esc to interrupt` matching.
5. **Broad patterns cause cascading false positives.** `(?i)Continue` matched "spinner ticks continue indefinitely" in Codex review output. `\?$` matched any question in text. Keep patterns specific to actual prompt UI text.
6. **Session name changes don't indicate status.** Claude updates its tab title even while idle, so session name changes are unreliable as a working signal. Use session names for activity text only; screen reads are the sole authority on status.
7. **Bell character needs `/dev/tty`.** Writing `\a` to stderr doesn't reach the terminal through Bubble Tea's alternate screen. Write directly to `/dev/tty`.
8. **Multiple agents per directory need session-scoped state.** Keying by ProjectDir causes agents to overwrite each other's sessions and share attention highlights. Key everything (store, attention map) by SessionID.
9. **Slice mutation during iteration skips entries.** Removing dead sessions with `RemoveSession` inside `for range store.Sessions` can skip adjacent entries. Collect IDs first, then remove.
10. **Cross-agent pattern isolation prevents false positives.** With shared regexes, one agent's UI text (e.g. Claude's `❯` prompt) can match when reading a different agent's screen. Per-agent pattern registries ensure each agent's patterns only apply to that agent type. Shared patterns (bell, error, completed) are safe fallbacks.
11. **Killed agents leave orphan panes.** When an agent process exits but the iTerm pane survives, the session ID stays live and the entry persists forever. Detect via: (a) consecutive *stable* (unchanged) unmatched screen reads transition status to idle (3 reads / ~9s); changing content resets the counter since it means the agent is still producing output, and (b) session refresh checks if the iTerm session name no longer matches an agent pattern while idle, and removes it.
